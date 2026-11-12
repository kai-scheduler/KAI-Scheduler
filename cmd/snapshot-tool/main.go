// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	verbosity := fs.Int("verbosity", 4, "logging verbosity")
	filename := fs.String("filename", "", "location of the zipped JSON file")
	cpuprofile := fs.String("cpuprofile", "", "write cpu profile to file")
	memprofile := fs.String("memprofile", "", "write memory profile to file")

	_ = fs.Parse(os.Args[1:])
	if filename == nil || len(*filename) == 0 {
		fs.Usage()
		return
	}

	snapshot.DisableWatchListClientForFakeReplay()

	if err := log.InitLoggers(int(*verbosity), false); err != nil {
		fmt.Printf("Failed to initialize logger: %v", err)
		return
	}
	defer func() {
		syncErr := log.InfraLogger.Sync()
		if syncErr != nil && !errors.Is(syncErr, syscall.EINVAL) {
			fmt.Printf("Failed to write log: %v", syncErr)
		}
	}()
	log.InfraLogger.SetSessionID("snapshot-runner")

	runStart := time.Now()

	loadClients := snapshot.LoadClientsWithSnapshot
	disableWatchList := snapshot.DisableWatchListClientForFakeReplay
	loaded, err := loadSnapshot(*filename)
	if err != nil {
		log.InfraLogger.Fatalf(err.Error(), err)
	}

	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	disableWatchList()
	kubeClient, kaiClient, nrtClient := loadClients(loaded.RawObjects, loaded.Discovery)

	schedulerCacheParams := &cache.SchedulerCacheParams{
		KubeClient:                  kubeClient,
		KAISchedulerClient:          kaiClient,
		NRTClient:                   nrtClient,
		SchedulerName:               loaded.SchedulerParams.SchedulerName,
		NodePoolParams:              loaded.SchedulerParams.PartitionParams,
		RestrictNodeScheduling:      loaded.SchedulerParams.RestrictSchedulingNodes,
		DetailedFitErrors:           loaded.SchedulerParams.DetailedFitErrors,
		ScheduleCSIStorage:          loaded.SchedulerParams.ScheduleCSIStorage,
		FullHierarchyFairness:       loaded.SchedulerParams.FullHierarchyFairness,
		AllowConsolidatingReclaim:   loaded.SchedulerParams.AllowConsolidatingReclaim,
		NumOfStatusRecordingWorkers: loaded.SchedulerParams.NumOfStatusRecordingWorkers,
		StuckInReleasingThreshold:   loaded.SchedulerParams.StuckInReleasingThreshold,
		DiscoveryClient:             kubeClient.Discovery(),
	}

	schedulerCache, err := cache.New(schedulerCacheParams)
	if err != nil {
		log.InfraLogger.Fatalf("Failed to create scheduler cache: %v", err)
	}
	stopCh := make(chan struct{})
	schedulerCache.Run(stopCh)
	schedulerCache.WaitForCacheSync(stopCh)

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.InfraLogger.Fatalf("Failed to create CPU profile file: %v", err)
		}
		err = pprof.StartCPUProfile(f)
		if err != nil {
			log.InfraLogger.Fatalf("Failed to start CPU profile: %v", err)
		}

		defer pprof.StopCPUProfile()
	}

	ssn, err := framework.OpenSession(
		schedulerCache, loaded.Config, loaded.SchedulerParams, "", &http.ServeMux{},
	)
	if err != nil {
		log.InfraLogger.Fatalf(err.Error(), err)
	}
	defer framework.CloseSession(ssn)

	actionDurations := make(map[string]time.Duration)
	actions, _ := conf_util.GetActionsFromConfig(loaded.Config)
	for _, action := range actions {
		log.InfraLogger.SetAction(string(action.Name()))
		metrics.SetCurrentAction(string(action.Name()))
		actionStartTime := time.Now()
		action.Execute(ssn)
		elapsed := time.Since(actionStartTime)
		metrics.UpdateActionDuration(string(action.Name()), metrics.Duration(actionStartTime))
		log.InfraLogger.V(2).Infof("Action <%s> completed in %v", action.Name(), elapsed)
		actionDurations[string(action.Name())] = elapsed
	}

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.InfraLogger.Errorf("Failed to create memory profile file: %v", err)
		} else {
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.InfraLogger.Errorf("Failed to write memory profile: %v", err)
			}
			f.Close()
		}
	}

	totalDuration := time.Since(runStart)
	log.InfraLogger.V(2).Infof("Snapshot tool run completed in %v", totalDuration)
	printRunSummary(totalDuration, actionDurations)
}

type runSummary struct {
	TotalDurationMs   int64            `json:"total_duration_ms"`
	ActionDurationsMs map[string]int64 `json:"action_durations_ms"`
}

func printRunSummary(totalDuration time.Duration, actionDurations map[string]time.Duration) {
	actionMs := make(map[string]int64, len(actionDurations))
	for name, d := range actionDurations {
		actionMs[name] = d.Milliseconds()
	}
	summary := runSummary{
		TotalDurationMs:   totalDuration.Milliseconds(),
		ActionDurationsMs: actionMs,
	}
	out, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		log.InfraLogger.Errorf("Failed to marshal run summary: %v", err)
		return
	}
	fmt.Println(string(out))
}

func loadSnapshot(filename string) (*snapshot.Snapshot, error) {
	zipFile, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer zipFile.Close()

	for _, file := range zipFile.File {
		if file.Name == snapshot.SnapshotFileName {
			jsonFile, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer jsonFile.Close()

			var snapshot snapshot.Snapshot
			err = json.NewDecoder(jsonFile).Decode(&snapshot)
			if err != nil {
				return nil, err
			}

			return &snapshot, nil
		}
	}

	return nil, os.ErrNotExist
}

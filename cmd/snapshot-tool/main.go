// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime/pprof"
	"syscall"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	version "k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"

	kaischedulerfake "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/cache"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/metrics"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins/snapshot"
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	verbosity := fs.Int("verbosity", 4, "logging verbosity")
	filename := fs.String("filename", "", "location of the zipped JSON file")
	cpuprofile := fs.String("cpuprofile", "", "write cpu profile to file")
	_ = fs.Parse(os.Args[1:])
	if filename == nil || len(*filename) == 0 {
		fs.Usage()
		return
	}

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

	snapshot, err := loadSnapshot(*filename)
	if err != nil {
		log.InfraLogger.Fatalf(err.Error(), err)
	}

	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	kubeClient, kaiClient := loadClientsWithSnapshot(snapshot.RawObjects, snapshot.Discovery)

	schedulerCacheParams := &cache.SchedulerCacheParams{
		KubeClient:                  kubeClient,
		KAISchedulerClient:          kaiClient,
		SchedulerName:               snapshot.SchedulerParams.SchedulerName,
		NodePoolParams:              snapshot.SchedulerParams.PartitionParams,
		RestrictNodeScheduling:      snapshot.SchedulerParams.RestrictSchedulingNodes,
		DetailedFitErrors:           snapshot.SchedulerParams.DetailedFitErrors,
		ScheduleCSIStorage:          snapshot.SchedulerParams.ScheduleCSIStorage,
		FullHierarchyFairness:       snapshot.SchedulerParams.FullHierarchyFairness,
		AllowConsolidatingReclaim:   snapshot.SchedulerParams.AllowConsolidatingReclaim,
		NumOfStatusRecordingWorkers: snapshot.SchedulerParams.NumOfStatusRecordingWorkers,
		DiscoveryClient:             kubeClient.Discovery(),
	}

	schedulerCache := cache.New(schedulerCacheParams)
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
		schedulerCache, snapshot.Config, snapshot.SchedulerParams, "", &http.ServeMux{},
	)
	if err != nil {
		log.InfraLogger.Fatalf(err.Error(), err)
	}
	defer framework.CloseSession(ssn)

	actions, _ := conf_util.GetActionsFromConfig(snapshot.Config)
	for _, action := range actions {
		log.InfraLogger.SetAction(string(action.Name()))
		metrics.SetCurrentAction(string(action.Name()))
		actionStartTime := time.Now()
		action.Execute(ssn)
		metrics.UpdateActionDuration(string(action.Name()), metrics.Duration(actionStartTime))
	}
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

func loadClientsWithSnapshot(rawObjects *snapshot.RawKubernetesObjects, discoverySnapshot *snapshot.DiscoverySnapshot) (*fake.Clientset, *kaischedulerfake.Clientset) {
	kubeClient := fake.NewSimpleClientset()
	kaiClient := kaischedulerfake.NewSimpleClientset()
	applyDiscoverySnapshot(kubeClient, discoverySnapshot)

	for _, pod := range rawObjects.Pods {
		_, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create pod: %v", err)
		}
	}

	for _, node := range rawObjects.Nodes {
		_, err := kubeClient.CoreV1().Nodes().Create(context.TODO(), node, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create node: %v", err)
		}
	}

	for _, bindRequest := range rawObjects.BindRequests {
		_, err := kaiClient.SchedulingV1alpha2().BindRequests(bindRequest.Namespace).Create(context.TODO(), bindRequest, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create bind request: %v", err)
		}
	}

	for _, podGroup := range rawObjects.PodGroups {
		_, err := kaiClient.SchedulingV2alpha2().PodGroups(podGroup.Namespace).Create(context.TODO(), podGroup, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create pod group: %v", err)
		}
	}

	for _, queue := range rawObjects.Queues {
		_, err := kaiClient.SchedulingV2().Queues(queue.Namespace).Create(context.TODO(), queue, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create queue: %v", err)
		}
	}

	for _, priorityClass := range rawObjects.PriorityClasses {
		_, err := kubeClient.SchedulingV1().PriorityClasses().Create(context.TODO(), priorityClass, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create priority class: %v", err)
		}
	}

	for _, configMap := range rawObjects.ConfigMaps {
		_, err := kubeClient.CoreV1().ConfigMaps(configMap.Namespace).Create(context.TODO(), configMap, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create config map: %v", err)
		}
	}

	for _, persistentVolumeClaim := range rawObjects.PersistentVolumeClaims {
		_, err := kubeClient.CoreV1().PersistentVolumeClaims(persistentVolumeClaim.Namespace).Create(context.TODO(), persistentVolumeClaim, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create persistent volume claim: %v", err)
		}
	}

	for _, csiStorageCapacity := range rawObjects.CSIStorageCapacities {
		_, err := kubeClient.StorageV1().CSIStorageCapacities(csiStorageCapacity.Namespace).Create(context.TODO(), csiStorageCapacity, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create CSI storage capacity: %v", err)
		}
	}

	for _, storageClass := range rawObjects.StorageClasses {
		_, err := kubeClient.StorageV1().StorageClasses().Create(context.TODO(), storageClass, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create storage class: %v", err)
		}
	}

	for _, csiDriver := range rawObjects.CSIDrivers {
		_, err := kubeClient.StorageV1().CSIDrivers().Create(context.TODO(), csiDriver, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create CSI driver: %v", err)
		}
	}

	for _, topology := range rawObjects.Topologies {
		_, err := kaiClient.KaiV1alpha1().Topologies().Create(context.TODO(), topology, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create topology: %v", err)
		}
	}

	for _, resourceClaim := range rawObjects.ResourceClaims {
		_, err := kubeClient.ResourceV1().ResourceClaims(resourceClaim.Namespace).Create(context.TODO(), resourceClaim, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create resource claim: %v", err)
		}
	}

	for _, resourceSlice := range rawObjects.ResourceSlices {
		_, err := kubeClient.ResourceV1().ResourceSlices().Create(context.TODO(), resourceSlice, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create resource slice: %v", err)
		}
	}

	for _, deviceClass := range rawObjects.DeviceClasses {
		_, err := kubeClient.ResourceV1().DeviceClasses().Create(context.TODO(), deviceClass, v1.CreateOptions{})
		if err != nil {
			log.InfraLogger.Errorf("Failed to create device class: %v", err)
		}
	}

	return kubeClient, kaiClient
}

func applyDiscoverySnapshot(kubeClient *fake.Clientset, discoverySnapshot *snapshot.DiscoverySnapshot) {
	if kubeClient == nil || discoverySnapshot == nil {
		return
	}

	fakeDiscoveryClient, ok := kubeClient.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		return
	}

	if discoverySnapshot.ServerVersion != nil {
		fakeDiscoveryClient.FakedServerVersion = &version.Info{
			Major: discoverySnapshot.ServerVersion.Major,
			Minor: discoverySnapshot.ServerVersion.Minor,
		}
	}
	if discoverySnapshot.Resources != nil {
		kubeClient.Resources = discoverySnapshot.Resources
	}
}

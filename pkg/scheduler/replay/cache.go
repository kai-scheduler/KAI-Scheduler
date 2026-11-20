// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/clock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
)

type staticUsage struct {
	usage *queue_info.ClusterUsage
}

func (s staticUsage) GetResourceUsage() (*queue_info.ClusterUsage, error) {
	if s.usage == nil {
		return queue_info.NewClusterUsage(), nil
	}
	return s.usage, nil
}

// NewReplayCache loads a recorded snapshot into a scheduler cache.
// opened, when set, is the recorded cycle open time. The decision clock then
// runs forward from that instant. usage, when set, is the recorded fair-share usage.
func NewReplayCache(
	params *conf.SchedulerParams, objects *snapshot.RawKubernetesObjects,
	usage *queue_info.ClusterUsage, opened time.Time,
) (cache.Cache, context.CancelFunc, error) {
	DisableWatchListClientForFakeReplay()
	restore := clock.Set(opened)
	fail := func(err error) (cache.Cache, context.CancelFunc, error) {
		if restore != nil {
			restore()
		}
		return nil, nil, err
	}
	kubeClient, kaiClient, nrtClient := LoadClientsWithSnapshot(objects, nil)
	cacheParams := &cache.SchedulerCacheParams{
		KubeClient:                  kubeClient,
		KAISchedulerClient:          kaiClient,
		NRTClient:                   nrtClient,
		SchedulerName:               "kai-scheduler",
		NumOfStatusRecordingWorkers: 1,
		DiscoveryClient:             kubeClient.Discovery(),
	}
	if params != nil {
		cacheParams.SchedulerName = params.SchedulerName
		cacheParams.NodePoolParams = params.PartitionParams
		cacheParams.RestrictNodeScheduling = params.RestrictSchedulingNodes
		cacheParams.DetailedFitErrors = params.DetailedFitErrors
		cacheParams.ScheduleCSIStorage = params.ScheduleCSIStorage
		cacheParams.FullHierarchyFairness = params.FullHierarchyFairness
		cacheParams.AllowConsolidatingReclaim = params.AllowConsolidatingReclaim
		if params.NumOfStatusRecordingWorkers > 0 {
			cacheParams.NumOfStatusRecordingWorkers = params.NumOfStatusRecordingWorkers
		}
		cacheParams.StuckInReleasingThreshold = params.StuckInReleasingThreshold
	}
	if cacheParams.NodePoolParams == nil {
		cacheParams.NodePoolParams = &conf.SchedulingNodePoolParams{}
	}
	if usage != nil {
		interval := time.Second
		cacheParams.UsageDBClient = staticUsage{usage: usage}
		cacheParams.UsageDBParams = &api.UsageParams{
			FetchInterval:   &metav1.Duration{Duration: interval},
			StalenessPeriod: &metav1.Duration{Duration: time.Hour},
			WaitTimeout:     &metav1.Duration{Duration: 5 * time.Second},
		}
	}
	schedCache, err := cache.New(cacheParams)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	schedCache.Run(ctx.Done())
	schedCache.WaitForCacheSync(ctx.Done())
	return schedCache, func() {
		cancel()
		if restore != nil {
			restore()
		}
	}, nil
}

// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	schedcache "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	kaifake "github.com/kai-scheduler/api/client/clientset/versioned/fake"
)

func TestGetEvictionActionNames(t *testing.T) {
	actions.InitDefaultActions()
	configuredActions, err := conf_util.GetActionsFromConfig(&conf.SchedulerConfiguration{
		Actions: "allocate, reclaim, stalegangeviction",
	})
	require.NoError(t, err)

	require.Equal(t, []string{"reclaim", "stalegangeviction"}, getEvictionActionNames(configuredActions))
}

func TestRunOnceExecutesTheConfiguredAction(t *testing.T) {
	require.NoError(t, log.InitLoggers(0, false))
	actions.InitDefaultActions()
	kubeClient := fake.NewSimpleClientset()
	kaiClient := kaifake.NewSimpleClientset()
	schedCache, err := schedcache.New(&schedcache.SchedulerCacheParams{
		KubeClient:                  kubeClient,
		KAISchedulerClient:          kaiClient,
		SchedulerName:               "kai-scheduler",
		NodePoolParams:              &conf.SchedulingNodePoolParams{},
		NumOfStatusRecordingWorkers: 1,
		DiscoveryClient:             kubeClient.Discovery(),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	schedCache.Run(ctx.Done())
	schedCache.WaitForCacheSync(ctx.Done())

	sched := &Scheduler{
		cache: schedCache,
		config: &conf.SchedulerConfiguration{
			Actions: "allocate",
		},
		schedulerParams: &conf.SchedulerParams{
			SchedulerName:   "kai-scheduler",
			PartitionParams: &conf.SchedulingNodePoolParams{},
		},
		mux: http.NewServeMux(),
	}
	sched.runOnce()
}

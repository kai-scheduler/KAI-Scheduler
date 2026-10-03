// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestBindReadyHooks(t *testing.T) {
	failure := errors.New("readiness evaluation failed")
	for _, tc := range []struct {
		name    string
		results []bool
		err     error
		ready   bool
	}{
		{name: "no hooks", ready: true},
		{name: "all ready", results: []bool{true, true}, ready: true},
		{name: "one deferred", results: []bool{true, false, true}},
		{name: "evaluation error", results: []bool{true}, err: failure},
		{name: "error after deferral", results: []bool{false}, err: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ssn := &framework.Session{}
			task, node := &pod_info.PodInfo{}, &node_info.NodeInfo{}
			for _, result := range tc.results {
				ssn.AddBindReadyFn(func(actualTask *pod_info.PodInfo, actualNode *node_info.NodeInfo) (bool, error) {
					require.Same(t, task, actualTask)
					require.Same(t, node, actualNode)
					return result, nil
				})
			}
			if tc.err != nil {
				ssn.AddBindReadyFn(func(*pod_info.PodInfo, *node_info.NodeInfo) (bool, error) { return false, tc.err })
			}
			ready, err := ssn.IsTaskReadyForBinding(task, node)
			require.Equal(t, tc.ready, ready)
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestStatementAllocateOrPipeline(t *testing.T) {
	defer gock.Off()
	test_utils.InitTestingInfrastructure()
	failure := errors.New("readiness evaluation failed")
	for _, tc := range []struct {
		name   string
		hook   api.BindReadyFn
		err    error
		status pod_status.PodStatus
		direct bool
	}{
		{name: "no hooks allocate", status: pod_status.Allocated},
		{name: "ready allocates", hook: func(*pod_info.PodInfo, *node_info.NodeInfo) (bool, error) { return true, nil }, status: pod_status.Allocated},
		{name: "deferred pipelines", hook: func(*pod_info.PodInfo, *node_info.NodeInfo) (bool, error) { return false, nil }, status: pod_status.Pipelined},
		{name: "error preserves state", hook: func(*pod_info.PodInfo, *node_info.NodeInfo) (bool, error) { return false, failure }, err: failure, status: pod_status.Pending},
		{name: "low-level allocate bypasses readiness", hook: func(*pod_info.PodInfo, *node_info.NodeInfo) (bool, error) {
			t.Fatal("readiness must not run")
			return false, nil
		}, status: pod_status.Allocated, direct: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
				Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 2}},
				Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 2, GPUOverQuotaWeight: 1}},
				Jobs:   []*jobs_fake.TestJobBasic{{Name: "job0", QueueName: "queue0", RequiredGPUsPerTask: 1, Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}}}},
				Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
			}, gomock.NewController(t))
			var task *pod_info.PodInfo
			for _, candidate := range ssn.ClusterInfo.PodGroupInfos["job0"].GetAllPodsMap() {
				task = candidate
			}
			require.NotNil(t, task)
			if tc.hook != nil {
				ssn.AddBindReadyFn(tc.hook)
			}
			stmt := ssn.Statement()
			checkpoint := stmt.Checkpoint()
			var err error
			if tc.direct {
				err = stmt.Allocate(task, "node0")
			} else {
				err = stmt.AllocateOrPipeline(task, "node0")
			}
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.status, task.Status)
			if tc.err != nil {
				require.Empty(t, task.NodeName)
				require.Empty(t, ssn.ClusterInfo.Nodes["node0"].PodInfos)
			} else {
				require.Equal(t, "node0", task.NodeName)
				require.Len(t, ssn.ClusterInfo.Nodes["node0"].PodInfos, 1)
			}
			require.NoError(t, stmt.Rollback(checkpoint))
			require.Equal(t, pod_status.Pending, task.Status)
			require.Empty(t, ssn.ClusterInfo.Nodes["node0"].PodInfos)
		})
	}
}

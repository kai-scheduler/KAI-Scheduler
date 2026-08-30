// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package backgroundpods_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/backgroundpods"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const (
	backgroundLabel = "kai.scheduler/background"
	nodeName        = "node-0"
)

// buildSession assembles a single 8-GPU node holding the given jobs, and labels every pod whose
// job name starts with "background" so the plugin's default selector matches it.
func buildSession(t *testing.T, jobs []*jobs_fake.TestJobBasic) *framework.Session {
	t.Helper()
	test_utils.InitTestingInfrastructure()

	topology := test_utils.TestTopologyBasic{
		Name: t.Name(),
		Jobs: jobs,
		Nodes: map[string]nodes_fake.TestNodeBasic{
			nodeName: {GPUs: 8},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue", DeservedGPUs: 8, GPUOverQuotaWeight: 1},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: 10},
		},
	}

	ssn := test_utils.BuildSession(topology, gomock.NewController(t))

	for _, node := range ssn.ClusterInfo.Nodes {
		for _, podInfo := range node.PodInfos {
			if isBackgroundJob(string(podInfo.Job)) {
				if podInfo.Pod.Labels == nil {
					podInfo.Pod.Labels = map[string]string{}
				}
				podInfo.Pod.Labels[backgroundLabel] = "true"
			}
		}
	}

	return ssn
}

func isBackgroundJob(jobID string) bool {
	return len(jobID) >= len("background") && jobID[:len("background")] == "background"
}

func backgroundJob(name string, gpus float64) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		Namespace:           "default",
		QueueName:           "queue",
		RequiredGPUsPerTask: gpus,
		Tasks: []*tasks_fake.TestTaskBasic{
			{Name: name + "-0", State: pod_status.Running, NodeName: nodeName},
		},
	}
}

func userJob(name string, gpus float64, state pod_status.PodStatus) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		Namespace:           "default",
		QueueName:           "queue",
		RequiredGPUsPerTask: gpus,
		Tasks: []*tasks_fake.TestTaskBasic{
			{Name: name + "-0", State: state, NodeName: nodeName},
		},
	}
}

func newPlugin() framework.Plugin {
	return backgroundpods.New(framework.PluginArguments{})
}

func podByName(t *testing.T, ssn *framework.Session, name string) *pod_info.PodInfo {
	t.Helper()
	for _, node := range ssn.ClusterInfo.Nodes {
		for _, podInfo := range node.PodInfos {
			if podInfo.Name == name {
				return podInfo
			}
		}
	}
	t.Fatalf("pod %q not found on any node", name)
	return nil
}

// TestVirtualEvictionReleasesCapacity checks that opening a session moves background capacity into
// the node's releasing vector, leaving idle untouched but the node fully available.
func TestVirtualEvictionReleasesCapacity(t *testing.T) {
	ssn := buildSession(t, []*jobs_fake.TestJobBasic{
		backgroundJob("background-job", 1),
	})
	node := ssn.ClusterInfo.Nodes[nodeName]

	require.Equal(t, 7.0, node.IdleVector.Get(resource_info.GPUIndex))
	require.Equal(t, 0.0, node.ReleasingVector.Get(resource_info.GPUIndex))

	newPlugin().OnSessionOpen(ssn)

	require.Equal(t, 7.0, node.IdleVector.Get(resource_info.GPUIndex),
		"idle should not change: the pod has not actually gone anywhere")
	require.Equal(t, 1.0, node.ReleasingVector.Get(resource_info.GPUIndex))
	require.Equal(t, 8.0, node.NonAllocatedResource("nvidia.com/gpu"),
		"the whole node should look available to the session")
	require.Equal(t, pod_status.Releasing, podByName(t, ssn, "background-job-0").Status)
}

// TestBackgroundPodRestoredWhenNodeHasRoom checks the common case: the session placed a workload
// that does not need the background pod's capacity, so it gets its place back.
func TestBackgroundPodRestoredWhenNodeHasRoom(t *testing.T) {
	ssn := buildSession(t, []*jobs_fake.TestJobBasic{
		backgroundJob("background-job", 1),
		userJob("user-job", 2, pod_status.Running),
	})
	node := ssn.ClusterInfo.Nodes[nodeName]

	plugin := newPlugin()
	plugin.OnSessionOpen(ssn)
	plugin.OnSessionClose(ssn)

	require.Equal(t, pod_status.Running, podByName(t, ssn, "background-job-0").Status,
		"background pod should be restored, not evicted")
	require.Equal(t, 5.0, node.IdleVector.Get(resource_info.GPUIndex))
	require.Equal(t, 0.0, node.ReleasingVector.Get(resource_info.GPUIndex))
}

// TestBackgroundPodEvictedWhenDisplaced checks that a background pod whose capacity was taken by a
// pipelined workload is evicted for real.
func TestBackgroundPodEvictedWhenDisplaced(t *testing.T) {
	ssn := buildSession(t, []*jobs_fake.TestJobBasic{
		backgroundJob("background-job", 1),
		userJob("user-job", 8, pod_status.Pipelined),
	})

	plugin := newPlugin()
	plugin.OnSessionOpen(ssn)

	node := ssn.ClusterInfo.Nodes[nodeName]
	require.Equal(t, 0.0, node.NonAllocatedResource("nvidia.com/gpu"),
		"the pipelined pod should have claimed everything the background pod was holding")

	plugin.OnSessionClose(ssn)

	require.Equal(t, pod_status.Releasing, podByName(t, ssn, "background-job-0").Status,
		"background pod should stay evicted")
}

// TestOnlyDisplacedBackgroundPodsAreEvicted checks that displacement is limited to what the
// workload actually needed: a 7-GPU pod on an 8-GPU node with two background pods costs one of
// them, not both.
func TestOnlyDisplacedBackgroundPodsAreEvicted(t *testing.T) {
	ssn := buildSession(t, []*jobs_fake.TestJobBasic{
		backgroundJob("background-a", 1),
		backgroundJob("background-b", 1),
		userJob("user-job", 7, pod_status.Pipelined),
	})

	plugin := newPlugin()
	plugin.OnSessionOpen(ssn)
	plugin.OnSessionClose(ssn)

	first := podByName(t, ssn, "background-a-0")
	second := podByName(t, ssn, "background-b-0")

	require.Equal(t, pod_status.Running, first.Status,
		"the first background pod in sort order should keep its place")
	require.Equal(t, pod_status.Releasing, second.Status,
		"only the second should be displaced")
}

// TestUnlabelledPodsAreIgnored checks that the plugin does not touch pods the selector misses.
func TestUnlabelledPodsAreIgnored(t *testing.T) {
	ssn := buildSession(t, []*jobs_fake.TestJobBasic{
		userJob("user-job", 1, pod_status.Running),
	})
	node := ssn.ClusterInfo.Nodes[nodeName]

	plugin := newPlugin()
	plugin.OnSessionOpen(ssn)

	require.Equal(t, 0.0, node.ReleasingVector.Get(resource_info.GPUIndex))
	require.Equal(t, pod_status.Running, podByName(t, ssn, "user-job-0").Status)

	plugin.OnSessionClose(ssn)
	require.Equal(t, pod_status.Running, podByName(t, ssn, "user-job-0").Status)
}

// TestCustomSelector checks that the label selector is configurable.
func TestCustomSelector(t *testing.T) {
	ssn := buildSession(t, []*jobs_fake.TestJobBasic{
		backgroundJob("background-job", 1),
	})
	node := ssn.ClusterInfo.Nodes[nodeName]

	plugin := backgroundpods.New(framework.PluginArguments{"labelSelector": "some.other/label=yes"})
	plugin.OnSessionOpen(ssn)

	require.Equal(t, 0.0, node.ReleasingVector.Get(resource_info.GPUIndex),
		"a selector that matches nothing should evict nothing")
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package accumulated_scenario_filters

import (
	"fmt"
	"testing"

	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	commonconstants "github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

const fractionalVictimsPodGroup = "fractional-victims"

type sharedGpuCase struct {
	fraction   string
	podsPerGpu int
	gpuMemory  string // nvidia.com/gpu.memory label in MiB; empty leaves it unset, so every GPU counts as 100 memory units
}

func (c sharedGpuCase) String() string {
	return fmt.Sprintf("%s x%d memory=%q", c.fraction, c.podsPerGpu, c.gpuMemory)
}

var sharedGpuCases = []sharedGpuCase{
	{"0.14", 1, ""}, {"0.14", 3, ""}, {"0.14", 7, ""},
	{"0.125", 1, ""}, {"0.125", 7, ""}, {"0.25", 3, ""}, {"0.5", 1, ""},
	{"0.14", 7, "183359"}, {"0.125", 7, "183359"}, {"0.5", 2, "183359"},
}

// newSharedGpuNode returns an 8-GPU node whose GPUs are each shared by c.podsPerGpu running pods requesting c.fraction,
// plus those pods.
func newSharedGpuNode(t *testing.T, c sharedGpuCase) (*node_info.NodeInfo, []*pod_info.PodInfo) {
	controller := gomock.NewController(t)
	podAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
	podAffinityInfo.EXPECT().AddPod(gomock.Any()).AnyTimes()
	podAffinityInfo.EXPECT().RemovePod(gomock.Any()).AnyTimes()

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "n1", Labels: map[string]string{}},
		Status: v1.NodeStatus{
			Allocatable: v1.ResourceList{resource_info.GPUResourceName: resource.MustParse("8")},
			Capacity:    v1.ResourceList{resource_info.GPUResourceName: resource.MustParse("8")},
		},
	}
	if c.gpuMemory != "" {
		node.Labels[node_info.GpuMemoryLabel] = c.gpuMemory
	}
	vectorMap := resource_info.BuildResourceVectorMap([]v1.ResourceList{node.Status.Allocatable})
	nodeInfo := node_info.NewNodeInfo(node, podAffinityInfo, vectorMap)

	var victims []*pod_info.PodInfo
	for gpu := range 8 {
		for i := range c.podsPerGpu {
			name := fmt.Sprintf("frac-%d-%d", gpu, i)
			task := pod_info.NewTaskInfo(&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID:       types.UID(name),
					Name:      name,
					Namespace: "data",
					Annotations: map[string]string{
						commonconstants.GpuFraction:              c.fraction,
						commonconstants.PodGroupAnnotationForPod: fractionalVictimsPodGroup,
					},
				},
				Spec:   v1.PodSpec{NodeName: "n1", Containers: []v1.Container{{}}},
				Status: v1.PodStatus{Phase: v1.PodRunning},
			}, vectorMap)
			task.SetFractionalGpuGroups([]schedulingv1alpha2.FractionalGpuGroup{{ID: fmt.Sprintf("group-%d", gpu)}})
			if err := nodeInfo.AddTask(task); err != nil {
				t.Fatalf("AddTask(%s): %v", name, err)
			}
			victims = append(victims, task)
		}
	}
	return nodeInfo, victims
}

func newWholeNodeScenario(victims []*pod_info.PodInfo) *scenario.ByNodeScenario {
	pending := pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "gang-worker", Name: "gang-worker", Namespace: "m4"},
		Spec: v1.PodSpec{Containers: []v1.Container{{
			Resources: v1.ResourceRequirements{Requests: v1.ResourceList{resource_info.GPUResourceName: resource.MustParse("8")}},
		}}},
	}, resource_info.NewResourceVectorMap())
	session := &framework.Session{ClusterInfo: &api.ClusterInfo{PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
		common_info.NewPodGroupID("data", fractionalVictimsPodGroup): podgroup_info.NewPodGroupInfo(
			common_info.NewPodGroupID("data", fractionalVictimsPodGroup)),
	}}}
	return scenario.NewByNodeScenario(session, nil, []*pod_info.PodInfo{pending}, victims, []*podgroup_info.PodGroupInfo{})
}

// Evicting every fractional pod on a node must make the whole node available to an 8-GPU task, whatever the fraction
// and the GPU memory the node reports.
func TestIdleGpusFilter_EvictingAllFractionalPodsFreesWholeNode(t *testing.T) {
	for _, c := range sharedGpuCases {
		t.Run(c.String(), func(t *testing.T) {
			nodeInfo, victims := newSharedGpuNode(t, c)
			nodes := map[string]*node_info.NodeInfo{"n1": nodeInfo}

			filter := NewIdleGpusFilter(newWholeNodeScenario(nil), nodes)
			if filter == nil {
				t.Fatal("NewIdleGpusFilter returned nil")
			}
			if fits, err := filter.Filter(newWholeNodeScenario(victims[:len(victims)-1])); err != nil || fits {
				t.Fatalf("with one sharing pod left the node must not fit 8 GPUs: fits=%v err=%v", fits, err)
			}
			if fits, err := filter.Filter(newWholeNodeScenario(victims)); err != nil || !fits {
				t.Fatalf("with every sharing pod evicted the node must fit 8 GPUs: fits=%v err=%v idle=%v",
					fits, err, filter.nodesNameToIdleGpus["n1"])
			}
		})
	}
}

// The GPUs credited for evicting every sharing pod must add back up to what the node accounts as used.
func TestSharedGpuEvictions_AddUpToWholeGpus(t *testing.T) {
	for _, c := range sharedGpuCases {
		t.Run(c.String(), func(t *testing.T) {
			nodeInfo, victims := newSharedGpuNode(t, c)
			idle := nodeIdleOrReleasingGpuCapacity(nodeInfo)
			evictions := node_info.NewSharedGpuEvictions(map[string]*node_info.NodeInfo{"n1": nodeInfo})
			for _, victim := range victims {
				idle += evictions.Credit(victim)
			}
			if !resource_info.LessOrEqualWithTolerance(8, idle) || !resource_info.LessOrEqualWithTolerance(idle, 8) {
				t.Fatalf("idle GPUs after evicting every fractional pod = %v, want 8", idle)
			}
		})
	}
}

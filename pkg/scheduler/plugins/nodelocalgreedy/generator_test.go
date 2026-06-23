// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodelocalgreedy

import (
	"fmt"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	schedulingv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/multinodegang"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

type SolveContext = solvers.SolveContext
type GenerateVictimsQueue = solvers.GenerateVictimsQueue

func TestNodeLocalGreedyEmitsRecordedVictimsBeforePotentialVictims(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1, "node-2": 1})
	recordedJob, recordedTasks := addGeneratorTestJob(t, ssn, 1, 1, "team-recorded", "node-1")
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 1, 2, "team-victim", "node-2")
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	generator := NewNodeLocalGreedyGenerator(&SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		RecordedVictimsJobs:  []*podgroup_info.PodGroupInfo{recordedJob},
		RecordedVictimsTasks: recordedTasks,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, victimJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	require.Equal(t, constants.GeneratorNodeLocalGreedy, generator.Name())

	sn := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(recordedTasks), podNames(sn.RecordedVictimsTasks()))
	require.Empty(t, sn.PotentialVictimsTasks())

	sn = requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(recordedTasks), podNames(sn.RecordedVictimsTasks()))
	require.ElementsMatch(t, podNames(victimTasks), podNames(sn.PotentialVictimsTasks()))
	require.Nil(t, generator.Next())
}

func TestNodeLocalGreedyKeepsWholeVictimJobs(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1, "node-2": 1})
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 2, 1, "team-victim", "node-1", "node-2")
	setGeneratorTestMinAvailable(victimJob, 2)
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	generator := NewNodeLocalGreedyGenerator(&SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, victimJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	sn := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(victimTasks), podNames(sn.PotentialVictimsTasks()))
	for _, task := range victimTasks {
		representative := sn.GetVictimJobRepresentativeById(task)
		require.NotNil(t, representative)
		require.Len(t, representative.GetAllPodsMap(), len(victimTasks))
	}
	require.Nil(t, generator.Next())
}

func TestNodeLocalGreedyKeepsVictimRepresentativesPerJob(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 2})
	firstJob, firstTasks := addGeneratorTestJob(t, ssn, 1, 1, "team-first", "node-1")
	secondJob, secondTasks := addGeneratorTestJob(t, ssn, 1, 2, "team-second", "node-1")
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	generator := NewNodeLocalGreedyGenerator(&SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, firstJob, secondJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	sn := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(secondTasks), podNames(sn.PotentialVictimsTasks()))

	for _, task := range secondTasks {
		representative := sn.GetVictimJobRepresentativeById(task)
		require.NotNil(t, representative)
		require.Equal(t, secondJob.UID, representative.UID)
		require.ElementsMatch(t, podNames(secondTasks), podNamesFromMap(representative.GetAllPodsMap()))
	}

	sn = requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, append(podNames(firstTasks), podNames(secondTasks)...), podNames(sn.PotentialVictimsTasks()))
	for _, task := range firstTasks {
		representative := sn.GetVictimJobRepresentativeById(task)
		require.NotNil(t, representative)
		require.Equal(t, firstJob.UID, representative.UID)
		require.ElementsMatch(t, podNames(firstTasks), podNamesFromMap(representative.GetAllPodsMap()))
	}
	for _, task := range secondTasks {
		representative := sn.GetVictimJobRepresentativeById(task)
		require.NotNil(t, representative)
		require.Equal(t, secondJob.UID, representative.UID)
		require.ElementsMatch(t, podNames(secondTasks), podNamesFromMap(representative.GetAllPodsMap()))
	}
}

func TestNodeLocalGreedyPreservesAccumulatedVictimQueueProgression(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1, "node-2": 1})
	firstJob, firstTasks := addGeneratorTestJob(t, ssn, 1, 1, "team-first", "node-1")
	secondJob, secondTasks := addGeneratorTestJob(t, ssn, 1, 2, "team-second", "node-2")
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	generator := NewNodeLocalGreedyGenerator(&SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, firstJob, secondJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	first := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(secondTasks), podNames(first.PotentialVictimsTasks()))

	second := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(firstTasks), podNames(second.PotentialVictimsTasks()))
	require.Nil(t, generator.Next())
}

func TestNodeLocalGreedyOrdersCandidatesBySmallestUsefulNode(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1, "node-2": 2})
	oneGpuJob, oneGpuTasks := addGeneratorTestJob(t, ssn, 1, 1, "team-small", "node-1")
	twoGpuJob, twoGpuTasks := addGeneratorTestJob(t, ssn, 2, 2, "team-large", "node-2", "node-2")
	setGeneratorTestMinAvailable(twoGpuJob, 2)
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	generator := NewNodeLocalGreedyGenerator(&SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, twoGpuJob, oneGpuJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	first := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(oneGpuTasks), podNames(first.PotentialVictimsTasks()))

	second := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(twoGpuTasks), podNames(second.PotentialVictimsTasks()))
	require.Nil(t, generator.Next())
}

func TestNodeLocalGreedyFiltersInsufficientAccumulatedScenarios(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1, "node-2": 2})
	oneGpuJob, _ := addGeneratorTestJob(t, ssn, 1, 1, "team-small", "node-1")
	twoGpuJob, twoGpuTasks := addGeneratorTestJob(t, ssn, 2, 2, "team-large", "node-2", "node-2")
	setGeneratorTestMinAvailable(twoGpuJob, 2)
	pendingJob := addGeneratorTestPendingJob(t, ssn, 2, 10, "team-pending")
	setGeneratorTestMinAvailable(pendingJob, 2)

	generator := NewNodeLocalGreedyGenerator(&SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, oneGpuJob, twoGpuJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	first := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(twoGpuTasks), podNames(first.PotentialVictimsTasks()))
	require.Nil(t, generator.Next())
}

func TestNodeLocalGreedyReturnsNilWhenVictimQueueExhausted(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1})
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	generator := NewNodeLocalGreedyGenerator(&SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	require.Nil(t, generator.Next())
}

func TestNodeLocalGreedyDoesNotBuildScenariosUntilNext(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1})
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 1, 1, "team-victim", "node-1")
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
	queueBuilds := 0

	generator := NewNodeLocalGreedyGenerator(&SolveContext{
		Session:           ssn,
		ActionType:        framework.Reclaim,
		PartialPendingJob: pendingJob,
		GenerateVictimsQueue: func() *utils.JobsOrderByQueues {
			queueBuilds++
			return generatorTestVictimsQueue(ssn, victimJob)
		},
		FeasibleNodes: ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	require.Zero(t, queueBuilds)

	sn := requireByNodeScenario(t, generator.Next())
	require.Equal(t, 1, queueBuilds)
	require.ElementsMatch(t, podNames(victimTasks), podNames(sn.PotentialVictimsTasks()))

	require.Nil(t, generator.Next())
	require.Equal(t, 1, queueBuilds)
}

func TestNodeLocalGreedyUsesIndependentVictimsQueue(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1})
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 1, 1, "team-victim", "node-1")
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
	ctx := &SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, victimJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	}

	nodeLocal := NewNodeLocalGreedyGenerator(ctx)
	require.NotNil(t, nodeLocal)
	require.NotNil(t, nodeLocal.Next())

	multiNode := multinodegang.NewMultiNodeGangGenerator(ctx)
	require.NotNil(t, multiNode)
	sn := requireByNodeScenario(t, multiNode.Next())
	require.ElementsMatch(t, podNames(victimTasks), podNames(sn.PotentialVictimsTasks()))
}

func TestNodeLocalGreedyRequeuesElasticVictims(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 3})
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 3, 1, "team-victim", "node-1", "node-1", "node-1")
	setGeneratorTestMinAvailable(victimJob, 1)
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	generator := NewNodeLocalGreedyGenerator(&SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, victimJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	firstEvictableTasks, _ := podgroup_info.GetTasksToEvict(victimJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn)

	sn := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(firstEvictableTasks), podNames(sn.PotentialVictimsTasks()))

	sn = requireByNodeScenario(t, generator.Next())
	require.NotEmpty(t, sn.PotentialVictimsTasks())
	require.Subset(t, podNames(victimTasks), podNames(sn.PotentialVictimsTasks()))
	for next := generator.Next(); next != nil; next = generator.Next() {
		sn = requireByNodeScenario(t, next)
		require.NotEmpty(t, sn.PotentialVictimsTasks())
		require.Subset(t, podNames(victimTasks), podNames(sn.PotentialVictimsTasks()))
	}
}

func TestNodeLocalGreedyRequeuesRecordedOverlap(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 3})
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 3, 1, "team-victim", "node-1", "node-1", "node-1")
	setGeneratorTestMinAvailable(victimJob, 1)
	recordedTasks, _ := podgroup_info.GetTasksToEvict(victimJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn)
	recordedJob := victimJob.CloneWithTasks(recordedTasks)
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	generator := NewNodeLocalGreedyGenerator(&SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		RecordedVictimsJobs:  []*podgroup_info.PodGroupInfo{recordedJob},
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, victimJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	sn := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(recordedTasks), podNames(sn.RecordedVictimsTasks()))
	require.Empty(t, sn.PotentialVictimsTasks())

	sn = requireByNodeScenario(t, generator.Next())
	require.NotEmpty(t, sn.PotentialVictimsTasks())
	require.Subset(t, podNames(victimTasks), podNames(sn.PotentialVictimsTasks()))
	for next := generator.Next(); next != nil; next = generator.Next() {
		sn = requireByNodeScenario(t, next)
		require.NotEmpty(t, sn.PotentialVictimsTasks())
		require.Subset(t, podNames(victimTasks), podNames(sn.PotentialVictimsTasks()))
	}
}

func TestScenarioGeneratorConstructorsRejectMalformedContext(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1})
	victimJob, _ := addGeneratorTestJob(t, ssn, 1, 1, "team-victim", "node-1")
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	valid := &SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, victimJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	}
	tests := []struct {
		name string
		ctx  framework.ScenarioGeneratorContext
	}{
		{name: "nil context", ctx: nil},
		{name: "wrong context type", ctx: emptyGeneratorContext{}},
		{name: "nil session", ctx: cloneSolveContext(valid, func(ctx *SolveContext) { ctx.Session = nil })},
		{name: "nil cluster info", ctx: cloneSolveContext(valid, func(ctx *SolveContext) { ctx.Session = &framework.Session{} })},
		{name: "nil nodes", ctx: cloneSolveContext(valid, func(ctx *SolveContext) {
			ctx.Session = newGeneratorTestSession(t, map[string]int{"node-1": 1})
			ctx.Session.ClusterInfo.Nodes = nil
		})},
		{name: "nil pod group infos", ctx: cloneSolveContext(valid, func(ctx *SolveContext) {
			ctx.Session = newGeneratorTestSession(t, map[string]int{"node-1": 1})
			ctx.Session.ClusterInfo.PodGroupInfos = nil
		})},
		{name: "nil pending job", ctx: cloneSolveContext(valid, func(ctx *SolveContext) { ctx.PartialPendingJob = nil })},
		{name: "nil feasible nodes", ctx: cloneSolveContext(valid, func(ctx *SolveContext) { ctx.FeasibleNodes = nil })},
		{name: "nil victim queue source", ctx: cloneSolveContext(valid, func(ctx *SolveContext) { ctx.GenerateVictimsQueue = nil })},
		{name: "fallback-only victim queue", ctx: cloneSolveContext(valid, func(ctx *SolveContext) {
			ctx.GenerateVictimsQueue = nil
			ctx.VictimsQueue = generatorTestVictimsQueue(ssn, victimJob)
		})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Nil(t, NewNodeLocalGreedyGenerator(tt.ctx))
			require.Nil(t, multinodegang.NewMultiNodeGangGenerator(tt.ctx))
		})
	}
}

func TestMultiNodeGangPreservesWholeGangVictims(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1, "node-2": 1})
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 2, 1, "team-victim", "node-1", "node-2")
	setGeneratorTestMinAvailable(victimJob, 2)
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	generator := multinodegang.NewMultiNodeGangGenerator(&SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn, victimJob),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	})

	require.NotNil(t, generator)
	require.Equal(t, constants.GeneratorMultiNodeGang, generator.Name())

	sn := requireByNodeScenario(t, generator.Next())
	require.ElementsMatch(t, podNames(victimTasks), podNames(sn.PotentialVictimsTasks()))
	for _, task := range victimTasks {
		representative := sn.GetVictimJobRepresentativeById(task)
		require.NotNil(t, representative)
		require.Len(t, representative.GetAllPodsMap(), len(victimTasks))
	}
}

func newGeneratorTestSession(t *testing.T, nodeGPUs map[string]int) *framework.Session {
	t.Helper()

	defaultQueue := createQueue("default")
	defaultQueue.ParentQueue = ""

	return &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{},
			Queues: map[common_info.QueueID]*queue_info.QueueInfo{
				defaultQueue.UID: defaultQueue,
			},
			Nodes: newGeneratorTestNodes(t, nodeGPUs),
		},
	}
}

func newGeneratorTestNodes(t *testing.T, nodeGPUs map[string]int) map[string]*node_info.NodeInfo {
	t.Helper()

	resourceLists := make([]v1.ResourceList, 0, len(nodeGPUs))
	for _, gpus := range nodeGPUs {
		resourceLists = append(resourceLists, generatorTestNodeResources(gpus))
	}
	vectorMap := resource_info.BuildResourceVectorMap(resourceLists)

	nodes := map[string]*node_info.NodeInfo{}
	for name, gpus := range nodeGPUs {
		controller := gomock.NewController(t)
		nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
		nodePodAffinityInfo.EXPECT().AddPod(gomock.Any()).AnyTimes()
		nodePodAffinityInfo.EXPECT().RemovePod(gomock.Any()).AnyTimes()

		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: v1.NodeStatus{
				Allocatable: generatorTestNodeResources(gpus),
				Capacity:    generatorTestNodeResources(gpus),
			},
		}
		nodes[name] = node_info.NewNodeInfo(node, nodePodAffinityInfo, vectorMap)
	}
	return nodes
}

func generatorTestNodeResources(gpus int) v1.ResourceList {
	return v1.ResourceList{
		resource_info.GPUResourceName: resource.MustParse(strconv.Itoa(gpus)),
		v1.ResourcePods:               resource.MustParse("100"),
	}
}

func createQueue(queueName string) *queue_info.QueueInfo {
	return queue_info.NewQueueInfo(&schedulingv2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: queueName,
		},
		Spec: schedulingv2.QueueSpec{
			ParentQueue: "default",
			Resources: &schedulingv2.QueueResources{
				GPU: schedulingv2.QueueResource{
					Quota: 1,
				},
			},
		},
	})
}

func createJobWithTasks(
	tasksPerJob int, jobID int, queueName string, tasksStatus v1.PodPhase, podResources []v1.ResourceRequirements,
) (*podgroup_info.PodGroupInfo, []*pod_info.PodInfo) {
	jobTasks := []*pod_info.PodInfo{}
	namespace := "kai-" + queueName
	jobUID := strconv.Itoa(jobID)

	for taskID := 0; taskID < tasksPerJob; taskID++ {
		taskNum := jobID*tasksPerJob + taskID
		jobTasks = append(jobTasks, pod_info.NewTaskInfo(
			buildPod(
				strconv.Itoa(taskNum),
				fmt.Sprintf("pod-%d", taskNum),
				namespace,
				jobUID,
				tasksStatus,
				podResources,
			),
			resource_info.NewResourceVectorMap(),
		))
	}

	newJob := podgroup_info.NewPodGroupInfo(common_info.PodGroupID(strconv.Itoa(jobID)), jobTasks...)
	newJob.SetPodGroup(&schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("job%d", jobID),
			Namespace: namespace,
			UID:       types.UID(jobUID),
		},
		Spec: schedulingv2alpha2.PodGroupSpec{
			MinMember: ptr.To(int32(1)),
			Queue:     queueName,
		},
	})

	return newJob, jobTasks
}

func buildPod(
	uid, name, namespace, podGroupID string, status v1.PodPhase, resources []v1.ResourceRequirements,
) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(uid),
			Annotations: map[string]string{
				constants.PodGroupAnnotationForPod: podGroupID,
			},
		},
		Spec: v1.PodSpec{
			Containers: func() []v1.Container {
				var containers []v1.Container
				for _, r := range resources {
					containers = append(containers, v1.Container{
						Resources: r,
					})
				}
				return containers
			}(),
		},
		Status: v1.PodStatus{
			Phase: status,
		},
	}

	if status == v1.PodRunning {
		pod.Spec.NodeName = "node-1"
	}

	return pod
}

func requireOneGPU() v1.ResourceRequirements {
	return v1.ResourceRequirements{
		Requests: v1.ResourceList{
			resource_info.GPUResourceName: resource.MustParse("1"),
		},
	}
}

func addGeneratorTestPendingJob(
	t *testing.T, ssn *framework.Session, tasksPerJob int, jobID int, queueName string,
) *podgroup_info.PodGroupInfo {
	t.Helper()

	job, _ := createJobWithTasks(tasksPerJob, jobID, queueName, v1.PodPending, []v1.ResourceRequirements{requireOneGPU()})
	addGeneratorTestQueue(ssn, queueName)
	ssn.ClusterInfo.PodGroupInfos[job.UID] = job
	return job
}

func addGeneratorTestJob(
	t *testing.T, ssn *framework.Session, tasksPerJob int, jobID int, queueName string, nodeNames ...string,
) (*podgroup_info.PodGroupInfo, []*pod_info.PodInfo) {
	t.Helper()

	job, tasks := createJobWithTasks(tasksPerJob, jobID, queueName, v1.PodRunning, []v1.ResourceRequirements{requireOneGPU()})
	addGeneratorTestQueue(ssn, queueName)
	ssn.ClusterInfo.PodGroupInfos[job.UID] = job

	for index, task := range tasks {
		nodeName := nodeNames[index%len(nodeNames)]
		task.NodeName = nodeName
		task.Pod.Spec.NodeName = nodeName
		require.NoError(t, ssn.ClusterInfo.Nodes[nodeName].AddTask(task))
	}
	return job, tasks
}

func addGeneratorTestQueue(ssn *framework.Session, queueName string) {
	queue := createQueue(queueName)
	ssn.ClusterInfo.Queues[queue.UID] = queue
}

func setGeneratorTestMinAvailable(job *podgroup_info.PodGroupInfo, minAvailable int) {
	for _, podSet := range job.GetAllPodSets() {
		podSet.SetMinAvailable(int32(minAvailable))
	}
	job.PodGroup.Spec.MinMember = ptr.To(int32(minAvailable))
}

func generatorTestVictimsQueue(
	ssn *framework.Session, jobs ...*podgroup_info.PodGroupInfo,
) *utils.JobsOrderByQueues {
	victimsQueue := utils.NewJobsOrderByQueues(ssn, utils.JobsOrderInitOptions{
		VictimQueue:       true,
		MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
	})
	for _, job := range jobs {
		victimsQueue.PushJob(job)
	}
	return &victimsQueue
}

func generatorTestVictimsQueueFactory(
	ssn *framework.Session, jobs ...*podgroup_info.PodGroupInfo,
) GenerateVictimsQueue {
	return func() *utils.JobsOrderByQueues {
		return generatorTestVictimsQueue(ssn, jobs...)
	}
}

func cloneSolveContext(ctx *SolveContext, mutate func(*SolveContext)) *SolveContext {
	clone := *ctx
	mutate(&clone)
	return &clone
}

type emptyGeneratorContext struct{}

func (emptyGeneratorContext) Action() framework.ActionType {
	return framework.Reclaim
}

func requireByNodeScenario(t *testing.T, scenarioInfo api.ScenarioInfo) *scenario.ByNodeScenario {
	t.Helper()

	sn, ok := scenarioInfo.(*scenario.ByNodeScenario)
	require.True(t, ok)
	require.NotNil(t, sn)
	return sn
}

func podNames(tasks []*pod_info.PodInfo) []string {
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.Name)
	}
	sort.Strings(names)
	return names
}

func podNamesFromMap(tasks pod_info.PodsMap) []string {
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.Name)
	}
	sort.Strings(names)
	return names
}

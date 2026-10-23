// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deferredresize_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/deferredresize"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const resizerPriority = constants.PriorityInteractivePreemptibleNumber

func deferredTask(nodeName string, actualCPUs float64) *tasks_fake.TestTaskBasic {
	return &tasks_fake.TestTaskBasic{
		State:        pod_status.Running,
		NodeName:     nodeName,
		ResizeStatus: &tasks_fake.TestResizeStatus{ActualCPUs: actualCPUs, Deferred: true},
	}
}

func runningJob(name, nodeName string, priority int32, cpus float64) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		QueueName:           "queue0",
		Priority:            priority,
		RequiredCPUsPerTask: cpus,
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: nodeName}},
	}
}

// baseTopology: node0 (4 cores) holds the resizer (charged 3, enacted 1) and a 2-core victim.
func baseTopology() test_utils.TestTopologyBasic {
	return test_utils.TestTopologyBasic{
		Name: "deferred resize",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "resizer",
				QueueName:           "queue0",
				Priority:            resizerPriority,
				RequiredCPUsPerTask: 3,
				Tasks:               []*tasks_fake.TestTaskBasic{deferredTask("node0", 1)},
			},
			runningJob("victim", "node0", constants.PriorityTrainNumber, 2),
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {CPUMillis: 4},
			"node1": {CPUMillis: 4},
		},
		Queues:                   []test_utils.TestQueueBasic{{Name: "queue0"}},
		Mocks:                    &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: 10}},
		DeferredResizePreemption: true,
	}
}

func buildSession(t *testing.T, topology test_utils.TestTopologyBasic) *framework.Session {
	test_utils.InitTestingInfrastructure()
	controller := gomock.NewController(t)
	return test_utils.BuildSession(topology, controller)
}

func lowerPriorityOnNode(ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo, nodeName string) func(*podgroup_info.PodGroupInfo) bool {
	return func(job *podgroup_info.PodGroupInfo) bool {
		return job.UID != preemptor.UID && job.Priority < preemptor.Priority && job.HasEvictableTasks() &&
			deferredresize.HasActiveAllocatedTaskOnNode(job, nodeName)
	}
}

func preemptSolve(ssn *framework.Session) deferredresize.Solve {
	return withSolver(ssn, func(job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) *solvers.JobSolver {
		filter := lowerPriorityOnNode(ssn, job, node.Name)
		victims := utils.NewCachedVictimsQueueGenerator(ssn,
			func() map[common_info.PodGroupID]*podgroup_info.PodGroupInfo {
				return utils.GetVictimCandidates(ssn, filter)
			},
			utils.JobsOrderInitOptions{FilterNonPreemptible: true, FilterNonActiveAllocated: true},
		)
		return solvers.NewJobsSolver([]*node_info.NodeInfo{node}, ssn.PreemptScenarioValidator, victims,
			framework.Preempt, nil).PinFeasibleNodes()
	})
}

func withSolver(
	ssn *framework.Session, newSolver func(*podgroup_info.PodGroupInfo, *node_info.NodeInfo) *solvers.JobSolver,
) deferredresize.Solve {
	return func(
		job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo, task *pod_info.PodInfo,
	) (*framework.Statement, *solvers.SearchResult) {
		_, stmt, _, result := newSolver(job, node).SolveTasksWithResult(ssn, job, []*pod_info.PodInfo{task})
		return stmt, result
	}
}

// noGeneratorSolve runs a solver that has no victims generator, recording the nodes it ran for.
func noGeneratorSolve(ssn *framework.Session, tried *[]string) deferredresize.Solve {
	return withSolver(ssn, func(_ *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) *solvers.JobSolver {
		*tried = append(*tried, node.Name)
		return solvers.NewJobsSolver([]*node_info.NodeInfo{node}, nil, nil, framework.Preempt, nil)
	})
}

type taskState struct {
	ptr             *pod_info.PodInfo
	status          pod_status.PodStatus
	nodeName        string
	isVirtualStatus bool
}

type sessionSnapshot struct {
	jobTasks  map[common_info.PodID]taskState
	nodeTasks map[string]map[common_info.PodID]taskState
	idle      map[string][]float64
	used      map[string][]float64
	releasing map[string][]float64
	queueCPU  map[common_info.QueueID]float64
}

func snapshot(ssn *framework.Session) sessionSnapshot {
	s := sessionSnapshot{
		jobTasks:  map[common_info.PodID]taskState{},
		nodeTasks: map[string]map[common_info.PodID]taskState{},
		idle:      map[string][]float64{},
		used:      map[string][]float64{},
		releasing: map[string][]float64{},
		queueCPU:  map[common_info.QueueID]float64{},
	}
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		for uid, task := range job.GetAllPodsMap() {
			s.jobTasks[uid] = taskState{task, task.Status, task.NodeName, task.IsVirtualStatus}
		}
	}
	for name, node := range ssn.ClusterInfo.Nodes {
		s.nodeTasks[name] = map[common_info.PodID]taskState{}
		for key, task := range node.PodInfos {
			s.nodeTasks[name][key] = taskState{task, task.Status, task.NodeName, task.IsVirtualStatus}
		}
		s.idle[name] = append([]float64{}, node.IdleVector...)
		s.used[name] = append([]float64{}, node.UsedVector...)
		s.releasing[name] = append([]float64{}, node.ReleasingVector...)
	}
	for uid, queue := range ssn.ClusterInfo.Queues {
		s.queueCPU[uid] = ssn.QueueAllocatedResources(queue).Get(v1.ResourceCPU)
	}
	return s
}

func requireUnchanged(t *testing.T, before sessionSnapshot, ssn *framework.Session) {
	t.Helper()
	after := snapshot(ssn)
	assert.Equal(t, before.jobTasks, after.jobTasks, "job task objects and states")
	assert.Equal(t, before.nodeTasks, after.nodeTasks, "node task objects and states")
	assert.Equal(t, before.idle, after.idle, "node idle")
	assert.Equal(t, before.used, after.used, "node used")
	assert.Equal(t, before.releasing, after.releasing, "node releasing")
	assert.Equal(t, before.queueCPU, after.queueCPU, "queue allocated cpu")
}

// resizerTasks returns the resizer job and its deferred tasks by node, as the pop-time scan finds them.
func resizerTasks(ssn *framework.Session) (*podgroup_info.PodGroupInfo, map[string][]*pod_info.PodInfo) {
	job := ssn.ClusterInfo.PodGroupInfos["resizer"]
	tasksByNode := map[string][]*pod_info.PodInfo{}
	for _, task := range job.GetAllPodsMap() {
		if pod_info.IsDeferredResizeTask(task) {
			tasksByNode[task.NodeName] = append(tasksByNode[task.NodeName], task)
		}
	}
	return job, tasksByNode
}

func failIfSolverRuns(t *testing.T) deferredresize.Solve {
	return func(
		*podgroup_info.PodGroupInfo, *node_info.NodeInfo, *pod_info.PodInfo,
	) (*framework.Statement, *solvers.SearchResult) {
		t.Fatal("solver must not run")
		return nil, nil
	}
}

func neverStop(*solvers.SearchResult) bool { return false }

func scanTopology() test_utils.TestTopologyBasic {
	topology := baseTopology()
	topology.Jobs = append(topology.Jobs,
		&jobs_fake.TestJobBasic{
			Name:                "multi",
			QueueName:           "queue0",
			Priority:            resizerPriority,
			RequiredCPUsPerTask: 1,
			RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
			Tasks: []*tasks_fake.TestTaskBasic{
				deferredTask("node0", 0.5),
				deferredTask("node1", 0.5),
				deferredTask("node1", 0.5),
				{State: pod_status.Running, NodeName: "node1"},
				{
					State: pod_status.Running, NodeName: "node1",
					ResizeStatus: &tasks_fake.TestResizeStatus{ActualCPUs: 0.5, Infeasible: true},
				},
				deferredTask("missing", 0.5),
			},
		},
		&jobs_fake.TestJobBasic{
			Name:                "no-resize",
			QueueName:           "queue0",
			RequiredCPUsPerTask: 1,
			Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: "node1"}},
		},
	)
	return topology
}

func TestHasDeferredResize(t *testing.T) {
	ssn := buildSession(t, scanTopology())

	assert.True(t, deferredresize.HasDeferredResize(ssn.ClusterInfo.PodGroupInfos["resizer"]))
	assert.True(t, deferredresize.HasDeferredResize(ssn.ClusterInfo.PodGroupInfos["multi"]))
	assert.False(t, deferredresize.HasDeferredResize(ssn.ClusterInfo.PodGroupInfos["no-resize"]))
	assert.False(t, deferredresize.HasDeferredResize(ssn.ClusterInfo.PodGroupInfos["victim"]))
}

func TestNonPendingJobFilter(t *testing.T) {
	ssn := buildSession(t, scanTopology())

	filter := deferredresize.NonPendingJobFilter(ssn)
	require.NotNil(t, filter)
	assert.True(t, filter(ssn.ClusterInfo.PodGroupInfos["resizer"]))
	assert.False(t, filter(ssn.ClusterInfo.PodGroupInfos["no-resize"]))

	ssn.OverrideDeferredResizePreemption(false)
	assert.Nil(t, deferredresize.NonPendingJobFilter(ssn), "flag off keeps the job queue as on main")
}

func TestAttemptForJob_ScansDeferredTasksOnKnownNodesInOrder(t *testing.T) {
	ssn := buildSession(t, scanTopology())
	job := ssn.ClusterInfo.PodGroupInfos["multi"]

	var tried []string
	// Both nodes are short on room, so every deferred task reaches the solver: node0's one, then
	// node1's two. The Infeasible, non-resizing and unknown-node pods are not attempted.
	deferredresize.AttemptForJob(ssn, job, framework.Preempt, noGeneratorSolve(ssn, &tried), neverStop)
	assert.Equal(t, []string{"node0", "node1", "node1"}, tried)
}

func TestAttemptForJob_FeatureDisabled(t *testing.T) {
	ssn := buildSession(t, baseTopology())
	ssn.OverrideDeferredResizePreemption(false)
	before := snapshot(ssn)

	stopped := deferredresize.AttemptForJob(ssn, ssn.ClusterInfo.PodGroupInfos["resizer"], framework.Preempt,
		failIfSolverRuns(t), neverStop)

	assert.False(t, stopped)
	requireUnchanged(t, before, ssn)
}

func TestHasActiveAllocatedTaskOnNode(t *testing.T) {
	topology := baseTopology()
	topology.Jobs = append(topology.Jobs, &jobs_fake.TestJobBasic{
		Name:      "releasing",
		QueueName: "queue0",
		Tasks:     []*tasks_fake.TestTaskBasic{{State: pod_status.Releasing, NodeName: "node0"}},
	})
	ssn := buildSession(t, topology)

	assert.True(t, deferredresize.HasActiveAllocatedTaskOnNode(ssn.ClusterInfo.PodGroupInfos["victim"], "node0"))
	assert.False(t, deferredresize.HasActiveAllocatedTaskOnNode(ssn.ClusterInfo.PodGroupInfos["victim"], "node1"))
	assert.False(t, deferredresize.HasActiveAllocatedTaskOnNode(ssn.ClusterInfo.PodGroupInfos["releasing"], "node0"))
}

func TestAttempt_EvictsOnlyVictimsAndRestoresResizer(t *testing.T) {
	topology := baseTopology()
	// No other node to move the victim to, so it is evicted.
	delete(topology.Nodes, "node1")
	ssn := buildSession(t, topology)
	job, nodeTasks := resizerTasks(ssn)
	resizer := nodeTasks["node0"][0]
	before := snapshot(ssn)

	succeeded, result := deferredresize.Attempt(ssn, job, ssn.ClusterInfo.Nodes["node0"],
		nodeTasks["node0"][0], framework.Preempt, preemptSolve(ssn))

	require.True(t, succeeded)
	require.Equal(t, solvers.SearchResultSolved, result.Reason())

	after := snapshot(ssn)
	assert.Equal(t, before.jobTasks[resizer.UID], after.jobTasks[resizer.UID], "resizer restored in job")
	resizerKey := pod_info.PodKey(resizer.Pod)
	assert.Equal(t, before.nodeTasks["node0"][resizerKey], after.nodeTasks["node0"][resizerKey], "resizer restored on node")
	assert.Equal(t, pod_status.Releasing, ssn.ClusterInfo.PodGroupInfos["victim"].GetAllPodsMap()["victim-0"].Status)
	assert.Equal(t, before.used["node0"], after.used["node0"])
	assert.Equal(t, 2000.0, after.releasing["node0"][0])
	assert.Equal(t, before.queueCPU["queue0"]-2000, after.queueCPU["queue0"])
}

func TestAttempt_NodeAlreadyFits(t *testing.T) {
	topology := baseTopology()
	topology.Jobs[1] = runningJob("victim", "node0", constants.PriorityTrainNumber, 1)
	ssn := buildSession(t, topology)
	job, nodeTasks := resizerTasks(ssn)
	before := snapshot(ssn)

	succeeded, result := deferredresize.Attempt(ssn, job, ssn.ClusterInfo.Nodes["node0"],
		nodeTasks["node0"][0], framework.Preempt, failIfSolverRuns(t))

	assert.True(t, succeeded)
	assert.Nil(t, result)
	requireUnchanged(t, before, ssn)
}

func TestAttempt_FitsWhileEarlierVictimsTerminate(t *testing.T) {
	topology := baseTopology()
	topology.Jobs[1].Tasks[0].State = pod_status.Releasing
	ssn := buildSession(t, topology)
	job, nodeTasks := resizerTasks(ssn)
	before := snapshot(ssn)

	succeeded, _ := deferredresize.Attempt(ssn, job, ssn.ClusterInfo.Nodes["node0"],
		nodeTasks["node0"][0], framework.Preempt, failIfSolverRuns(t))

	assert.True(t, succeeded)
	requireUnchanged(t, before, ssn)
}

func TestAttempt_NoSolutionFromSolveRestoresSession(t *testing.T) {
	ssn := buildSession(t, baseTopology())
	job, nodeTasks := resizerTasks(ssn)
	before := snapshot(ssn)

	var solveSawReleasing bool
	declining := func(
		_ *podgroup_info.PodGroupInfo, _ *node_info.NodeInfo, task *pod_info.PodInfo,
	) (*framework.Statement, *solvers.SearchResult) {
		solveSawReleasing = task.Status == pod_status.Releasing && task.IsVirtualStatus
		return nil, nil
	}
	succeeded, _ := deferredresize.Attempt(ssn, job, ssn.ClusterInfo.Nodes["node0"],
		nodeTasks["node0"][0], framework.Preempt, declining)

	assert.False(t, succeeded)
	assert.True(t, solveSawReleasing, "solve must run with the resizer virtually evicted")
	requireUnchanged(t, before, ssn)
}

func TestAttempt_NoSolutionRestoresSession(t *testing.T) {
	topology := baseTopology()
	topology.Jobs[1].Priority = resizerPriority + 1
	ssn := buildSession(t, topology)
	job, nodeTasks := resizerTasks(ssn)
	before := snapshot(ssn)

	succeeded, result := deferredresize.Attempt(ssn, job, ssn.ClusterInfo.Nodes["node0"],
		nodeTasks["node0"][0], framework.Preempt, preemptSolve(ssn))

	assert.False(t, succeeded)
	assert.NotEqual(t, solvers.SearchResultSolved, result.Reason())
	requireUnchanged(t, before, ssn)
}

func TestAttempt_InsufficientVictimsRestoresSession(t *testing.T) {
	topology := baseTopology()
	topology.Jobs[0].RequiredCPUsPerTask = 5
	ssn := buildSession(t, topology)
	job, nodeTasks := resizerTasks(ssn)
	before := snapshot(ssn)

	succeeded, _ := deferredresize.Attempt(ssn, job, ssn.ClusterInfo.Nodes["node0"],
		nodeTasks["node0"][0], framework.Preempt, preemptSolve(ssn))

	assert.False(t, succeeded)
	requireUnchanged(t, before, ssn)
}

func TestAttemptForJob_SkipsTaskEvictedEarlierInTheAction(t *testing.T) {
	topology := baseTopology()
	// Keeps node0 short even after the resizer's own room is released.
	topology.Jobs = append(topology.Jobs, runningJob("other", "node0", resizerPriority+1, 3))
	// A second running pod keeps the gang secured, so only the scan can skip the evicted pod.
	topology.Jobs[0].RootSubGroupSet = jobs_fake.DefaultSubGroup(1)
	topology.Jobs[0].Tasks = append(topology.Jobs[0].Tasks, &tasks_fake.TestTaskBasic{State: pod_status.Running, NodeName: "node1"})
	ssn := buildSession(t, topology)
	job, nodeTasks := resizerTasks(ssn)

	stmt := ssn.Statement()
	require.NoError(t, stmt.Evict(nodeTasks["node0"][0], "", eviction_info.EvictionMetadata{}))
	require.NoError(t, stmt.Commit())
	before := snapshot(ssn)

	stopped := deferredresize.AttemptForJob(ssn, job, framework.Preempt,
		failIfSolverRuns(t), neverStop)

	assert.False(t, stopped)
	requireUnchanged(t, before, ssn)
}

func TestAttemptForJob_SkipsTaskMovedEarlierInTheSession(t *testing.T) {
	topology := baseTopology()
	topology.Mocks = &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
		NumberOfCacheEvictions: 1, NumberOfPipelineActions: 1,
	}}
	ssn := buildSession(t, topology)
	job, nodeTasks := resizerTasks(ssn)
	resizer := nodeTasks["node0"][0]

	// Consolidation moving the pod to node1, as committed before preempt and reclaim run.
	stmt := ssn.Statement()
	require.NoError(t, stmt.Evict(resizer, "", eviction_info.EvictionMetadata{}))
	require.NoError(t, stmt.Pipeline(resizer, "node1", false))
	require.NoError(t, stmt.Commit())
	moved := job.GetAllPodsMap()[resizer.UID]
	require.Equal(t, pod_status.Pipelined, moved.Status)
	require.True(t, hasDeferredCondition(moved), "the pod still carries the kubelet's Deferred condition")
	before := snapshot(ssn)

	stopped := deferredresize.AttemptForJob(ssn, job, framework.Preempt,
		failIfSolverRuns(t), neverStop)

	assert.False(t, stopped)
	requireUnchanged(t, before, ssn)
}

func hasDeferredCondition(task *pod_info.PodInfo) bool {
	for _, c := range task.Pod.Status.Conditions {
		if c.Type == v1.PodResizePending {
			return c.Reason == v1.PodReasonDeferred
		}
	}
	return false
}

func TestAttempt_RepeatedFailuresKeepSessionStable(t *testing.T) {
	topology := baseTopology()
	topology.Jobs[1].Priority = resizerPriority + 1
	ssn := buildSession(t, topology)
	job, nodeTasks := resizerTasks(ssn)
	before := snapshot(ssn)

	for range 5 {
		succeeded, _ := deferredresize.Attempt(ssn, job, ssn.ClusterInfo.Nodes["node0"],
			nodeTasks["node0"][0], framework.Preempt, preemptSolve(ssn))
		require.False(t, succeeded)
	}
	requireUnchanged(t, before, ssn)
}

func TestAttemptForJob_TriesNodesInOrderAndStops(t *testing.T) {
	topology := baseTopology()
	topology.Jobs[0].Tasks = []*tasks_fake.TestTaskBasic{deferredTask("node0", 1), deferredTask("node1", 1)}
	topology.Jobs = append(topology.Jobs, runningJob("victim1", "node1", constants.PriorityTrainNumber, 2))
	ssn := buildSession(t, topology)
	job, nodeTasks := resizerTasks(ssn)
	require.Len(t, nodeTasks, 2)

	var tried []string
	noGenerator := noGeneratorSolve(ssn, &tried)

	stopped := deferredresize.AttemptForJob(ssn, job, framework.Preempt, noGenerator,
		func(result *solvers.SearchResult) bool { return result.Reason() == solvers.SearchResultNoGenerator })
	assert.True(t, stopped)
	assert.Equal(t, []string{"node0"}, tried)

	tried = nil
	stopped = deferredresize.AttemptForJob(ssn, job, framework.Preempt, noGenerator,
		func(*solvers.SearchResult) bool { return false })
	assert.False(t, stopped)
	assert.Equal(t, []string{"node0", "node1"}, tried)
}

func TestAttemptForJob_EachNodeIndependently(t *testing.T) {
	topology := baseTopology()
	// node0 has a victim, node1 only a higher-priority pod: node0 succeeds, node1 does not block it.
	topology.Jobs[0].Tasks = []*tasks_fake.TestTaskBasic{deferredTask("node0", 1), deferredTask("node1", 1)}
	topology.Jobs = append(topology.Jobs, runningJob("protected", "node1", resizerPriority+1, 2))
	ssn := buildSession(t, topology)
	job := ssn.ClusterInfo.PodGroupInfos["resizer"]

	stopped := deferredresize.AttemptForJob(ssn, job, framework.Preempt,
		preemptSolve(ssn), func(*solvers.SearchResult) bool { return false })

	assert.False(t, stopped)
	assert.Equal(t, pod_status.Releasing, ssn.ClusterInfo.PodGroupInfos["victim"].GetAllPodsMap()["victim-0"].Status)
	assert.Equal(t, pod_status.Running, ssn.ClusterInfo.PodGroupInfos["protected"].GetAllPodsMap()["protected-0"].Status)
	for _, task := range job.GetAllPodsMap() {
		assert.Equal(t, pod_status.Running, task.Status)
		assert.False(t, task.IsVirtualStatus)
	}
}

func TestIsGangSecured(t *testing.T) {
	topology := baseTopology()
	topology.Jobs = append(topology.Jobs,
		&jobs_fake.TestJobBasic{
			Name: "unsecured", QueueName: "queue0", RequiredCPUsPerTask: 1,
			RootSubGroupSet: jobs_fake.DefaultSubGroup(2),
			Tasks: []*tasks_fake.TestTaskBasic{
				{State: pod_status.Running, NodeName: "node1"},
				{State: pod_status.Pending},
			},
		},
		&jobs_fake.TestJobBasic{
			Name: "terminating", QueueName: "queue0", RequiredCPUsPerTask: 1,
			RootSubGroupSet: jobs_fake.DefaultSubGroup(2),
			Tasks: []*tasks_fake.TestTaskBasic{
				{State: pod_status.Running, NodeName: "node1"},
				{State: pod_status.Releasing, NodeName: "node1"},
			},
		},
	)
	ssn := buildSession(t, topology)

	assert.True(t, deferredresize.IsGangSecured(ssn.ClusterInfo.PodGroupInfos["resizer"]))
	assert.False(t, deferredresize.IsGangSecured(ssn.ClusterInfo.PodGroupInfos["unsecured"]))
	assert.False(t, deferredresize.IsGangSecured(ssn.ClusterInfo.PodGroupInfos["terminating"]),
		"pods being deleted do not secure the gang")
}

func TestResizeRepresentativeDropsPendingPods(t *testing.T) {
	topology := baseTopology()
	topology.Jobs[0].RootSubGroupSet = jobs_fake.DefaultSubGroup(1)
	topology.Jobs[0].Tasks = append(topology.Jobs[0].Tasks,
		&tasks_fake.TestTaskBasic{State: pod_status.Running, NodeName: "node1"},
		&tasks_fake.TestTaskBasic{State: pod_status.Pending},
	)
	ssn := buildSession(t, topology)
	job, nodeTasks := resizerTasks(ssn)
	resizing := nodeTasks["node0"][0]

	stmt := ssn.Statement()
	defer stmt.Discard()
	require.NoError(t, stmt.Evict(resizing, "", eviction_info.EvictionMetadata{}))

	representative := deferredresize.ResizeRepresentative(job, []*pod_info.PodInfo{resizing})
	assert.Equal(t, job.UID, representative.UID)
	assert.Equal(t, job.Queue, representative.Queue)
	assert.Zero(t, representative.GetNumPendingTasks())
	assert.Len(t, representative.GetAllPodsMap(), 2, "allocated pod and resizing pod")
	assert.Contains(t, representative.GetAllPodsMap(), resizing.UID)
	assert.Equal(t, 1, job.GetNumPendingTasks(), "the original job is unchanged")
}

func TestAttemptForJob_GangNotSecuredRestoresSession(t *testing.T) {
	topology := baseTopology()
	topology.Jobs[0].RootSubGroupSet = jobs_fake.DefaultSubGroup(2)
	topology.Jobs[0].Tasks = append(topology.Jobs[0].Tasks, &tasks_fake.TestTaskBasic{State: pod_status.Pending})
	ssn := buildSession(t, topology)
	job := ssn.ClusterInfo.PodGroupInfos["resizer"]
	before := snapshot(ssn)

	stopped := deferredresize.AttemptForJob(ssn, job, framework.Preempt,
		failIfSolverRuns(t), neverStop)

	assert.False(t, stopped)
	requireUnchanged(t, before, ssn)
}

// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"fmt"
	"slices"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	schedulingv2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func remainderFixture(size int) (*framework.Session, *podgroup_info.PodGroupInfo, []common_info.PodID) {
	job, tasks := createJobWithTasks(size, 0, "victims", v1.PodRunning, []v1.ResourceRequirements{requireOneGPU()})
	queue := createQueue("victims")
	queue.ParentQueue = ""
	ssn := &framework.Session{ClusterInfo: &api.ClusterInfo{
		PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{job.UID: job},
		Queues:        map[common_info.QueueID]*queue_info.QueueInfo{queue.UID: queue},
	}}
	order := make([]common_info.PodID, len(tasks))
	for i, task := range tasks {
		order[i] = task.UID
	}
	sort.Slice(order, func(i, j int) bool { return order[i] > order[j] })
	return ssn, job, order
}

func remainderBuilder(ssn *framework.Session, job *podgroup_info.PodGroupInfo) *PodAccumulatedScenarioBuilder {
	queue := utils.NewJobsOrderByQueues(ssn, utils.JobsOrderInitOptions{VictimQueue: true, MaxJobsQueueDepth: -1})
	queue.PushJob(job)
	return &PodAccumulatedScenarioBuilder{session: ssn, victimsJobsQueue: &queue, lastScenario: scenario.NewByNodeScenario(ssn, job, nil, nil, nil)}
}

func BenchmarkElasticVictimRequeue(b *testing.B) {
	for _, size := range []int{1024, 2464} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			ssn, job, want := remainderFixture(size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				builder := remainderBuilder(ssn, job)
				for batch, uid := range want {
					if builder.victimsJobsQueue.IsEmpty() {
						b.Fatal("victim queue exhausted early")
					}
					if !builder.addNextPotentialVictims() {
						b.Fatal("unexpected recorded batch")
					}
					tasks := builder.lastScenario.PotentialVictimsTasks()
					if len(tasks) != batch+1 || tasks[batch].UID != uid {
						b.Fatal("victim order or batch boundary changed")
					}
				}
				if !builder.victimsJobsQueue.IsEmpty() {
					b.Fatal("victim queue did not drain")
				}
			}
		})
	}
}

func TestElasticVictimRequeueOrder(t *testing.T) {
	ssn, job, want := remainderFixture(32)
	builder := remainderBuilder(ssn, job)
	for batch, uid := range want {
		require.True(t, builder.addNextPotentialVictims())
		tasks := builder.lastScenario.PotentialVictimsTasks()
		require.Len(t, tasks, batch+1)
		require.Equal(t, uid, tasks[batch].UID)
	}

	if !builder.victimsJobsQueue.IsEmpty() {
		t.Fatal("victim queue did not drain")
	}
	if len(job.GetAllPodsMap()) != len(want) {
		t.Fatal("source job changed")
	}
}

// Frozen upstream requeue algorithm is the oracle for victim batches and remainder state.
func legacyAddNextPotentialVictims(asb *PodAccumulatedScenarioBuilder) bool {
	nextVictimJob := asb.victimsJobsQueue.PopNextJob()

	potentialVictimTasks, jobHasMoreTasks := podgroup_info.GetTasksToEvict(
		nextVictimJob, asb.session.SubGroupOrderFn, asb.session.TaskOrderFn,
	)

	// Jump over recorded victims in potential victims generation
	for _, potentialVictimTask := range potentialVictimTasks {
		if _, ok := asb.recordedVictimsTasks[potentialVictimTask.UID]; ok {
			// If any of the tasks of the victim job are recorded victims
			// we still want to evaluate the job again if there are tasks
			// that are not recorded victims yet, like elastic jobs
			var remainingTasks []*pod_info.PodInfo
			for _, task := range nextVictimJob.GetAllPodsMap() {
				if _, ok := asb.recordedVictimsTasks[task.UID]; !ok {
					remainingTasks = append(remainingTasks, task)
				}
			}
			if len(remainingTasks) != 0 {
				jobToPush := nextVictimJob.CloneWithTasks(remainingTasks)
				asb.victimsJobsQueue.PushJob(jobToPush)
				if asb.victimRemainders == nil {
					asb.victimRemainders = make(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo)
				}
				asb.victimRemainders[jobToPush.UID] = jobToPush
			}
			return false
		}
	}

	// A job that yielded no victims will yield none again, so re-pushing it would leave the queue
	// unable to drain. Semi-preemptible jobs sitting at their core are the case that hits this.
	if jobHasMoreTasks && len(potentialVictimTasks) > 0 {
		var remainingTasks []*pod_info.PodInfo
		for _, task := range nextVictimJob.GetAllPodsMap() {
			if !slices.Contains(potentialVictimTasks, task) {
				remainingTasks = append(remainingTasks, task)
			}
		}

		jobToPush := nextVictimJob.CloneWithTasks(remainingTasks)
		asb.victimsJobsQueue.PushJob(jobToPush)
		if asb.victimRemainders == nil {
			asb.victimRemainders = make(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo)
		}
		asb.victimRemainders[jobToPush.UID] = jobToPush
	}

	if asb.lastScenario != nil {
		asb.lastScenario.AddPotentialVictimsTasks(potentialVictimTasks)
	}
	return true
}

func remainderState(job *podgroup_info.PodGroupInfo) string {
	var pods []string
	for _, task := range job.GetAllPodsMap() {
		pods = append(pods, fmt.Sprintf("%s:%v:%s:%v:%v", task.UID, task.Status, task.NodeName, task.ResReqVector, task.AcceptedResourceVector))
	}
	sort.Strings(pods)
	var groups []string
	for name, ps := range job.PodSets {
		groups = append(groups, fmt.Sprintf("%s:%d:%d:%d:%d:%d", name, ps.GetMinAvailable(), ps.GetNumActiveAllocatedTasks(), ps.GetNumActiveUsedTasks(), ps.GetNumAliveTasks(), ps.GetNumPendingTasks()))
	}
	sort.Strings(groups)
	var statuses []string
	for status, tasks := range job.PodStatusIndex {
		statuses = append(statuses, fmt.Sprintf("%v:%d", status, len(tasks)))
	}
	sort.Strings(statuses)
	return fmt.Sprintf("%v/%v/%v/%v/%d/%v/%v", pods, groups, statuses, job.AllocatedVector, job.GetActiveAllocatedTasksCount(), job.GetAliveTasksRequestedGPUs(), job.IsGangSatisfied())
}

func TestVictimRemainderMatchesSuffixCloning(t *testing.T) {
	for _, kind := range []string{"elastic", "gang", "semi", "nested", "nested-semi", "recorded", "recorded-gang", "core-only", "inactive", "fractional", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			ssn, job, _ := remainderFixture(12)
			switch kind {
			case "fractional", "oversized":
				var tasks []*pod_info.PodInfo
				for _, task := range job.GetAllPodsMap() {
					task.ResReqVector = task.ResReqVector.Clone()
					if kind == "fractional" {
						task.ResReqVector.Set(resource_info.GPUIndex, 0.1)
					} else {
						task.ResReqVector.Set(resource_info.MemoryIndex, 1<<54)
					}
					tasks = append(tasks, task)
				}
				job = job.CloneWithTasks(tasks)
				ssn.ClusterInfo.PodGroupInfos[job.UID] = job
			case "gang", "recorded-gang":
				job.PodSets[podgroup_info.DefaultSubGroup].SetMinAvailable(12)
			case "semi":
				job.Preemptibility = schedulingv2alpha2.SemiPreemptible
				job.PodSets[podgroup_info.DefaultSubGroup].SetMinAvailable(3)
			case "core-only":
				job.Preemptibility = schedulingv2alpha2.SemiPreemptible
				job.PodSets[podgroup_info.DefaultSubGroup].SetMinAvailable(12)
			case "nested", "nested-semi":
				tasks := make([]*pod_info.PodInfo, 0)
				for _, task := range job.GetAllPodsMap() {
					tasks = append(tasks, task)
				}
				sort.Slice(tasks, func(i, j int) bool { return tasks[i].UID < tasks[j].UID })
				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.SetMinSubGroup(ptr.To(int32(1)))
				for i := 0; i < 3; i++ {
					branch := subgroup_info.NewSubGroupSet(fmt.Sprint("branch-", i), nil)
					branch.SetMinSubGroup(ptr.To(int32(1)))
					branch.AddPodSet(subgroup_info.NewPodSet(fmt.Sprint("leaf-", i), 2, nil))
					root.AddSubGroup(branch)
				}
				job.RootSubGroupSet = root
				job.PodSets = root.GetDescendantPodSets()
				for i, task := range tasks {
					task.SubGroupName = fmt.Sprint("leaf-", i/5)
				}
				job = job.CloneWithTasks(tasks)
				// The last leaf has only two pods; leave it below its floor to exercise stale eviction.
				job.PodSets["leaf-2"].SetMinAvailable(3)
				if kind == "nested-semi" {
					job.Preemptibility = schedulingv2alpha2.SemiPreemptible
				}
				ssn.ClusterInfo.PodGroupInfos[job.UID] = job
			case "inactive":
				for _, task := range job.GetAllPodsMap() {
					require.NoError(t, job.UpdateTaskStatus(task, pod_status.Pending))
				}
			}
			old, candidate := remainderBuilder(ssn, job), remainderBuilder(ssn, job)
			if kind == "recorded" || kind == "recorded-gang" {
				recorded := map[common_info.PodID]*pod_info.PodInfo{}
				tasks, _ := podgroup_info.GetTasksToEvict(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn)
				recorded[tasks[0].UID] = tasks[0]
				old.recordedVictimsTasks = recorded
				candidate.recordedVictimsTasks = recorded
			}
			source := remainderState(job)
			var escaped []*podgroup_info.PodGroupInfo
			var escapedStates []string
			for step := 0; !old.victimsJobsQueue.IsEmpty(); step++ {
				require.Less(t, step, 32, "queue failed to drain")
				require.False(t, candidate.victimsJobsQueue.IsEmpty())
				require.Equal(t, legacyAddNextPotentialVictims(old), candidate.addNextPotentialVictims())
				oldTasks, tasks := old.lastScenario.PotentialVictimsTasks(), candidate.lastScenario.PotentialVictimsTasks()
				require.Equal(t, len(oldTasks), len(tasks))
				for i := range tasks {
					require.Equal(t, oldTasks[i].UID, tasks[i].UID)
				}
				if kind == "fractional" || kind == "oversized" {
					require.Empty(t, candidate.victimRemainders)
				} else {
					for uid, expected := range old.victimRemainders {
						require.Equal(t, remainderState(expected), remainderState(candidate.victimRemainders[uid]))
					}
				}
				require.Equal(t, source, remainderState(job), "source job changed")
				for i, rep := range escaped {
					require.Equal(t, escapedStates[i], remainderState(rep), "escaped representative changed")
				}
				if len(tasks) > 0 {
					rep := candidate.lastScenario.GetVictimJobRepresentativeById(tasks[len(tasks)-1])
					escaped = append(escaped, rep)
					escapedStates = append(escapedStates, remainderState(rep))
				}
			}
			require.True(t, candidate.victimsJobsQueue.IsEmpty())
		})
	}
}

func TestSiblingBuilderRemainderIsolation(t *testing.T) {
	ssn, job, _ := remainderFixture(16)
	first, second := remainderBuilder(ssn, job), remainderBuilder(ssn, job)
	require.True(t, first.addNextPotentialVictims())
	require.True(t, second.addNextPotentialVictims())
	state := remainderState(first.victimRemainders[job.UID])
	tasks := slices.Clone(first.lastScenario.PotentialVictimsTasks())
	for !second.victimsJobsQueue.IsEmpty() {
		second.addNextPotentialVictims()
	}
	require.Equal(t, state, remainderState(first.victimRemainders[job.UID]))
	require.Equal(t, tasks, first.lastScenario.PotentialVictimsTasks())
}

func TestRemainderPreservesQueueHistoryAndInterleaving(t *testing.T) {
	run := func(legacy bool) ([]string, []string) {
		ssn, job, _ := remainderFixture(8)
		root := createQueue("root")
		root.ParentQueue = ""
		root.ChildQueues = []common_info.QueueID{"victims", "other"}
		ssn.ClusterInfo.Queues[root.UID] = root
		ssn.ClusterInfo.Queues[job.Queue].ParentQueue = root.UID
		otherQueue := createQueue("other")
		otherQueue.ParentQueue = root.UID
		ssn.ClusterInfo.Queues[otherQueue.UID] = otherQueue
		jobs := []*podgroup_info.PodGroupInfo{job}
		for i := 1; i < 3; i++ {
			queueName := "victims"
			if i == 2 {
				queueName = "other"
			}
			next, _ := createJobWithTasks(8, i, queueName, v1.PodRunning, []v1.ResourceRequirements{requireOneGPU()})
			ssn.ClusterInfo.PodGroupInfos[next.UID] = next
			jobs = append(jobs, next)
		}
		ssn.AddVictimOrderFn(func(l, r interface{}) int {
			left, right := l.(*podgroup_info.PodGroupInfo), r.(*podgroup_info.PodGroupInfo)
			if left.GetActiveAllocatedTasksCount() > right.GetActiveAllocatedTasksCount() {
				return -1
			}
			if left.GetActiveAllocatedTasksCount() < right.GetActiveAllocatedTasksCount() {
				return 1
			}
			return 0
		})
		var comparisons []string
		ssn.AddQueueOrderFn(func(l, r *queue_info.QueueInfo, _, _ *podgroup_info.PodGroupInfo, lv, rv []api.QueueVictimResources, _ *int64) int {
			sum := func(victims []api.QueueVictimResources) float64 {
				var total float64
				for _, victim := range victims {
					total += victim.AllocatedVector.Get(resource_info.GPUIndex)
				}
				return total
			}
			left, right := sum(lv), sum(rv)
			comparisons = append(comparisons, fmt.Sprintf("%s:%v:%s:%v", l.UID, left, r.UID, right))
			if left < right {
				return -1
			}
			if left > right {
				return 1
			}
			return 0
		})
		builder := remainderBuilder(ssn, job)
		for _, next := range jobs[1:] {
			builder.victimsJobsQueue.PushJob(next)
		}
		var batches []string
		for step := 0; !builder.victimsJobsQueue.IsEmpty(); step++ {
			require.Less(t, step, 30)
			count := len(builder.lastScenario.PotentialVictimsTasks())
			if legacy {
				legacyAddNextPotentialVictims(builder)
			} else {
				builder.addNextPotentialVictims()
			}
			tasks := builder.lastScenario.PotentialVictimsTasks()[count:]
			var ids []string
			for _, task := range tasks {
				ids = append(ids, fmt.Sprint(task.Job, "/", task.UID))
			}
			batches = append(batches, fmt.Sprint(ids))
		}
		return batches, comparisons
	}
	wantBatches, wantComparisons := run(true)
	batches, comparisons := run(false)
	require.Len(t, batches, 24)
	require.NotEmpty(t, comparisons)
	require.Equal(t, wantBatches, batches)
	require.Equal(t, wantComparisons, comparisons)
}

func TestVictimRemainderSurvivesRejectedSimulations(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 8})
	require.NoError(t, ssn.InitNodeScoringPool())
	job, _ := addGeneratorTestJob(t, ssn, 8, 20, "team-victim", "node-1")
	pending := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
	queue := utils.GetVictimsQueue(ssn, nil)
	builder := NewPodAccumulatedScenarioBuilder(ssn, pending, nil, queue, ssn.ClusterInfo.Nodes)
	source := remainderState(job)
	rejections := 0
	solver := newByPodSolver(ssn.ClusterInfo.Nodes, func(api.ScenarioInfo) bool { rejections++; return false }, false, framework.Reclaim)
	count := 0
	for sn := builder.GetValidAccumulatedScenario(); sn != nil; sn = builder.GetNextAccumulatedScenario() {
		count++
		require.LessOrEqual(t, count, 9)
		var before string
		remainder := builder.victimRemainders[job.UID]
		if remainder != nil {
			before = remainderState(remainder)
		}
		result := solver.solve(ssn, sn)
		require.False(t, result.solved)
		if remainder != nil {
			require.Equal(t, before, remainderState(remainder))
		}
		require.Equal(t, source, remainderState(job), "simulation failed to restore source job")
		require.Equal(t, float64(0), ssn.ClusterInfo.Nodes["node-1"].IdleVector.Get(resource_info.GPUIndex))
		require.Equal(t, 1, pending.GetNumPendingTasks())
	}
	require.Equal(t, 9, count)
	require.Positive(t, rejections)
}

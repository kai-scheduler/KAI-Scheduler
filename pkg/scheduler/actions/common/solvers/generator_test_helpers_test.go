// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

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
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

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

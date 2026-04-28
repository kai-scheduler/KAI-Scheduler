// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobs_fake

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/pointer"

	enginev2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/constants/labels"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/resources_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

type TestJobBasic struct {
	RequiredGPUsPerTask                 float64
	RequiredGpuMemory                   uint64
	RequiredCPUsPerTask                 float64
	RequiredMemoryPerTask               float64
	RequiredMultiFractionDevicesPerTask *uint64
	Priority                            int32
	Name                                string
	Namespace                           string
	QueueName                           string
	IsBestEffortJob                     bool
	JobAgeInMinutes                     int
	DeleteJobInTest                     bool
	JobNotReadyForSsn                   bool
	MinAvailable                        *int32
	Tasks                               []*tasks_fake.TestTaskBasic
	StaleDuration                       *time.Duration
}

func BuildJobsAndTasksMaps(Jobs []*TestJobBasic) (
	map[common_info.PodGroupID]*podgroup_info.PodGroupInfo, map[string]pod_info.PodsMap, map[string]map[string]bool) {
	jobsInfoMap := map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{}
	usedSharedGPUs := map[string]map[string]bool{}
	tasksToNodeMap := map[string]pod_info.PodsMap{}
	allocatedGPUs := map[string]interface{}{}

	sort.SliceStable(Jobs, func(i, j int) bool {
		return Jobs[i].Priority > Jobs[j].Priority
	})

	for jobIndex, job := range Jobs {
		jobName := job.Name

		jobAllocatedResource := resource_info.EmptyResource()
		taskInfos := generateTasks(job, jobAllocatedResource, usedSharedGPUs, allocatedGPUs,
			tasksToNodeMap)

		jobUID := common_info.PodGroupID(jobName)
		queueUID := common_info.QueueID(job.QueueName)
		numberOfJobs := len(Jobs)
		var jobCreationTime time.Time
		if job.JobAgeInMinutes != 0 {
			jobCreationTime = time.Now().Add(time.Minute * time.Duration(job.JobAgeInMinutes) * (-1))
		} else {
			jobCreationTime = time.Now().Add(time.Minute * time.Duration(numberOfJobs-jobIndex) * (-1))
		}

		if job.MinAvailable == nil {
			job.MinAvailable = pointer.Int32(int32(len(job.Tasks)))
		}

		jobInfo := BuildJobInfo(
			jobName, job.Namespace, jobUID, jobAllocatedResource, taskInfos, job.Priority, queueUID, jobCreationTime,
			*job.MinAvailable, job.StaleDuration,
		)
		jobsInfoMap[common_info.PodGroupID(job.Name)] = jobInfo
	}

	return jobsInfoMap, tasksToNodeMap, usedSharedGPUs
}

func BuildJobInfo(
	name, namespace string,
	uid common_info.PodGroupID, allocatedResource *resource_info.Resource, taskInfos []*pod_info.PodInfo,
	priority int32, queueUID common_info.QueueID, jobCreationTime time.Time, minAvailable int32, staleDuration *time.Duration,
) *podgroup_info.PodGroupInfo {
	allTasks := pod_info.PodsMap{}
	taskStatusIndex := map[pod_status.PodStatus]pod_info.PodsMap{}

	for _, taskInfo := range taskInfos {
		allTasks[taskInfo.UID] = taskInfo
		if len(taskStatusIndex[taskInfo.Status]) == 0 {
			taskStatusIndex[taskInfo.Status] = pod_info.PodsMap{}
		}
		taskStatusIndex[taskInfo.Status][taskInfo.UID] = taskInfo
	}

	result := &podgroup_info.PodGroupInfo{
		UID:               uid,
		Name:              name,
		Namespace:         namespace,
		Allocated:         allocatedResource,
		PodInfos:          allTasks,
		PodStatusIndex:    taskStatusIndex,
		Priority:          priority,
		JobFitErrors:      make(enginev2alpha2.UnschedulableExplanations, 0),
		NodesFitErrors:    map[common_info.PodID]*common_info.FitErrors{},
		Queue:             queueUID,
		CreationTimestamp: metav1.Time{Time: jobCreationTime},
		MinAvailable:      minAvailable,
		PodGroup: &enginev2alpha2.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				UID:               types.UID(uid),
				Name:              name,
				Namespace:         namespace,
				CreationTimestamp: metav1.Time{Time: jobCreationTime},
			},
			Spec: enginev2alpha2.PodGroupSpec{
				Queue:     string(queueUID),
				MinMember: minAvailable,
			},
		},
	}
	_ = result.GetActiveAllocatedTasksCount()
	if staleDuration != nil {
		staleTime := time.Now().Add(-1 * *staleDuration)
		result.StalenessInfo.TimeStamp = &staleTime
	}
	return result
}

func generateTasks(job *TestJobBasic, jobAllocatedResource *resource_info.Resource,
	usedSharedGPUs map[string]map[string]bool, allocatedGPUs map[string]interface{},
	tasksToNodeMap map[string]pod_info.PodsMap) []*pod_info.PodInfo {
	taskInfos := []*pod_info.PodInfo{}
	for taskIndex, task := range job.Tasks {
		gpuGroups := tasks_fake.GetTestTaskGPUIndex(task)

		podResourceList, gpuMemory, gpuFraction, gpuGroups :=
			CalcJobAndPodResources(job, jobAllocatedResource, task, gpuGroups,
				usedSharedGPUs)

		podOfTask := createPodOfTask(job, taskIndex, task, podResourceList, gpuFraction,
			gpuMemory, gpuGroups)

		taskInfo := pod_info.NewTaskInfo(podOfTask)
		taskInfo.Status = task.State
		taskInfo.GPUGroups = gpuGroups
		taskInfo.IsLegacyMIGtask = task.IsLegacyMigTask
		taskInfos = append(taskInfos, taskInfo)

		if pod_status.AllocatedStatus(taskInfo.Status) && gpuFraction == "" {
			jobAllocatedResource.Add(resource_info.ResourceFromResourceList(*podResourceList))
		}

		if tasks_fake.IsTaskStartedStatus(taskInfo.Status) {
			gpuName := taskInfo.NodeName + fmt.Sprint(taskInfo.GPUGroups)
			if _, ok := allocatedGPUs[gpuName]; !ok {
				var void interface{}
				allocatedGPUs[gpuName] = void
			}
		}

		if pod_status.IsActiveUsedStatus(task.State) {
			if tasksToNodeMap[task.NodeName] == nil {
				tasksToNodeMap[task.NodeName] = pod_info.PodsMap{}
			}

			tasksToNodeMap[task.NodeName][taskInfo.UID] = taskInfo
		}
	}
	return taskInfos
}

func CalcJobAndPodResources(job *TestJobBasic, jobAllocatedResource *resource_info.Resource,
	task *tasks_fake.TestTaskBasic, gpuGroups []string,
	usedSharedGPUs map[string]map[string]bool) (*v1.ResourceList, string, string, []string) {
	var podResourceList *v1.ResourceList
	var gpuFraction string
	var gpuMemory string
	if job.IsBestEffortJob {
		podResourceList =
			resources_fake.BuildResourceList(nil, nil, nil, nil)
	} else {
		var requiredGPUsAsString, requiredCPUsAsString, requiredMemoryAsString string
		var requiredCpuInput, requiredMemoryInput *string
		requiredCpuInput = calcRequiredCpu(job, requiredCPUsAsString, requiredCpuInput)
		requiredMemoryInput = CalcRequiredMemory(job, requiredMemoryAsString, requiredMemoryInput)

		// whole GPU job
		gpuMemory = strconv.FormatUint(job.RequiredGpuMemory, 10)
		if float64(int(job.RequiredGPUsPerTask)) == job.RequiredGPUsPerTask {
			requiredGPUsAsString = strconv.Itoa(int(job.RequiredGPUsPerTask))
		} else {
			gpuFraction, gpuGroups = resourceFractionCalc(job, jobAllocatedResource, task,
				gpuGroups, usedSharedGPUs, requiredCpuInput, requiredMemoryInput, requiredGPUsAsString)
		}
		podResourceList = resources_fake.BuildResourceList(requiredCpuInput, requiredMemoryInput, &requiredGPUsAsString,
			resources_fake.MigInstancesToMigInstanceCount(task.RequiredMigInstances))
	}
	return podResourceList, gpuMemory, gpuFraction, gpuGroups
}

func CalcRequiredMemory(job *TestJobBasic, requiredMemoryAsString string, requiredMemoryInput *string) *string {
	if job.RequiredMemoryPerTask != 0 {
		requiredMemoryAsString = strconv.FormatFloat(job.RequiredMemoryPerTask, 'f', -1, 64)
		requiredMemoryInput = &requiredMemoryAsString
	}
	return requiredMemoryInput
}

func calcRequiredCpu(job *TestJobBasic, requiredCPUsAsString string, requiredCpuInput *string) *string {
	if job.RequiredCPUsPerTask != 0 {
		requiredCPUsAsString = strconv.FormatFloat(job.RequiredCPUsPerTask, 'f', -1, 64)
		requiredCpuInput = &requiredCPUsAsString
	}
	return requiredCpuInput
}

func resourceFractionCalc(job *TestJobBasic, jobAllocatedResource *resource_info.Resource,
	task *tasks_fake.TestTaskBasic, gpuGroups []string, usedSharedGPUs map[string]map[string]bool,
	requiredCpuInput *string, requiredMemoryInput *string, requiredGPUsAsString string) (string, []string) {
	gpuFraction := strconv.FormatFloat(job.RequiredGPUsPerTask, 'f', -1, 64)
	multiFractionsCount := float64(1)
	if job.RequiredMultiFractionDevicesPerTask != nil {
		multiFractionsCount = float64(*job.RequiredMultiFractionDevicesPerTask)
	}
	if task.NodeName != "" {
		if usedSharedGPUs[task.NodeName] == nil {
			usedSharedGPUs[task.NodeName] = map[string]bool{}

			jobAllocatedResource.Add(resources_fake.BuildResource(requiredCpuInput, requiredMemoryInput,
				&requiredGPUsAsString, task.RequiredMigInstances))
			jobAllocatedResource.AddGPUs(job.RequiredGPUsPerTask * multiFractionsCount)
		}
		for len(gpuGroups) < int(multiFractionsCount) {
			gpuGroups = append(gpuGroups, string(uuid.NewUUID()))
		}
		for _, gpuGroup := range gpuGroups {
			usedSharedGPUs[task.NodeName][gpuGroup] = true
		}
	}
	return gpuFraction, gpuGroups
}

func createPodOfTask(job *TestJobBasic, taskIndex int,
	task *tasks_fake.TestTaskBasic, podResourceList *v1.ResourceList,
	gpuFraction string, gpuMemory string, gpuGroups []string) *v1.Pod {
	podName := fmt.Sprintf("%s-%d", job.Name, taskIndex)
	podOfTask := tasks_fake.BuildPod(podName, job.Namespace, task.NodeName, task.NodeAffinityNames, v1.PodPending, *podResourceList, gpuFraction, gpuMemory,
		gpuGroups, job.Name, task.RequiredMigInstances, task.ResourceClaimNames, task.ResourceClaimTemplates)
	addPersistentVolumeClaimVolumes(podOfTask, task.PersistentVolumeClaimNames)

	if task.Priority != nil {
		podOfTask.Labels[labels.TaskOrderLabelKey] = strconv.Itoa(int(*task.Priority))
	}

	if task.RequiredGPUs != nil {
		numGPUsStr := strconv.FormatInt(*task.RequiredGPUs, 10)
		podOfTask.Spec.Containers[0].Resources.Requests[resource_info.GPUResourceName] = resource.MustParse(numGPUsStr)
	}

	if pod_status.IsActiveUsedStatus(task.State) {
		podOfTask.Annotations[pod_info.ReceivedResourceTypeAnnotationName] = string(pod_info.ReceivedTypeRegular)
		if gpuFraction != "" {
			podOfTask.Annotations[pod_info.ReceivedResourceTypeAnnotationName] = string(pod_info.ReceivedTypeFraction)
		}
		if len(task.RequiredMigInstances) > 0 {
			podOfTask.Annotations[pod_info.ReceivedResourceTypeAnnotationName] =
				string(pod_info.ReceivedTypeMigInstance)
		}
	}
	return podOfTask
}

func addPersistentVolumeClaimVolumes(pod *v1.Pod, claimNames []string) {
	for index, claimName := range claimNames {
		pod.Spec.Volumes = append(pod.Spec.Volumes, v1.Volume{
			Name: fmt.Sprintf("pvc-%d", index),
			VolumeSource: v1.VolumeSource{
				PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
					ClaimName: claimName,
				},
			},
		})
	}
}

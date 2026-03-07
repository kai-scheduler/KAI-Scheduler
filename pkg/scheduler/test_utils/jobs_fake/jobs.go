// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobs_fake

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"

	enginev2alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	pg "github.com/kai-scheduler/kai-scheduler/pkg/common/podgroup"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants/labels"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/resources_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

type TestJobBasic struct {
	RequiredGPUsPerTask                 float64
	RequiredGpuMemory                   uint64
	RequiredCPUsPerTask                 float64
	RequiredMemoryPerTask               float64
	RequiredMultiFractionDevicesPerTask *uint64
	Priority                            int32
	Preemptibility                      enginev2alpha2.Preemptibility
	Name                                string
	Namespace                           string
	QueueName                           string
	IsBestEffortJob                     bool
	JobAgeInMinutes                     int
	DeleteJobInTest                     bool
	JobNotReadyForSsn                   bool
	Tasks                               []*tasks_fake.TestTaskBasic
	RootSubGroupSet                     *subgroup_info.SubGroupSet
	StaleDuration                       *time.Duration
}

func BuildJobsAndTasksMaps(Jobs []*TestJobBasic, vectorMap *resource_info.ResourceVectorMap, draClaims ...runtime.Object) (
	map[common_info.PodGroupID]*podgroup_info.PodGroupInfo, map[string]pod_info.PodsMap, map[string]map[string]bool,
) {
	jobsInfoMap := map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{}
	usedSharedGPUs := map[string]map[string]bool{}
	tasksToNodeMap := map[string]pod_info.PodsMap{}
	allocatedGPUs := map[string]interface{}{}

	sort.SliceStable(Jobs, func(i, j int) bool {
		return Jobs[i].Priority > Jobs[j].Priority
	})

	draClaimsMap := map[string]*resourceapi.ResourceClaim{}
	for _, draObject := range draClaims {
		if draObject, ok := draObject.(*resourceapi.ResourceClaim); ok {
			draClaimsMap[draObject.Name] = draObject
		}
	}

	for jobIndex, job := range Jobs {
		jobName := job.Name

		jobAllocatedResource := resource_info.EmptyResource()
		taskInfos := generateTasks(job, jobAllocatedResource, usedSharedGPUs, allocatedGPUs,
			tasksToNodeMap, draClaimsMap, vectorMap)

		jobUID := common_info.PodGroupID(jobName)
		queueUID := common_info.QueueID(job.QueueName)
		numberOfJobs := len(Jobs)

		jobCreationTime := time.Now().Add(time.Minute * time.Duration(numberOfJobs-jobIndex) * (-1))
		if job.JobAgeInMinutes != 0 {
			jobCreationTime = time.Now().Add(time.Minute * time.Duration(job.JobAgeInMinutes) * (-1))
		}

		job.Preemptibility = pg.CalculatePreemptibility(job.Preemptibility, job.Priority)

		jobInfo := BuildJobInfo(
			jobName, job.Namespace, jobUID, jobAllocatedResource, job.RootSubGroupSet, taskInfos,
			job.Priority, job.Preemptibility, queueUID, jobCreationTime, job.StaleDuration, vectorMap,
		)
		jobsInfoMap[common_info.PodGroupID(job.Name)] = jobInfo
	}

	return jobsInfoMap, tasksToNodeMap, usedSharedGPUs
}

func BuildJobInfo(
	name, namespace string, uid common_info.PodGroupID, allocatedResource *resource_info.Resource,
	rootSubGroupSet *subgroup_info.SubGroupSet, taskInfos []*pod_info.PodInfo,
	priority int32, preemptibility enginev2alpha2.Preemptibility, queueUID common_info.QueueID,
	jobCreationTime time.Time, staleDuration *time.Duration, vectorMap *resource_info.ResourceVectorMap,
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

	if rootSubGroupSet == nil {
		rootSubGroupSet = subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	}
	podSets := rootSubGroupSet.GetAllPodSets()
	for _, taskInfo := range taskInfos {
		if len(taskInfo.SubGroupName) > 0 {
			subGroup := podSets[taskInfo.SubGroupName]
			subGroup.AssignTask(taskInfo)
		} else {
			if podSets[podgroup_info.DefaultSubGroup] == nil {
				podSets[podgroup_info.DefaultSubGroup] = subgroup_info.NewPodSet(
					podgroup_info.DefaultSubGroup, int32(len(taskInfos)), nil,
				)
				rootSubGroupSet.AddPodSet(podSets[podgroup_info.DefaultSubGroup])
			}
			podSets[podgroup_info.DefaultSubGroup].AssignTask(taskInfo)
		}
	}

	result := &podgroup_info.PodGroupInfo{
		UID:             uid,
		Name:            name,
		Namespace:       namespace,
		Allocated:       allocatedResource,
		AllocatedVector: allocatedResource.ToVector(vectorMap),
		VectorMap:       vectorMap,
		PodStatusIndex:  taskStatusIndex,
		Priority:        priority,
		Preemptibility:  preemptibility, JobFitErrors: make([]common_info.JobFitError, 0),
		TasksFitErrors:    map[common_info.PodID]*common_info.TasksFitErrors{},
		Queue:             queueUID,
		CreationTimestamp: metav1.Time{Time: jobCreationTime},
		RootSubGroupSet:   rootSubGroupSet,
		PodSets:           podSets,
		PodGroup: &enginev2alpha2.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				UID:               types.UID(uid),
				Name:              name,
				Namespace:         namespace,
				CreationTimestamp: metav1.Time{Time: jobCreationTime},
			},
			Spec: enginev2alpha2.PodGroupSpec{
				Queue: string(queueUID),
			},
		},
	}

	_ = result.GetActiveAllocatedTasksCount()
	if staleDuration != nil {
		staleTime := time.Now().Add(-1 * *staleDuration)
		result.StalenessInfo.TimeStamp = &staleTime
		result.StalenessInfo.Stale = true
	}
	if result.LastStartTimestamp == nil && result.GetNumAllocatedTasks() > 0 {
		startTime := time.Now().Add(-1 * time.Minute * 1)
		result.LastStartTimestamp = &startTime
	}
	return result
}

func DefaultSubGroup(minAvailable int32) *subgroup_info.SubGroupSet {
	subGroup := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	subGroup.AddPodSet(subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, minAvailable, nil))
	return subGroup
}

func generateTasks(
	job *TestJobBasic, jobAllocatedResource *resource_info.Resource,
	usedSharedGPUs map[string]map[string]bool, allocatedGPUs map[string]interface{},
	tasksToNodeMap map[string]pod_info.PodsMap, draClaimsMap map[string]*resourceapi.ResourceClaim,
	vectorMap *resource_info.ResourceVectorMap,
) []*pod_info.PodInfo {
	taskInfos := []*pod_info.PodInfo{}
	for taskIndex, task := range job.Tasks {
		gpuGroups := tasks_fake.GetTestTaskGPUIndex(task)

		podResourceList, gpuMemory, gpuFraction, gpuGroups :=
			CalcJobAndPodResources(job, jobAllocatedResource, task, gpuGroups,
				usedSharedGPUs)

		podOfTask := createPodOfTask(job, taskIndex, task, podResourceList, gpuFraction,
			gpuMemory, gpuGroups)

		var draPodClaims []*resourceapi.ResourceClaim
		if len(draClaimsMap) > 0 {
			draPodClaims = getDraClaimsForPod(task, draClaimsMap)
		}

		for _, container := range append(podOfTask.Spec.Containers, podOfTask.Spec.InitContainers...) {
			vectorMap.AddResourceList(container.Resources.Requests)
		}

		taskInfo := pod_info.NewTaskInfo(podOfTask, draPodClaims, vectorMap)
		taskInfo.Status = task.State
		taskInfo.GPUGroups = gpuGroups
		taskInfo.SubGroupName = task.SubGroupName
		taskInfo.IsLegacyMIGtask = task.IsLegacyMigTask
		taskInfos = append(taskInfos, taskInfo)

		if pod_status.AllocatedStatus(taskInfo.Status) && gpuFraction == "" {
			jobAllocatedResource.Add(resource_info.ResourceFromResourceList(*podResourceList))
		}
		if gpuFraction != "" {
			jobAllocatedResource.Add(resource_info.ResourceFromResourceList(
				v1.ResourceList{
					v1.ResourcePods: resource.MustParse("1"),
				},
			))
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

func getDraClaimsForPod(task *tasks_fake.TestTaskBasic, draClaimsMap map[string]*resourceapi.ResourceClaim) []*resourceapi.ResourceClaim {
	draPodClaims := []*resourceapi.ResourceClaim{}
	for _, podClaimName := range task.ResourceClaimNames {
		if draClaim, ok := draClaimsMap[podClaimName]; ok {
			draPodClaims = append(draPodClaims, draClaim)
		}
	}
	for _, podClaimName := range task.ResourceClaimTemplates {
		if draClaim, ok := draClaimsMap[podClaimName]; ok {
			draPodClaims = append(draPodClaims, draClaim)
		}
	}
	return draPodClaims
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

	(*podResourceList)[v1.ResourcePods] = resource.MustParse("1")

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
	podOfTask := tasks_fake.BuildPod(podName, job.Namespace, task, v1.PodPending, *podResourceList, gpuFraction, gpuMemory,
		gpuGroups, job.Name)

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

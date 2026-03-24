// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"testing"
	"time"

	// lint:ignore ST1001 we want to use gomock here
	. "go.uber.org/mock/gomock"
	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/cache"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	k8splugins "github.com/NVIDIA/KAI-scheduler/pkg/scheduler/k8s_internal/plugins"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/k8s_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
)

var SchedulerVerbosity = flag.String("vv", "", "Scheduler's verbosity")

type TestTopologyBasic struct {
	Name string
	Jobs []*jobs_fake.TestJobBasic

	Nodes                  map[string]nodes_fake.TestNodeBasic
	Queues                 []TestQueueBasic
	Departments            []TestDepartmentBasic
	JobExpectedResults     map[string]TestExpectedResultBasic
	TaskExpectedResults    map[string]TestExpectedResultBasic
	ExpectedNodesResources map[string]TestExpectedNodesResources
	Mocks                  *TestMock

	dra_fake.TestDRAObjects
}

type TestMock struct {
	CacheRequirements *CacheMocking
	Cache             *cache.Cache
	GPUMetric         *GPUMetricMocks
	SchedulerConf     *conf.SchedulerConfiguration
}

type CacheMocking struct {
	NumberOfCacheBinds      int
	NumberOfCacheEvictions  int
	NumberOfPipelineActions int
}

type GPUMetricMocks struct {
	ActiveGPUs []int
}

type TestQueueBasic struct {
	Name                        string
	Priority                    *int
	DeservedGPUs                float64
	DeservedCPUs                *float64
	DeservedMemory              *float64
	MaxAllowedGPUs              float64
	MaxAllowedCPUs              *float64
	MaxAllowedMemory            *float64
	GPUOverQuotaWeight          float64
	ParentQueue                 string
	InteractiveTimeoutInMinutes int64
	UseOnlyFreeCPUResources     bool
	V1                          bool
}

type TestDepartmentBasic struct {
	Name             string
	DeservedGPUs     float64
	MaxAllowedGPUs   float64
	MaxAllowedCPUs   *float64
	MaxAllowedMemory *float64
}

type TestSessionConfig struct {
	Plugins      []conf.Tier
	CachePlugins map[string]bool
}

type TestExpectedResultBasic struct {
	NodeName                    string
	GPUsRequired                float64
	GPUsAccepted                float64
	MilliCpuRequired            float64
	MemoryRequired              float64
	Status                      pod_status.PodStatus
	GPUGroups                   []string
	DontValidateGPUGroup        bool
	LastStartTimestampOlderThan *time.Duration
}

type TestExpectedNodesResources struct {
	ReleasingGPUs float64
	IdleGPUs      float64
}

func MatchExpectedAndRealTasks(t *testing.T, testNumber int, testMetadata TestTopologyBasic, ssn *framework.Session) {

	tasksToGPUGroup := make(map[string]map[string]string)

	for jobName, jobExpectedResult := range testMetadata.JobExpectedResults {
		var sumOfJobRequestedGPU, sumOfJobRequestedMillisCpu, sumOfJobRequestedMemory, sumOfAcceptedGpus float64
		job, found := ssn.PodGroupInfos[common_info.PodGroupID(jobName)]
		if !found {
			t.Errorf("Test number: %d, name: %v, has failed. Couldn't find job: %v for expected tasks.", testNumber, testMetadata.Name, jobName)
		}
		for _, taskInfo := range ssn.PodGroupInfos[common_info.PodGroupID(jobName)].PodInfos {

			if taskInfo.Status != jobExpectedResult.Status {
				t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual uses status: %v, was expecting status: %v", testNumber, testMetadata.Name, taskInfo.Name, taskInfo.Status, jobExpectedResult.Status.String())
				if jobExpectedResult.Status == pod_status.Running {
					t.Errorf("%v", job.JobFitErrors)
					t.Errorf("%v", job.NodesFitErrors)
				}
			}

			if len(jobExpectedResult.NodeName) > 0 && taskInfo.NodeName != jobExpectedResult.NodeName {
				t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual uses node: %v, was expecting node: %v", testNumber, testMetadata.Name, taskInfo.Name, taskInfo.NodeName, jobExpectedResult.NodeName)
			}

			sumOfJobRequestedGPU += taskInfo.ResReq.GPUs()
			sumOfJobRequestedMillisCpu += taskInfo.ResReq.Cpu()
			sumOfJobRequestedMemory += taskInfo.ResReq.Memory()
			sumOfAcceptedGpus += taskInfo.AcceptedResource.GPUs()

			// verify fractional GPUs index
			if pod_status.IsActiveUsedStatus(taskInfo.Status) &&
				!jobExpectedResult.DontValidateGPUGroup &&
				taskInfo.IsSharedGPUAllocation() &&
				slices.Equal(taskInfo.GPUGroups, jobExpectedResult.GPUGroups) {
				nodeGPUs, found := tasksToGPUGroup[taskInfo.NodeName]
				if !found {
					tasksToGPUGroup[taskInfo.NodeName] = make(map[string]string)
					nodeGPUs = tasksToGPUGroup[taskInfo.NodeName]
				}
				for gpuGroupIndex, expectedGpuGroup := range jobExpectedResult.GPUGroups {
					if gpuGroup, found := nodeGPUs[expectedGpuGroup]; !found {
						nodeGPUs[expectedGpuGroup] = taskInfo.GPUGroups[gpuGroupIndex]
					} else if gpuGroup != taskInfo.GPUGroups[gpuGroupIndex] {
						t.Errorf(
							"Test number: %d, name: %v, has failed. Task name: %v, "+
								"running on GPU: %s, was expecting GPU index: %s",
							testNumber, testMetadata.Name, taskInfo.Name, taskInfo.GPUGroups, jobExpectedResult.GPUGroups,
						)
					}
				}
			}
		}

		if pod_status.AllocatedStatus(jobExpectedResult.Status) {
			if job.LastStartTimestamp == nil {
				t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual last start timestamp is not set expecting pod_status.%v", testNumber, testMetadata.Name, jobName, jobExpectedResult.Status.String())
			} else if jobExpectedResult.LastStartTimestampOlderThan != nil {
				now := time.Now()
				if now.Sub(*job.LastStartTimestamp) < *jobExpectedResult.LastStartTimestampOlderThan {
					t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual last start timestamp is not older than %v", testNumber, testMetadata.Name, jobName,
						*jobExpectedResult.LastStartTimestampOlderThan)
				}
			}
		}

		if sumOfAcceptedGpus != jobExpectedResult.GPUsAccepted && jobExpectedResult.GPUsAccepted != 0 {
			t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual accept GPUs: %v, was expecting GPUs: %v", testNumber, testMetadata.Name, jobName, sumOfAcceptedGpus, jobExpectedResult.GPUsAccepted)
		}
		if sumOfJobRequestedGPU != jobExpectedResult.GPUsRequired {
			t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual uses  GPUs: %v, was expecting GPUs: %v", testNumber, testMetadata.Name, jobName, sumOfJobRequestedGPU, jobExpectedResult.GPUsRequired)
		}
		if jobExpectedResult.MilliCpuRequired != 0 && sumOfJobRequestedMillisCpu != jobExpectedResult.MilliCpuRequired {
			t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual uses MilliCpu: %v, was expecting MilliCpu: %v", testNumber, testMetadata.Name, jobName, sumOfJobRequestedMillisCpu, jobExpectedResult.MilliCpuRequired)
		}
		if jobExpectedResult.MemoryRequired != 0 && sumOfJobRequestedMemory != jobExpectedResult.MemoryRequired {
			t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual uses Memory: %v, was expecting Memory: %v", testNumber, testMetadata.Name, jobName, sumOfJobRequestedMemory, jobExpectedResult.MemoryRequired)
		}
	}

	if len(testMetadata.TaskExpectedResults) > 0 {
		for jobId, job := range ssn.PodGroupInfos {
			for taskId, task := range ssn.PodGroupInfos[jobId].PodInfos {
				taskExpectedResult, found := testMetadata.TaskExpectedResults[string(taskId)]
				if !found {
					continue
				}

				if task.Status != taskExpectedResult.Status {
					t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual uses status: %v, "+
						"was expecting status: %v", testNumber, testMetadata.Name, taskId, task.Status,
						taskExpectedResult.Status.String())
				}

				if len(taskExpectedResult.NodeName) > 0 && task.NodeName != taskExpectedResult.NodeName {
					t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual uses node: %v, "+
						"was expecting node: %v", testNumber, testMetadata.Name, taskId, task.NodeName,
						taskExpectedResult.NodeName)
				}

				acceptedGPUs := task.AcceptedResource.GPUs()
				if taskExpectedResult.GPUsAccepted != 0 && acceptedGPUs != taskExpectedResult.GPUsAccepted {
					t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual accept GPUs: %v, "+
						"was expecting GPUs: %v", testNumber, testMetadata.Name, taskId, acceptedGPUs,
						taskExpectedResult.GPUsAccepted)
				}

				requestedGPUs := task.ResReq.GPUs()
				if requestedGPUs != taskExpectedResult.GPUsRequired {
					t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual uses  GPUs: %v, "+
						"was expecting GPUs: %v", testNumber, testMetadata.Name, taskId, requestedGPUs,
						taskExpectedResult.GPUsRequired)
				}

				requestedMilliCPUs := task.ResReq.Cpu()
				if taskExpectedResult.MilliCpuRequired != 0 && requestedMilliCPUs != taskExpectedResult.MilliCpuRequired {
					t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual uses MilliCpu: %v, "+
						"was expecting MilliCpu: %v", testNumber, testMetadata.Name, taskId, requestedMilliCPUs,
						taskExpectedResult.MilliCpuRequired)
				}

				requestedMemory := task.ResReq.Memory()
				if taskExpectedResult.MemoryRequired != 0 && requestedMemory != taskExpectedResult.MemoryRequired {
					t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual uses Memory: %v, "+
						"was expecting Memory: %v", testNumber, testMetadata.Name, taskId, requestedMemory,
						taskExpectedResult.MemoryRequired)
				}

				if pod_status.AllocatedStatus(taskExpectedResult.Status) {
					if job.LastStartTimestamp == nil {
						t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual last start timestamp is not set expecting pod_status.%v", testNumber, testMetadata.Name, taskId, taskExpectedResult.Status.String())
					} else if taskExpectedResult.LastStartTimestampOlderThan != nil {
						now := time.Now()
						if now.Sub(*job.LastStartTimestamp) < *taskExpectedResult.LastStartTimestampOlderThan {
							t.Errorf("Test number: %d, name: %v, has failed. Task name: %v, actual last start timestamp is not older than %v", testNumber, testMetadata.Name, taskId,
								*taskExpectedResult.LastStartTimestampOlderThan)
						}
					}
				}

				// verify fractional GPUs index
				if pod_status.IsActiveUsedStatus(task.Status) &&
					!taskExpectedResult.DontValidateGPUGroup &&
					task.IsSharedGPUAllocation() &&
					slices.Equal(task.GPUGroups, taskExpectedResult.GPUGroups) {
					nodeGPUs, found := tasksToGPUGroup[task.NodeName]
					if !found {
						tasksToGPUGroup[task.NodeName] = make(map[string]string)
						nodeGPUs = tasksToGPUGroup[task.NodeName]
					}
					for gpuGroupIndex, expectedGpuGroup := range taskExpectedResult.GPUGroups {
						if gpuGroup, found := nodeGPUs[expectedGpuGroup]; !found {
							nodeGPUs[expectedGpuGroup] = task.GPUGroups[gpuGroupIndex]
						} else if gpuGroup != task.GPUGroups[gpuGroupIndex] {
							t.Errorf(
								"Test number: %d, name: %v, has failed. Task name: %v, "+
									"running on GPU: %s, was expecting GPU index: %s",
								testNumber, testMetadata.Name, taskId, task.GPUGroups, taskExpectedResult.GPUGroups,
							)
						}
					}
				}

			}
		}
	}

	for nodeName, nodeExpectedResources := range testMetadata.ExpectedNodesResources {
		ssnNode, found := ssn.Nodes[nodeName]
		if !found {
			t.Errorf("Test number: %d, name: %v, has failed. Couldn't find node: %v for expected nodes resources.", testNumber, testMetadata.Name, nodeName)
		}

		if nodeExpectedResources.ReleasingGPUs != ssnNode.Releasing.GPUs() {
			t.Errorf("Test number: %d, name: %v, has failed. Node name: %v, actual Releasing GPUs: %v, was expecting Releasing GPUs: %v", testNumber, testMetadata.Name, nodeName, ssnNode.Releasing.GPUs(), nodeExpectedResources.ReleasingGPUs)
		}

		if nodeExpectedResources.IdleGPUs != ssnNode.Idle.GPUs() {
			t.Errorf("Test number: %d, name: %v, has failed. Node name: %v, actual Idle GPUs: %v, was expecting Idle GPUs: %v", testNumber, testMetadata.Name, nodeName, ssnNode.Idle.GPUs(), nodeExpectedResources.IdleGPUs)
		}
	}
}

func GetTestCacheMock(controller *Controller, testMocks *TestMock, additionalObjects []runtime.Object) *cache.MockCache {
	cacheMock := cache.NewMockCache(controller)
	cacheRequirements := &CacheMocking{}
	if testMocks != nil {
		cacheRequirements = testMocks.CacheRequirements
	}

	if cacheRequirements.NumberOfCacheBinds != 0 {
		cacheMock.EXPECT().Bind(Any(), Any()).Return(nil).MaxTimes(cacheRequirements.NumberOfCacheBinds)
	}

	fakeClient := fake.NewSimpleClientset(additionalObjects...)
	cacheMock.EXPECT().KubeClient().AnyTimes().Return(fakeClient)

	informerFactory := informers.NewSharedInformerFactory(cacheMock.KubeClient(), 0)

	informerFactory.Resource().V1().ResourceClaims().Informer()
	informerFactory.Resource().V1().ResourceSlices().Informer()
	informerFactory.Resource().V1().DeviceClasses().Informer()

	ctx := context.Background()
	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())

	cacheMock.EXPECT().KubeInformerFactory().AnyTimes().Return(informerFactory)
	cacheMock.EXPECT().SnapshotSharedLister().AnyTimes().Return(cache.NewK8sClusterPodAffinityInfo())

	k8sPlugins := k8splugins.InitializeInternalPlugins(
		cacheMock.KubeClient(), cacheMock.KubeInformerFactory(), cacheMock.SnapshotSharedLister(),
	)
	cacheMock.EXPECT().InternalK8sPlugins().AnyTimes().Return(k8sPlugins)

	if cacheRequirements.NumberOfCacheEvictions != 0 {
		cacheMock.EXPECT().Evict(Any(), Any(), Any(), Any()).
			Return(nil).MaxTimes(cacheRequirements.NumberOfCacheEvictions)
	}

	if cacheRequirements.NumberOfPipelineActions != 0 {
		cacheMock.EXPECT().TaskPipelined(Any(), Any()).
			MaxTimes(cacheRequirements.NumberOfPipelineActions)
	}

	helpersMock := k8s_utils.NewMockInterface(controller)
	k8s_utils.Helpers = helpersMock
	helpersMock.EXPECT().PatchPodAnnotationsAndLabelsInterface(Any(), Any(), Any(), Any()).Return(nil).AnyTimes()

	return cacheMock
}

func CreateFloat64Pointer(x float64) *float64 {
	return &x
}

func InitTestingInfrastructure() {
	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	if *SchedulerVerbosity != "" {
		verbosity, err := strconv.Atoi(*SchedulerVerbosity)
		if err == nil {
			if err := log.InitLoggers(verbosity); err != nil {
				fmt.Printf("Failed to initialize loggers: %v", err)
			}
		}
	}
}

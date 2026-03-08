// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	resourceapi "k8s.io/api/resource/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregate "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/features"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStatementAllocateRollback_WithDRAClaims(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	type testMetadata struct {
		name     string
		topology test_utils.TestTopologyBasic
		nodeName string
	}

	for i, test := range []testMetadata{
		{
			name: "Allocate then rollback pod with DRA claim",
			topology: test_utils.TestTopologyBasic{
				Queues: []test_utils.TestQueueBasic{
					{Name: "q-1"},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-test",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "task-1",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"gpu-claim"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "gpu-claim",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
						},
					},
				},
			},
			nodeName: "node0",
		},
		{
			name: "Allocate then rollback pod with multiple DRA claims",
			topology: test_utils.TestTopologyBasic{
				Queues: []test_utils.TestQueueBasic{
					{Name: "q-1"},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-test",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "task-1",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"gpu-claim-1", "gpu-claim-2"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           2,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "gpu-claim-1",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
						},
						{
							Name:            "gpu-claim-2",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
						},
					},
				},
			},
			nodeName: "node0",
		},
		{
			name: "Allocate then rollback pod where claim is already allocated to another pod (shared claim)",
			topology: test_utils.TestTopologyBasic{
				Queues: []test_utils.TestQueueBasic{
					{Name: "q-1"},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-other",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "other-task",
								State:              pod_status.Running,
								NodeName:           "node0",
								ResourceClaimNames: []string{"shared-gpu-claim"},
							},
						},
					},
					{
						Name:      "job-test",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "task-1",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"shared-gpu-claim"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "shared-gpu-claim",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "gpu",
												Driver:  "gpu.resource.k8s.io",
												Pool:    "node0",
												Device:  "gpu-0",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{
										Resource: "pods",
										Name:     "job-other-other-task",
										UID:      "other-pod-uid",
									},
								},
							},
						},
					},
				},
			},
			nodeName: "node0",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Running test number: %d, test name: %s", i, test.name)

			ssn := setupTestSession(t, test.topology, controller)
			task := getTaskByStatus(t, ssn, "job-test", pod_status.Pending)

			stmt := ssn.Statement()
			cp := stmt.Checkpoint()

			initialState := captureTaskState(task)
			expectedClaimCount := len(test.topology.TestDRAObjects.ResourceClaims)
			assert.Equal(t, expectedClaimCount, initialState.claimInfoCount, "Task should have ResourceClaimInfo entries for each claim")

			err := stmt.Allocate(task, test.nodeName)
			assert.NoError(t, err)

			assert.Equal(t, test.nodeName, task.NodeName)
			assert.Equal(t, pod_status.Allocated, task.Status)
			assert.Equal(t, initialState.claimInfoCount, len(task.ResourceClaimInfo))
			assertClaimAllocationsExist(t, task)

			err = stmt.Rollback(cp)
			assert.NoError(t, err)

			assertTaskStateRestored(t, task, initialState, "rollback")
		})
	}
}

func TestStatementEvictUnevict_WithDRAClaims(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	type testMetadata struct {
		name                string
		topology            test_utils.TestTopologyBasic
		bindRequest         *schedulingv1alpha2.BindRequest
		bindRequestNodeName string
	}

	for i, test := range []testMetadata{
		{
			name: "Evict then unevict running pod with DRA claim",
			topology: test_utils.TestTopologyBasic{
				Queues: []test_utils.TestQueueBasic{
					{Name: "q-1"},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "test-job",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "test-task",
								State:              pod_status.Running,
								NodeName:           "node0",
								ResourceClaimNames: []string{"gpu-claim"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "gpu-claim",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "gpu",
												Driver:  "gpu.resource.k8s.io",
												Pool:    "node0",
												Device:  "gpu-0",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{
										Resource: "pods",
										Name:     "job-running-task-running",
										UID:      "running-pod-uid",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Evict then unevict running pod with shared DRA claim (multiple consumers)",
			topology: test_utils.TestTopologyBasic{
				Queues: []test_utils.TestQueueBasic{
					{Name: "q-1"},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-other",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "task-other",
								State:              pod_status.Running,
								NodeName:           "node0",
								ResourceClaimNames: []string{"shared-gpu-claim"},
							},
						},
					},
					{
						Name:      "test-job",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "test-task",
								State:              pod_status.Running,
								NodeName:           "node0",
								ResourceClaimNames: []string{"shared-gpu-claim"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "shared-gpu-claim",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "gpu",
												Driver:  "gpu.resource.k8s.io",
												Pool:    "node0",
												Device:  "gpu-0",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{
										Resource: "pods",
										Name:     "job-other-task-other",
										UID:      "other-pod-uid",
									},
									{
										Resource: "pods",
										Name:     "job-running-task-running",
										UID:      "running-pod-uid",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Evict then unevict pod in Binding state with BindRequest and DRA claim",
			bindRequest: &schedulingv1alpha2.BindRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bind-request",
					Namespace: "test",
				},
				Spec: schedulingv1alpha2.BindRequestSpec{
					PodName:      "test-pod",
					SelectedNode: "node0",
					ResourceClaimAllocations: []schedulingv1alpha2.ResourceClaimAllocation{
						{
							Name: "gpu-claim",
							Allocation: &resourceapi.AllocationResult{
								Devices: resourceapi.DeviceAllocationResult{
									Results: []resourceapi.DeviceRequestAllocationResult{
										{
											Request: "gpu",
											Driver:  "gpu.resource.k8s.io",
											Pool:    "node0",
											Device:  "gpu-0",
										},
									},
								},
							},
						},
					},
				},
			},
			bindRequestNodeName: "node0",
			topology: test_utils.TestTopologyBasic{
				Queues: []test_utils.TestQueueBasic{
					{Name: "q-1"},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "test-job",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "test-task",
								State:              pod_status.Binding,
								NodeName:           "node0",
								ResourceClaimNames: []string{"gpu-claim"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "gpu-claim",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "gpu",
												Driver:  "gpu.resource.k8s.io",
												Pool:    "node0",
												Device:  "gpu-0",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{
										Resource: "pods",
										Name:     "job-binding-task-binding",
										UID:      "binding-pod-uid",
									},
								},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Running test number: %d, test name: %s", i, test.name)

			ssn := setupTestSession(t, test.topology, controller)
			job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("test-job")]
			task := job.GetAllPodsMap()[common_info.PodID("test-job-0")]

			if test.bindRequest != nil {
				bindRequestInfo := bindrequest_info.NewBindRequestInfo(test.bindRequest)
				newTask := pod_info.NewTaskInfoWithBindRequest(task.Pod, bindRequestInfo, ssn.ClusterInfo.ResourceClaims, resource_info.NewResourceVectorMap())

				for _, podSet := range job.PodSets {
					if _, exists := podSet.GetPodInfos()[task.UID]; exists {
						delete(podSet.GetPodInfos(), task.UID)
						podSet.GetPodInfos()[newTask.UID] = newTask
						break
					}
				}
				task = newTask
			}

			stmt := ssn.Statement()
			cp := stmt.Checkpoint()

			initialState := captureTaskState(task)
			assert.Greater(t, initialState.claimInfoCount, 0, "Task should have ResourceClaimInfo")

			err := stmt.Evict(task, "test eviction", eviction_info.EvictionMetadata{
				EvictionGangSize: 1,
				Action:           "reclaim",
			})
			assert.NoError(t, err)

			assert.Equal(t, pod_status.Releasing, task.Status)
			assert.Equal(t, initialState.claimInfoCount, len(task.ResourceClaimInfo))

			err = stmt.Rollback(cp)
			assert.NoError(t, err)

			assertTaskStateRestored(t, task, initialState, "unevict")
		})
	}
}

func TestStatementPipelineUnpipeline_WithDRAClaims(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	type testMetadata struct {
		name     string
		topology test_utils.TestTopologyBasic
		nodeName string
	}

	for i, test := range []testMetadata{
		{
			name: "Pipeline then unpipeline pod with DRA claim",
			topology: test_utils.TestTopologyBasic{
				Queues: []test_utils.TestQueueBasic{
					{Name: "q-1"},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-pending",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "task-pending",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"gpu-claim"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "gpu-claim",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
						},
					},
				},
			},
			nodeName: "node0",
		},
		{
			name: "Pipeline then unpipeline pod with multiple DRA claims",
			topology: test_utils.TestTopologyBasic{
				Queues: []test_utils.TestQueueBasic{
					{Name: "q-1"},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-pending",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "task-pending",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"gpu-claim-1", "gpu-claim-2"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           2,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "gpu-claim-1",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
						},
						{
							Name:            "gpu-claim-2",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
						},
					},
				},
			},
			nodeName: "node0",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Running test number: %d, test name: %s", i, test.name)

			ssn := setupTestSession(t, test.topology, controller)
			task := getTaskByStatus(t, ssn, "job-pending", pod_status.Pending)

			stmt := ssn.Statement()
			cp := stmt.Checkpoint()

			initialState := captureTaskState(task)
			assert.Greater(t, initialState.claimInfoCount, 0, "Pending task should have ResourceClaimInfo")

			err := stmt.Pipeline(task, test.nodeName, false)
			assert.NoError(t, err)

			assert.Equal(t, test.nodeName, task.NodeName)
			assert.Equal(t, pod_status.Pipelined, task.Status)
			assert.Equal(t, initialState.claimInfoCount, len(task.ResourceClaimInfo))
			assertClaimAllocationsExist(t, task)

			for _, claimAlloc := range task.ResourceClaimInfo {
				for _, deviceResult := range claimAlloc.Allocation.Devices.Results {
					assert.NotEmpty(t, deviceResult.Pool, "Pool should be specified")
				}
			}

			err = stmt.Rollback(cp)
			assert.NoError(t, err)

			assertTaskStateRestored(t, task, initialState, "unpipeline")
		})
	}
}

type taskState struct {
	nodeName       string
	status         pod_status.PodStatus
	claimInfos     map[string]*schedulingv1alpha2.ResourceClaimAllocation
	claimInfoCount int
}

func setupTestSession(t *testing.T, topology test_utils.TestTopologyBasic, controller *Controller) *framework.Session {
	featuregate.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DynamicResourceAllocation, true)
	ssn := test_utils.BuildSession(topology, controller)
	time.Sleep(1 * time.Millisecond)
	return ssn
}

func getTaskByStatus(t *testing.T, ssn *framework.Session, jobName string, status pod_status.PodStatus) *pod_info.PodInfo {
	job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID(jobName)]
	assert.NotNil(t, job, "Job %s should exist", jobName)

	for _, task := range job.GetAllPodsMap() {
		if task.Status == status {
			return task
		}
	}

	assert.Fail(t, "Should find task with status %s in job %s", status, jobName)
	return nil
}

func captureTaskState(task *pod_info.PodInfo) taskState {
	state := taskState{
		nodeName:       task.NodeName,
		status:         task.Status,
		claimInfoCount: len(task.ResourceClaimInfo),
		claimInfos:     make(map[string]*schedulingv1alpha2.ResourceClaimAllocation),
	}

	for claimName, claimAlloc := range task.ResourceClaimInfo {
		if claimAlloc != nil {
			state.claimInfos[claimName] = claimAlloc.DeepCopy()
		}
	}

	return state
}

func assertTaskStateRestored(t *testing.T, task *pod_info.PodInfo, initial taskState, contextMsg string) {
	assert.Equal(t, initial.nodeName, task.NodeName, "Task node should be restored after %s", contextMsg)
	assert.Equal(t, initial.status, task.Status, "Task status should be restored after %s", contextMsg)
	assert.Equal(t, initial.claimInfoCount, len(task.ResourceClaimInfo), "ResourceClaimInfo count should match after %s", contextMsg)

	for claimName, currentAlloc := range task.ResourceClaimInfo {
		initialAlloc := initial.claimInfos[claimName]

		if initialAlloc == nil || initialAlloc.Allocation == nil {
			if currentAlloc.Allocation != nil {
				assert.Equal(t, 0, len(currentAlloc.Allocation.Devices.Results),
					"Claim %s should have no allocations after %s", claimName, contextMsg)
			}
		} else {
			assert.NotNil(t, currentAlloc.Allocation, "Claim %s should have allocation after %s", claimName, contextMsg)
			assert.Equal(t, len(initialAlloc.Allocation.Devices.Results), len(currentAlloc.Allocation.Devices.Results),
				"Claim %s device count should match after %s", claimName, contextMsg)

			for i, initialDevice := range initialAlloc.Allocation.Devices.Results {
				currentDevice := currentAlloc.Allocation.Devices.Results[i]
				assert.Equal(t, initialDevice.Device, currentDevice.Device,
					"Claim %s device %d name should match after %s", claimName, i, contextMsg)
				assert.Equal(t, initialDevice.Driver, currentDevice.Driver,
					"Claim %s device %d driver should match after %s", claimName, i, contextMsg)
				assert.Equal(t, initialDevice.Pool, currentDevice.Pool,
					"Claim %s device %d pool should match after %s", claimName, i, contextMsg)
				assert.Equal(t, initialDevice.Request, currentDevice.Request,
					"Claim %s device %d request should match after %s", claimName, i, contextMsg)
			}
		}
	}
}

func assertClaimAllocationsExist(t *testing.T, task *pod_info.PodInfo) {
	for claimName, claimAlloc := range task.ResourceClaimInfo {
		assert.NotNil(t, claimAlloc, "Claim %s should exist", claimName)
		assert.NotNil(t, claimAlloc.Allocation, "Claim %s should have allocation", claimName)
		assert.Greater(t, len(claimAlloc.Allocation.Devices.Results), 0, "Claim %s should have device results", claimName)

		for _, deviceResult := range claimAlloc.Allocation.Devices.Results {
			assert.NotEmpty(t, deviceResult.Device, "Claim %s device name should not be empty", claimName)
			assert.NotEmpty(t, deviceResult.Driver, "Claim %s driver should not be empty", claimName)
		}
	}
}

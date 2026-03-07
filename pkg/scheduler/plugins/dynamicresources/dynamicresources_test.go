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

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestDynamicResourceAllocationPreFilter(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	type testMetadata struct {
		name     string
		enabled  bool
		topology test_utils.TestTopologyBasic
		err      string
	}
	for i, test := range []testMetadata{
		{
			name:    "Dynamic Resource Allocation is enabled - no claims",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "task-1",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{},
							},
						},
					},
				},
			},
		},
		{
			name:    "Dynamic Resource Allocation is enabled - with shared claims with correct queue label",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "task-1",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0", "claim-1"},
							},
						},
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
						{
							Name:            "node1-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node1",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
						},
						{
							Name:            "claim-1",
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
		},
		{
			name:    "Dynamic Resource Allocation is enabled - template with no claim",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:  "task-1",
								State: pod_status.Pending,
								ResourceClaimTemplates: map[string]string{
									"template-0": "claim-0",
								},
							},
						},
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
						{
							Name:            "node1-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node1",
							Count:           1,
						},
					},
				},
			},
			err: "failed to get resource claim test/claim-0: could not find ResourceClaim \"test/claim-0\"",
		},
		{
			name:    "Dynamic Resource Allocation is enabled - template with claim",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:  "task-1",
								State: pod_status.Pending,
								ResourceClaimTemplates: map[string]string{
									"template-0": "claim-0",
								},
							},
						},
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
						{
							Name:            "node1-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node1",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
						},
					},
				},
			},
		},
		{
			name:    "Dynamic Resource Allocation is disabled - no claims",
			enabled: false,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "job-1-0",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{},
							},
						},
					},
				},
			},
		},
		{
			name:    "Dynamic Resource Allocation is disabled - with claims",
			enabled: false,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "job-1-0",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0", "claim-1"},
							},
						},
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
						{
							Name:            "node1-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node1",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
						},
						{
							Name:            "claim-1",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
						},
					},
				},
			},
			err: "pod test/job-1-0 cannot be scheduled, it references resource claims <claim-0, claim-1> " +
				"while dynamic resource allocation feature is not enabled in cluster",
		},
		{
			name:    "Dynamic Resource Allocation is disabled - with template",
			enabled: false,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:  "job-1-0",
								State: pod_status.Pending,
								ResourceClaimTemplates: map[string]string{
									"template-0": "claim-0",
								},
							},
						},
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
						{
							Name:            "node1-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node1",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
						},
					},
				},
			},
			err: "pod test/job-1-0 cannot be scheduled, it references resource claims <template-0> " +
				"while dynamic resource allocation feature is not enabled in cluster",
		},
		{
			name:    "Too many consumers per resource claim",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "job-1-0",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
						},
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
						{
							Name:            "node1-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node1",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "q-1",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								ReservedFor: dra_fake.RandomReservedForReferences(resourceapi.ResourceClaimReservedForMaxSize),
							},
						},
					},
				},
			},
			err: "resource claim test/claim-0 has reached its maximum number of consumers (256)",
		},
		{
			name:    "Shared claim with no queue label - blocked",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "job-1-0",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-no-label"},
							},
						},
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
							Name:            "claim-no-label",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							// No Labels - should fail
						},
					},
				},
			},
			err: "pod test/job-1-0 cannot be scheduled: DRA claim claim-no-label is a shared GPU claim but does not have a queue label (kai.scheduler/queue)",
		},
		{
			name:    "Shared claim with wrong queue label - blocked",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "job-1-0",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-wrong-queue"},
							},
						},
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
							Name:            "claim-wrong-queue",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								constants.DefaultQueueLabel: "different-queue",
							},
						},
					},
				},
			},
			err: "pod test/job-1-0 cannot be scheduled: DRA claim claim-wrong-queue is a shared GPU claim with wrong queue label (expected queue: q-1, claim queue label: different-queue)",
		},
		{
			name:    "Template claim without queue label - allowed",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:  "job-1-0",
								State: pod_status.Pending,
								ResourceClaimTemplates: map[string]string{
									"template-0": "claim-from-template",
								},
							},
						},
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
							Name:            "claim-from-template",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							// No Labels - but template claims should pass
						},
					},
				},
			},
			// No error expected - template claims skip queue label validation
		},
		{
			name:    "Non-GPU shared claim without queue label - allowed",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job-1",
						Namespace: "test",
						QueueName: "q-1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:               "job-1-0",
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"non-gpu-claim"},
							},
						},
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"network.example.com"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-network",
							DeviceClassName: "network.example.com",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "non-gpu-claim",
							Namespace:       "test",
							DeviceClassName: "network.example.com",
							Count:           1,
							// No Labels - but non-GPU claims should pass
						},
					},
				},
			},
			// No error expected - non-GPU claims skip queue label validation
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Running test number: %v, test name: %v,", i, test.name)

			featuregate.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate,
				features.DynamicResourceAllocation, test.enabled)

			ssn := test_utils.BuildSession(test.topology, controller)

			time.Sleep(1 * time.Millisecond)

			for _, job := range ssn.ClusterInfo.PodGroupInfos {
				for _, task := range job.GetAllPodsMap() {
					if task.Status == pod_status.Pending {
						err := ssn.PrePredicateFn(task, job)
						if test.err != "" {
							assert.EqualError(t, err, test.err)
						} else {
							assert.NoError(t, err)
						}
					}
				}
			}
		})
	}
}

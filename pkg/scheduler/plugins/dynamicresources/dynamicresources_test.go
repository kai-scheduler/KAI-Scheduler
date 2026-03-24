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
	"k8s.io/utils/ptr"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
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
						Name:         "job-1",
						QueueName:    "q-1",
						MinAvailable: ptr.To(int32(1)),
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
			name:    "Dynamic Resource Allocation is enabled - with claims",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:         "job-1",
						Namespace:    "test",
						QueueName:    "q-1",
						MinAvailable: ptr.To(int32(1)),
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
		},
		{
			name:    "Dynamic Resource Allocation is enabled - template with no claim",
			enabled: true,
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:         "job-1",
						Namespace:    "test",
						QueueName:    "q-1",
						MinAvailable: ptr.To(int32(1)),
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
						Name:         "job-1",
						Namespace:    "test",
						QueueName:    "q-1",
						MinAvailable: ptr.To(int32(1)),
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
						Name:         "job-1",
						Namespace:    "test",
						QueueName:    "q-1",
						MinAvailable: ptr.To(int32(1)),
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
						Name:         "job-1",
						Namespace:    "test",
						QueueName:    "q-1",
						MinAvailable: ptr.To(int32(1)),
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
						Name:         "job-1",
						Namespace:    "test",
						QueueName:    "q-1",
						MinAvailable: ptr.To(int32(1)),
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
						Name:         "job-1",
						Namespace:    "test",
						QueueName:    "q-1",
						MinAvailable: ptr.To(int32(1)),
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
							ReservedFor:     dra_fake.RandomReservedForReferences(resourceapi.ResourceClaimReservedForMaxSize),
						},
					},
				},
			},
			err: "resource claim test/claim-0 has reached its maximum number of consumers (256)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Running test number: %v, test name: %v,", i, test.name)

			featuregate.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate,
				features.DynamicResourceAllocation, test.enabled)

			ssn := test_utils.BuildSession(test.topology, controller)

			time.Sleep(1 * time.Millisecond)

			for _, job := range ssn.PodGroupInfos {
				for _, task := range job.PodInfos {
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

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_grouper

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	kaiv1common "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/pod_grouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestBuildArgsList(t *testing.T) {
	tests := []struct {
		name     string
		config   *kaiv1.Config
		expected []string
	}{
		{
			name: "basic configuration",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Global: &kaiv1.GlobalConfig{
						SchedulerName:    ptr.To(constants.DefaultSchedulerName),
						QueueLabelKey:    ptr.To(constants.DefaultQueueLabel),
						NodePoolLabelKey: ptr.To(constants.DefaultNodePoolLabelKey),
					},
					PodGrouper: &pod_grouper.PodGrouper{
						Replicas:        ptr.To(int32(1)),
						Args:            &pod_grouper.Args{},
						K8sClientConfig: &common.K8sClientConfig{},
					},
				},
			},
			expected: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--queue-label-key", constants.DefaultQueueLabel,
				"--nodepool-label-key", constants.DefaultNodePoolLabelKey,
			},
		},
		{
			name: "with namespace label selector",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Global: &kaiv1.GlobalConfig{
						SchedulerName:    ptr.To(constants.DefaultSchedulerName),
						QueueLabelKey:    ptr.To(constants.DefaultQueueLabel),
						NodePoolLabelKey: ptr.To(constants.DefaultNodePoolLabelKey),
						NamespaceLabelSelector: map[string]string{
							"environment": "production",
							"team":        "ai",
						},
					},
					PodGrouper: &pod_grouper.PodGrouper{
						Replicas:        ptr.To(int32(1)),
						Args:            &pod_grouper.Args{},
						K8sClientConfig: &common.K8sClientConfig{},
					},
				},
			},
			expected: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--queue-label-key", constants.DefaultQueueLabel,
				"--nodepool-label-key", constants.DefaultNodePoolLabelKey,
				"--namespace-label-selector", "environment=production,team=ai",
			},
		},
		{
			name: "with pod label selector",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Global: &kaiv1.GlobalConfig{
						SchedulerName:    ptr.To(constants.DefaultSchedulerName),
						QueueLabelKey:    ptr.To(constants.DefaultQueueLabel),
						NodePoolLabelKey: ptr.To(constants.DefaultNodePoolLabelKey),
						PodLabelSelector: map[string]string{
							"app": "training",
						},
					},
					PodGrouper: &pod_grouper.PodGrouper{
						Replicas:        ptr.To(int32(1)),
						Args:            &pod_grouper.Args{},
						K8sClientConfig: &common.K8sClientConfig{},
					},
				},
			},
			expected: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--queue-label-key", constants.DefaultQueueLabel,
				"--nodepool-label-key", constants.DefaultNodePoolLabelKey,
				"--pod-label-selector", "app=training",
			},
		},
		{
			name: "with both label selectors",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Global: &kaiv1.GlobalConfig{
						SchedulerName:    ptr.To(constants.DefaultSchedulerName),
						QueueLabelKey:    ptr.To(constants.DefaultQueueLabel),
						NodePoolLabelKey: ptr.To(constants.DefaultNodePoolLabelKey),
						NamespaceLabelSelector: map[string]string{
							"environment": "production",
						},
						PodLabelSelector: map[string]string{
							"app": "training",
						},
					},
					PodGrouper: &pod_grouper.PodGrouper{
						Replicas:        ptr.To(int32(1)),
						Args:            &pod_grouper.Args{},
						K8sClientConfig: &common.K8sClientConfig{},
					},
				},
			},
			expected: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--queue-label-key", constants.DefaultQueueLabel,
				"--nodepool-label-key", constants.DefaultNodePoolLabelKey,
				"--namespace-label-selector", "environment=production",
				"--pod-label-selector", "app=training",
			},
		},
		{
			name: "with gang schedule knative",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Global: &kaiv1.GlobalConfig{
						SchedulerName:    ptr.To(constants.DefaultSchedulerName),
						QueueLabelKey:    ptr.To(constants.DefaultQueueLabel),
						NodePoolLabelKey: ptr.To(constants.DefaultNodePoolLabelKey),
					},
					PodGrouper: &pod_grouper.PodGrouper{
						Replicas: ptr.To(int32(1)),
						Args: &pod_grouper.Args{
							GangScheduleKnative: ptr.To(true),
						},
						K8sClientConfig: &common.K8sClientConfig{},
					},
				},
			},
			expected: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--queue-label-key", constants.DefaultQueueLabel,
				"--nodepool-label-key", constants.DefaultNodePoolLabelKey,
				"--knative-gang-schedule=true",
			},
		},
		{
			name: "with leader election",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Global: &kaiv1.GlobalConfig{
						SchedulerName:    ptr.To(constants.DefaultSchedulerName),
						QueueLabelKey:    ptr.To(constants.DefaultQueueLabel),
						NodePoolLabelKey: ptr.To(constants.DefaultNodePoolLabelKey),
					},
					PodGrouper: &pod_grouper.PodGrouper{
						Replicas:        ptr.To(int32(2)),
						Args:            &pod_grouper.Args{},
						K8sClientConfig: &common.K8sClientConfig{},
					},
				},
			},
			expected: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--queue-label-key", constants.DefaultQueueLabel,
				"--nodepool-label-key", constants.DefaultNodePoolLabelKey,
				"--leader-elect",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildArgsList(tt.config)

			extractAndClean := func(slice []string) (cleaned []string, value string) {
				for i := 0; i < len(slice); i += 1 {
					if i+1 >= len(slice) {
						return slice, ""
					}
					if slice[i] == "--namespace-label-selector" {
						value = slice[i+1]
						return append(slice[:i], slice[i+2:]...), value
					}
				}
				return slice, ""
			}

			cleanedExpected, expectedNodePoolValue := extractAndClean(tt.expected)
			cleanedResult, actualNodePoolValue := extractAndClean(result)

			assert.Equal(t, cleanedExpected, cleanedResult,
				"unexpected difference in flags other than --namespace-label-selector")

			if expectedNodePoolValue != "" {
				assert.NotEmpty(t, actualNodePoolValue,
					"expected --namespace-label-selector in result but not found")
				expectedParts := strings.Split(expectedNodePoolValue, ",")
				actualParts := strings.Split(actualNodePoolValue, ",")
				require.Equal(t, len(expectedParts), len(actualParts),
					"--namespace-label-selector part counts do not match")
				for _, part := range expectedParts {
					assert.Contains(t, actualParts, part,
						"expected part %q missing in actual --namespace-label-selector value %q",
						part, actualNodePoolValue)
				}
			} else {
				assert.Empty(t, actualNodePoolValue,
					"--namespace-label-selector found in result but not expected")
			}
		})
	}
}

func TestFormatLabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector map[string]string
		expected string
	}{
		{
			name:     "empty selector",
			selector: map[string]string{},
			expected: "",
		},
		{
			name: "single label",
			selector: map[string]string{
				"app": "training",
			},
			expected: "app=training",
		},
		{
			name: "multiple labels",
			selector: map[string]string{
				"environment": "production",
				"team":        "ai",
				"app":         "training",
			},
			expected: "app=training,environment=production,team=ai",
		},
		{
			name: "labels with special characters",
			selector: map[string]string{
				"kubernetes.io/name":        "test",
				"app.kubernetes.io/part-of": "kai",
			},
			expected: "app.kubernetes.io/part-of=kai,kubernetes.io/name=test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLabelSelector(tt.selector)
			// Since map iteration order is not guaranteed, we need to check that all key-value pairs are present
			if tt.expected != "" {
				for key, value := range tt.selector {
					expectedPair := key + "=" + value
					assert.Contains(t, result, expectedPair)
				}
				// Check that the result has the correct number of pairs
				pairs := strings.Split(result, ",")
				assert.Equal(t, len(tt.selector), len(pairs))
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestDeploymentForKAIConfig(t *testing.T) {
	ctx := context.Background()

	config := &kaiv1.Config{
		Spec: kaiv1.ConfigSpec{
			Namespace: constants.DefaultKAINamespace,
			Global: &kaiv1.GlobalConfig{
				SchedulerName:    ptr.To(constants.DefaultSchedulerName),
				QueueLabelKey:    ptr.To(constants.DefaultQueueLabel),
				NodePoolLabelKey: ptr.To(constants.DefaultNodePoolLabelKey),
			},
			PodGrouper: &pod_grouper.PodGrouper{
				Replicas: ptr.To(int32(1)),
				Service: &kaiv1common.Service{
					Image: &common.Image{
						Name:       ptr.To("pod-grouper"),
						Repository: ptr.To("registry.local/kai-scheduler"),
						Tag:        ptr.To("latest"),
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().Build()

	config.Spec.SetDefaultsWhereNeeded()
	pg := &PodGrouper{BaseResourceName: defaultResourceName}
	deploymentObj, err := pg.deploymentForKAIConfig(ctx, client, config)
	require.NoError(t, err)
	require.NotNil(t, deploymentObj)

	deployment := deploymentObj.(*appsv1.Deployment)
	assert.Equal(t, "pod-grouper", deployment.GetName())
	assert.Equal(t, constants.DefaultKAINamespace, deployment.GetNamespace())

	assert.Equal(t, int32(1), *deployment.Spec.Replicas)

	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "pod-grouper", container.Name)
	assert.Equal(t, "registry.local/kai-scheduler/pod-grouper:latest", container.Image)

	// Check that args include the label selectors when configured
	args := container.Args
	assert.Contains(t, args, "--scheduler-name")
	assert.Contains(t, args, "--queue-label-key")
	assert.Contains(t, args, "--nodepool-label-key")
}

func TestServiceAccountForKAIConfig(t *testing.T) {
	ctx := context.Background()

	config := &kaiv1.Config{
		Spec: kaiv1.ConfigSpec{
			Namespace: constants.DefaultKAINamespace,
		},
	}

	client := fake.NewClientBuilder().Build()

	config.Spec.SetDefaultsWhereNeeded()
	pg := &PodGrouper{BaseResourceName: defaultResourceName}
	sa, err := pg.serviceAccountForKAIConfig(ctx, client, config)
	require.NoError(t, err)
	require.NotNil(t, sa)

	assert.Equal(t, "pod-grouper", sa.GetName())
	assert.Equal(t, constants.DefaultKAINamespace, sa.GetNamespace())
	assert.Equal(t, "ServiceAccount", sa.GetObjectKind().GroupVersionKind().Kind)
}

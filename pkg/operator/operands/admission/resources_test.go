// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1/admission"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeploymentForKAIConfig(t *testing.T) {
	tests := []struct {
		name            string
		config          *kaiv1.Config
		expectedArgs    []string
		notExpectedArgs []string
	}{
		{
			name: "basic configuration without GPU sharing",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						SchedulerName: ptr.To(constants.DefaultSchedulerName),
					},
					Admission: &admission.Admission{
						Replicas:   ptr.To(int32(1)),
						GPUSharing: ptr.To(false),
						Webhook: &admission.Webhook{
							TargetPort:  ptr.To(9443),
							ProbePort:   ptr.To(8081),
							MetricsPort: ptr.To(8080),
						},
					},
				},
			},
			expectedArgs: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--webhook-addr", "9443",
				"--health-probe-bind-address", ":8081",
				"--metrics-bind-address", ":8080",
			},
			notExpectedArgs: []string{
				"--gpu-sharing-enabled=true",
			},
		},
		{
			name: "configuration with GPU sharing enabled",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						SchedulerName: ptr.To(constants.DefaultSchedulerName),
					},
					Admission: &admission.Admission{
						Replicas:   ptr.To(int32(1)),
						GPUSharing: ptr.To(true),
						Webhook: &admission.Webhook{
							TargetPort:  ptr.To(9443),
							ProbePort:   ptr.To(8081),
							MetricsPort: ptr.To(8080),
						},
					},
				},
			},
			expectedArgs: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--webhook-addr", "9443",
				"--health-probe-bind-address", ":8081",
				"--metrics-bind-address", ":8080",
				"--gpu-sharing-enabled=true",
			},
		},
		{
			name: "configuration with leader election",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						SchedulerName: ptr.To(constants.DefaultSchedulerName),
					},
					Admission: &admission.Admission{
						Replicas:   ptr.To(int32(2)),
						GPUSharing: ptr.To(false),
						Webhook: &admission.Webhook{
							TargetPort:  ptr.To(9443),
							ProbePort:   ptr.To(8081),
							MetricsPort: ptr.To(8080),
						},
					},
				},
			},
			expectedArgs: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--webhook-addr", "9443",
				"--health-probe-bind-address", ":8081",
				"--metrics-bind-address", ":8080",
				"--leader-elect",
			},
		},
		{
			name: "configuration with default gpu pod runtime class",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						SchedulerName: ptr.To(constants.DefaultSchedulerName),
					},
					Admission: &admission.Admission{
						Replicas:   ptr.To(int32(2)),
						GPUSharing: ptr.To(false),
						Webhook: &admission.Webhook{
							TargetPort:  ptr.To(9443),
							ProbePort:   ptr.To(8081),
							MetricsPort: ptr.To(8080),
						},
					},
				},
			},
			expectedArgs: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--webhook-addr", "9443",
				"--health-probe-bind-address", ":8081",
				"--metrics-bind-address", ":8080",
				"--leader-elect",
				"--gpu-pod-runtime-class-name", constants.DefaultRuntimeClassName,
			},
		},
		{
			name: "configuration with custom gpu pod runtime class",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						SchedulerName: ptr.To(constants.DefaultSchedulerName),
					},
					Admission: &admission.Admission{
						Replicas:   ptr.To(int32(2)),
						GPUSharing: ptr.To(false),
						Webhook: &admission.Webhook{
							TargetPort:  ptr.To(9443),
							ProbePort:   ptr.To(8081),
							MetricsPort: ptr.To(8080),
						},
						GPUPodRuntimeClassName: ptr.To("custom-runtime-class"),
					},
				},
			},
			expectedArgs: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--webhook-addr", "9443",
				"--health-probe-bind-address", ":8081",
				"--metrics-bind-address", ":8080",
				"--leader-elect",
				"--gpu-pod-runtime-class-name", "custom-runtime-class",
			},
		},
		{
			name: "configuration with no gpu pod runtime class",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						SchedulerName: ptr.To(constants.DefaultSchedulerName),
					},
					Admission: &admission.Admission{
						Replicas:   ptr.To(int32(2)),
						GPUSharing: ptr.To(false),
						Webhook: &admission.Webhook{
							TargetPort:  ptr.To(9443),
							ProbePort:   ptr.To(8081),
							MetricsPort: ptr.To(8080),
						},
						GPUPodRuntimeClassName: ptr.To(""),
					},
				},
			},
			expectedArgs: []string{
				"--scheduler-name", constants.DefaultSchedulerName,
				"--webhook-addr", "9443",
				"--health-probe-bind-address", ":8081",
				"--metrics-bind-address", ":8080",
				"--leader-elect",
				"--gpu-pod-runtime-class-name", "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := fake.NewClientBuilder().Build()

			tt.config.Spec.SetDefaultsWhereNeeded()
			a := &Admission{BaseResourceName: defaultResourceName}
			objects, err := a.deploymentForKAIConfig(ctx, client, tt.config)
			require.NoError(t, err)
			require.Len(t, objects, 1)

			deploymentObj := objects[0]
			assert.Equal(t, "admission", deploymentObj.GetName())
			assert.Equal(t, constants.DefaultKAINamespace, deploymentObj.GetNamespace())

			deployment := deploymentObj.(*appsv1.Deployment)
			container := deployment.Spec.Template.Spec.Containers[0]
			args := container.Args

			// Check expected args
			for _, expectedArg := range tt.expectedArgs {
				assert.Contains(t, args, expectedArg)
			}

			// Check not expected args
			for _, notExpectedArg := range tt.notExpectedArgs {
				assert.NotContains(t, args, notExpectedArg)
			}
		})
	}
}

func TestMutatingWCForKAIConfig(t *testing.T) {
	tests := []struct {
		name                     string
		config                   *kaiv1.Config
		expectQueueLabelSelector bool
	}{
		{
			name: "with queue label selector enabled",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						QueueLabelKey: ptr.To(constants.DefaultQueueLabel),
						NamespaceLabelSelector: map[string]string{
							"environment": "production",
						},
						PodLabelSelector: map[string]string{
							"app": "training",
						},
					},
					Admission: &admission.Admission{
						QueueLabelSelector: ptr.To(true),
						Webhook: &admission.Webhook{
							Port:       ptr.To(443),
							TargetPort: ptr.To(9443),
						},
					},
				},
			},
			expectQueueLabelSelector: true,
		},
		{
			name: "with queue label selector disabled",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						QueueLabelKey: ptr.To(constants.DefaultQueueLabel),
						NamespaceLabelSelector: map[string]string{
							"environment": "production",
						},
						PodLabelSelector: map[string]string{
							"app": "training",
						},
					},
					Admission: &admission.Admission{
						QueueLabelSelector: ptr.To(false),
						Webhook: &admission.Webhook{
							Port:       ptr.To(443),
							TargetPort: ptr.To(9443),
						},
					},
				},
			},
			expectQueueLabelSelector: false,
		},
		{
			name: "with queue label selector not set (defaults to false)",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						QueueLabelKey: ptr.To(constants.DefaultQueueLabel),
						NamespaceLabelSelector: map[string]string{
							"environment": "production",
						},
						PodLabelSelector: map[string]string{
							"app": "training",
						},
					},
					Admission: &admission.Admission{
						Webhook: &admission.Webhook{
							Port:       ptr.To(443),
							TargetPort: ptr.To(9443),
						},
					},
				},
			},
			expectQueueLabelSelector: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := fake.NewClientBuilder().Build()

			tt.config.Spec.SetDefaultsWhereNeeded()

			a := &Admission{BaseResourceName: defaultResourceName}

			// Create a secret first since the function now requires it
			err, secret, webhookName := a.upsertKAIAdmissionCertSecret(ctx, client, tt.config)
			require.NoError(t, err)

			objects, err := a.mutatingWCForKAIConfig(ctx, client, tt.config, secret, webhookName)
			require.NoError(t, err)
			require.Len(t, objects, 1) // webhook only (secret is created separately now)

			// Find the webhook configuration
			var webhook *admissionv1.MutatingWebhookConfiguration
			for _, obj := range objects {
				if w, ok := obj.(*admissionv1.MutatingWebhookConfiguration); ok {
					webhook = w
					break
				}
			}
			require.NotNil(t, webhook, "should have mutating webhook configuration")

			// Check namespace selector
			namespaceSelector := webhook.Webhooks[0].NamespaceSelector
			assert.NotNil(t, namespaceSelector)
			assert.Contains(t, namespaceSelector.MatchLabels, "environment")
			assert.Equal(t, "production", namespaceSelector.MatchLabels["environment"])

			// Check object selector
			objectSelector := webhook.Webhooks[0].ObjectSelector
			assert.NotNil(t, objectSelector)
			assert.Contains(t, objectSelector.MatchLabels, "app")
			assert.Equal(t, "training", objectSelector.MatchLabels["app"])

			// Check queue label selector based on flag
			if tt.expectQueueLabelSelector {
				assert.Len(t, namespaceSelector.MatchExpressions, 2)
				assert.Equal(t, constants.DefaultQueueLabel, namespaceSelector.MatchExpressions[1].Key)
				assert.Equal(t, metav1.LabelSelectorOpExists, namespaceSelector.MatchExpressions[1].Operator)
			} else {
				assert.Len(t, namespaceSelector.MatchExpressions, 1)
			}
		})
	}
}

func TestValidatingWCForKAIConfig(t *testing.T) {
	tests := []struct {
		name                     string
		config                   *kaiv1.Config
		expectQueueLabelSelector bool
	}{
		{
			name: "with queue label selector enabled",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						QueueLabelKey: ptr.To(constants.DefaultQueueLabel),
						NamespaceLabelSelector: map[string]string{
							"environment": "production",
						},
						PodLabelSelector: map[string]string{
							"app": "training",
						},
					},
					Admission: &admission.Admission{
						QueueLabelSelector: ptr.To(true),
						Webhook: &admission.Webhook{
							Port:       ptr.To(443),
							TargetPort: ptr.To(9443),
						},
					},
				},
			},
			expectQueueLabelSelector: true,
		},
		{
			name: "with queue label selector disabled",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						QueueLabelKey: ptr.To(constants.DefaultQueueLabel),
						NamespaceLabelSelector: map[string]string{
							"environment": "production",
						},
						PodLabelSelector: map[string]string{
							"app": "training",
						},
					},
					Admission: &admission.Admission{
						QueueLabelSelector: ptr.To(false),
						Webhook: &admission.Webhook{
							Port:       ptr.To(443),
							TargetPort: ptr.To(9443),
						},
					},
				},
			},
			expectQueueLabelSelector: false,
		},
		{
			name: "with queue label selector not set (defaults to false)",
			config: &kaiv1.Config{
				Spec: kaiv1.ConfigSpec{
					Namespace: constants.DefaultKAINamespace,
					Global: &kaiv1.GlobalConfig{
						QueueLabelKey: ptr.To(constants.DefaultQueueLabel),
						NamespaceLabelSelector: map[string]string{
							"environment": "production",
						},
						PodLabelSelector: map[string]string{
							"app": "training",
						},
					},
					Admission: &admission.Admission{
						Webhook: &admission.Webhook{
							Port:       ptr.To(443),
							TargetPort: ptr.To(9443),
						},
					},
				},
			},
			expectQueueLabelSelector: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := fake.NewClientBuilder().Build()

			tt.config.Spec.SetDefaultsWhereNeeded()
			a := &Admission{BaseResourceName: defaultResourceName}

			// Create a secret first since the function now requires it
			err, secret, webhookName := a.upsertKAIAdmissionCertSecret(ctx, client, tt.config)
			require.NoError(t, err)

			objects, err := a.validatingWCForKAIConfig(ctx, client, tt.config, secret, webhookName)
			require.NoError(t, err)
			require.Len(t, objects, 1) // webhook only (secret is created separately now)

			webhook := objects[0].(*admissionv1.ValidatingWebhookConfiguration)
			require.NotNil(t, webhook, "should have validating webhook configuration")

			// Check namespace selector
			namespaceSelector := webhook.Webhooks[0].NamespaceSelector
			assert.NotNil(t, namespaceSelector)
			assert.Contains(t, namespaceSelector.MatchLabels, "environment")
			assert.Equal(t, "production", namespaceSelector.MatchLabels["environment"])

			// Check object selector
			objectSelector := webhook.Webhooks[0].ObjectSelector
			assert.NotNil(t, objectSelector)
			assert.Contains(t, objectSelector.MatchLabels, "app")
			assert.Equal(t, "training", objectSelector.MatchLabels["app"])

			// Check queue label selector based on flag
			if tt.expectQueueLabelSelector {
				assert.Len(t, namespaceSelector.MatchExpressions, 2)
				assert.Equal(t, constants.DefaultQueueLabel, namespaceSelector.MatchExpressions[1].Key)
				assert.Equal(t, metav1.LabelSelectorOpExists, namespaceSelector.MatchExpressions[1].Operator)
			} else {
				assert.Len(t, namespaceSelector.MatchExpressions, 1)
			}
		})
	}
}

func TestServiceAccountForKAIConfig(t *testing.T) {
	ctx := context.Background()
	client := fake.NewClientBuilder().Build()

	config := &kaiv1.Config{
		Spec: kaiv1.ConfigSpec{
			Namespace: constants.DefaultKAINamespace,
		},
	}

	a := &Admission{BaseResourceName: defaultResourceName}
	objects, err := a.serviceAccountForKAIConfig(ctx, client, config)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	sa := objects[0]
	assert.Equal(t, "admission", sa.GetName())
	assert.Equal(t, constants.DefaultKAINamespace, sa.GetNamespace())
	assert.Equal(t, "ServiceAccount", sa.GetObjectKind().GroupVersionKind().Kind)
}

func TestServiceForKAIConfig(t *testing.T) {
	ctx := context.Background()
	client := fake.NewClientBuilder().Build()

	config := &kaiv1.Config{
		Spec: kaiv1.ConfigSpec{
			Namespace: constants.DefaultKAINamespace,
			Admission: &admission.Admission{
				Webhook: &admission.Webhook{
					Port:        ptr.To(443),
					TargetPort:  ptr.To(9443),
					MetricsPort: ptr.To(8080),
				},
			},
		},
	}

	a := &Admission{BaseResourceName: defaultResourceName}
	objects, err := a.serviceForKAIConfig(ctx, client, config)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	service := objects[0]
	assert.Equal(t, "admission", service.GetName())
	assert.Equal(t, constants.DefaultKAINamespace, service.GetNamespace())
	assert.Equal(t, "Service", service.GetObjectKind().GroupVersionKind().Kind)

	// Type assert to access Spec
	serviceObj, ok := service.(*v1.Service)
	require.True(t, ok, "service should be of type *v1.Service")

	// Check ports
	assert.Len(t, serviceObj.Spec.Ports, 2)

	// Find webhook port
	var webhookPort *v1.ServicePort
	for _, port := range serviceObj.Spec.Ports {
		if port.Name == "webhook" {
			webhookPort = &port
			break
		}
	}
	require.NotNil(t, webhookPort, "should have webhook port")
	assert.Equal(t, int32(443), webhookPort.Port)
	assert.Equal(t, int32(9443), webhookPort.TargetPort.IntVal)

	// Find metrics port
	var metricsPort *v1.ServicePort
	for _, port := range serviceObj.Spec.Ports {
		if port.Name == "http-metrics" {
			metricsPort = &port
			break
		}
	}
	require.NotNil(t, metricsPort, "should have metrics port")
	assert.Equal(t, int32(8080), metricsPort.Port)
	assert.Equal(t, int32(8080), metricsPort.TargetPort.IntVal)
}

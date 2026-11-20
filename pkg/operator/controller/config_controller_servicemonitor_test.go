// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/controller/status_reconciler"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/known_types"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/prometheus"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaiprometheus "github.com/kai-scheduler/api/kai/v1/prometheus"
)

func TestReconcileConfigServiceMonitors(t *testing.T) {
	tests := []struct {
		name                  string
		prometheus            *kaiprometheus.Prometheus
		retainedPrometheus    bool
		expectServiceMonitors bool
	}{
		{
			name:                  "KAI prometheus enabled",
			prometheus:            &kaiprometheus.Prometheus{Enabled: ptr.To(true)},
			expectServiceMonitors: true,
		},
		{
			name:                  "KAI prometheus disabled and retained until its retention period ends",
			prometheus:            &kaiprometheus.Prometheus{Enabled: ptr.To(false)},
			retainedPrometheus:    true,
			expectServiceMonitors: false,
		},
		{
			name: "KAI prometheus disabled and retained, service monitors enabled",
			prometheus: &kaiprometheus.Prometheus{
				Enabled:        ptr.To(false),
				ServiceMonitor: &kaiprometheus.ServiceMonitor{Enabled: ptr.To(true)},
			},
			retainedPrometheus:    true,
			expectServiceMonitors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			testScheme := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(testScheme))
			require.NoError(t, kaiv1.AddToScheme(testScheme))
			require.NoError(t, monitoringv1.AddToScheme(testScheme))
			require.NoError(t, apiextensionsv1.AddToScheme(testScheme))
			require.NoError(t, vpav1.AddToScheme(testScheme))

			kaiConfig := &kaiv1.Config{
				ObjectMeta: metav1.ObjectMeta{Name: known_types.SingletonInstanceName},
				Spec: kaiv1.ConfigSpec{
					Namespace:  "kai-scheduler",
					Prometheus: tt.prometheus,
				},
			}
			objects := []client.Object{
				kaiConfig,
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "prometheuses.monitoring.coreos.com"},
				},
				&apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: "servicemonitors.monitoring.coreos.com"},
				},
			}
			if tt.retainedPrometheus {
				objects = append(objects, &monitoringv1.Prometheus{
					ObjectMeta: metav1.ObjectMeta{Name: kaiprometheus.DefaultInstanceName, Namespace: "kai-scheduler"},
				})
			}
			builder := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(objects...).
				WithStatusSubresource(kaiConfig)
			for _, collectable := range known_types.KAIConfigRegisteredCollectible {
				if collectable.InitWithFakeClientBuilder != nil {
					collectable.InitWithFakeClientBuilder(builder)
				}
			}
			fakeClient := builder.Build()

			reconciler := &ConfigReconciler{Client: fakeClient, Scheme: testScheme}
			reconciler.SetOperands([]operands.Operand{&prometheus.Prometheus{}})
			reconciler.StatusReconciler = status_reconciler.New(fakeClient, reconciler.deployable)
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(kaiConfig)})
			require.NoError(t, err)

			serviceMonitors := &monitoringv1.ServiceMonitorList{}
			require.NoError(t, fakeClient.List(ctx, serviceMonitors, client.InNamespace("kai-scheduler")))
			require.Equal(t, tt.expectServiceMonitors, len(serviceMonitors.Items) > 0)
		})
	}
}

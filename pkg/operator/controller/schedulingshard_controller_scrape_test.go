// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/known_types"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaiprometheus "github.com/kai-scheduler/api/kai/v1/prometheus"
)

func TestReconcileSchedulerServiceScrapeAnnotation(t *testing.T) {
	tests := []struct {
		name       string
		prometheus *kaiprometheus.Prometheus
		expected   string
	}{
		{
			name:       "no prometheus config",
			prometheus: nil,
			expected:   "true",
		},
		{
			name:       "prometheus disabled",
			prometheus: &kaiprometheus.Prometheus{Enabled: ptr.To(false)},
			expected:   "true",
		},
		{
			name: "service monitors enabled without the KAI prometheus",
			prometheus: &kaiprometheus.Prometheus{
				Enabled:        ptr.To(false),
				ServiceMonitor: &kaiprometheus.ServiceMonitor{Enabled: ptr.To(true)},
			},
			expected: "false",
		},
		{
			name:       "KAI prometheus enabled",
			prometheus: &kaiprometheus.Prometheus{Enabled: ptr.To(true)},
			expected:   "false",
		},
		{
			name: "KAI prometheus enabled with service monitors disabled",
			prometheus: &kaiprometheus.Prometheus{
				Enabled:        ptr.To(true),
				ServiceMonitor: &kaiprometheus.ServiceMonitor{Enabled: ptr.To(false)},
			},
			expected: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			testScheme := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(testScheme))
			require.NoError(t, kaiv1.AddToScheme(testScheme))
			require.NoError(t, vpav1.AddToScheme(testScheme))

			shard := &kaiv1.SchedulingShard{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			kaiConfig := &kaiv1.Config{
				ObjectMeta: metav1.ObjectMeta{Name: "kai-config"},
				Spec: kaiv1.ConfigSpec{
					Namespace:  "kai-scheduler",
					Prometheus: tt.prometheus,
				},
			}
			builder := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(shard, kaiConfig).
				WithStatusSubresource(shard)
			for _, collectable := range known_types.SchedulingShardRegisteredCollectable {
				if collectable.InitWithFakeClientBuilder != nil {
					collectable.InitWithFakeClientBuilder(builder)
				}
			}
			fakeClient := builder.Build()

			reconciler := NewSchedulingShardReconciler(fakeClient, testScheme)
			reconciler.SetOperands(OperandsForShard)
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(shard)})
			require.NoError(t, err)

			service := &corev1.Service{}
			require.NoError(t, fakeClient.Get(ctx,
				client.ObjectKey{Namespace: "kai-scheduler", Name: "kai-scheduler-default"}, service))
			require.Equal(t, tt.expected, service.Annotations["prometheus.io/scrape"])
		})
	}
}

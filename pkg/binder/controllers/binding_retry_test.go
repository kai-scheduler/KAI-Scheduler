// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding"
	bindingmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/mock"
	rrmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation/mock"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestUnknownBindingAttemptPreservesOutcome(t *testing.T) {
	for _, scenario := range []string{"pending request", "retryable failed request", "Pod read fails after restart", "Node read fails after restart", "panic during binding"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, v1.AddToScheme(scheme))
			require.NoError(t, schedulingv1alpha2.AddToScheme(scheme))
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "workloads", Name: "pod", UID: "pod-uid", Labels: map[string]string{"runai-gpu-group": "allocated"}},
				Status:     v1.PodStatus{Conditions: []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}},
			}
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "selected-node"}}
			br := &schedulingv1alpha2.BindRequest{
				ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: "request", UID: "request-uid"},
				Spec: schedulingv1alpha2.BindRequestSpec{
					PodName: pod.Name, SelectedNode: node.Name, BackoffLimit: ptr.To(int32(3)),
				},
				Status: schedulingv1alpha2.BindRequestStatus{Phase: schedulingv1alpha2.BindRequestPhasePending},
			}
			if scenario == "retryable failed request" {
				br.Status.Phase = schedulingv1alpha2.BindRequestPhaseFailed
				br.Status.FailedAttempts = 1
				br.Status.Reason = "previous completed failure"
			}
			backing := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, node, br).
				WithStatusSubresource(&schedulingv1alpha2.BindRequest{}, &v1.Pod{}).Build()
			require.NoError(t, backing.Get(ctx, client.ObjectKeyFromObject(br), br))
			require.NoError(t, backing.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			originalRequest, originalPod := br.DeepCopy(), pod.DeepCopy()
			kubeClient := interceptor.NewClient(backing, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					switch obj.(type) {
					case *v1.Pod:
						if scenario == "Pod read fails after restart" {
							return io.ErrUnexpectedEOF
						}
					case *v1.Node:
						if scenario == "Node read fails after restart" {
							return io.ErrUnexpectedEOF
						}
					}
					return c.Get(ctx, key, obj, opts...)
				},
			})
			controller := gomock.NewController(t)
			binder := bindingmock.NewMockInterface(controller)
			if scenario == "pending request" || scenario == "retryable failed request" {
				binder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(fmt.Errorf("%w: %w", binding.ErrBindingOutcomeUnknown, io.ErrUnexpectedEOF))
			}
			if scenario == "panic during binding" {
				binder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(context.Context, *v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest) error {
						panic("unknown completion")
					})
			}
			// No Rollback expectation: every case must preserve a potentially live allocation.
			rrs := rrmock.NewMockInterface(controller)
			events := record.NewFakeRecorder(10)
			reconciler := NewBindRequestReconciler(kubeClient, scheme, events, &ReconcilerParams{}, binder, rrs)
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(br)})
			if scenario == "panic during binding" {
				require.ErrorContains(t, err, "unknown completion")
			} else {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			}
			assert.NotErrorIs(t, err, reconcile.TerminalError(nil))
			assert.Empty(t, result)
			actualRequest := &schedulingv1alpha2.BindRequest{}
			actualPod := &v1.Pod{}
			require.NoError(t, backing.Get(ctx, client.ObjectKeyFromObject(br), actualRequest))
			require.NoError(t, backing.Get(ctx, client.ObjectKeyFromObject(pod), actualPod))
			assert.Equal(t, originalRequest, actualRequest)
			assert.Equal(t, originalPod, actualPod)
			assert.Empty(t, events.Events)
		})
	}
}

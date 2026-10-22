// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding"
	rrmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestBindingRetryObservationsAPIServer(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("requires envtest API server assets")
	}
	environment := &envtest.Environment{}
	config, err := environment.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, environment.Stop()) })
	apiClient, err := client.NewWithWatch(config, client.Options{})
	require.NoError(t, err)
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	require.NoError(t, schedulingv1alpha2.AddToScheme(scheme))

	for _, scenario := range []string{"deleted Node progresses after excluding old Binding", "deleted Pod progresses without reports", "cached Pod miss refreshes existing Pod"} {
		t.Run(scenario, func(t *testing.T) {
			namespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "retry-observation-"}}
			require.NoError(t, apiClient.Create(ctx, namespace))
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "worker"},
				Spec: v1.PodSpec{Containers: []v1.Container{{Name: "worker", Image: "registry.k8s.io/pause:3.10"}}}}
			require.NoError(t, apiClient.Create(ctx, pod))
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: namespace.Name + "-node"}}
			oldBinding := &v1.Binding{
				ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name, UID: pod.UID, ResourceVersion: pod.ResourceVersion},
				Target:     v1.ObjectReference{Kind: "Node", Name: node.Name},
			}
			request := &schedulingv1alpha2.BindRequest{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "request"},
				Spec: schedulingv1alpha2.BindRequestSpec{PodName: pod.Name, SelectedNode: node.Name,
					BackoffLimit: ptr.To(int32(2)), ReceivedResourceType: common.ReceivedTypeRegular},
				Status: schedulingv1alpha2.BindRequestStatus{Phase: schedulingv1alpha2.BindRequestPhasePending},
			}
			requests := fake.NewClientBuilder().WithScheme(scheme).WithObjects(request).
				WithStatusSubresource(&schedulingv1alpha2.BindRequest{}).Build()
			controllerClient := interceptor.NewClient(apiClient, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*schedulingv1alpha2.BindRequest); ok {
						return requests.Get(ctx, key, obj, opts...)
					}
					if _, ok := obj.(*v1.Pod); ok && scenario == "cached Pod miss refreshes existing Pod" {
						return apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, key.Name)
					}
					return c.Get(ctx, key, obj, opts...)
				},
				SubResourcePatch: func(ctx context.Context, c client.Client, subresource string, obj client.Object,
					patch client.Patch, opts ...client.SubResourcePatchOption) error {
					if _, ok := obj.(*schedulingv1alpha2.BindRequest); ok {
						return requests.SubResource(subresource).Patch(ctx, obj, patch, opts...)
					}
					return c.SubResource(subresource).Patch(ctx, obj, patch, opts...)
				},
			})
			rrs := rrmock.NewMockInterface(gomock.NewController(t))
			if scenario == "cached Pod miss refreshes existing Pod" {
				require.NoError(t, apiClient.Create(ctx, node))
				rrs.EXPECT().SyncForNode(gomock.Any(), node.Name).Return(nil)
			}
			if scenario == "deleted Pod progresses without reports" {
				require.NoError(t, apiClient.Delete(ctx, pod, client.GracePeriodSeconds(0)))
			}
			binder := binding.NewBinder(apiClient, apiClient, rrs, plugins.New())
			events := record.NewFakeRecorder(10)
			reconciler := NewBindRequestReconciler(controllerClient, scheme, events, &ReconcilerParams{}, binder, rrs)
			reconciler.apiReader = apiClient
			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)}
			result, err := reconciler.Reconcile(ctx, req)
			actualRequest := &schedulingv1alpha2.BindRequest{}
			require.NoError(t, requests.Get(ctx, req.NamespacedName, actualRequest))
			if scenario == "cached Pod miss refreshes existing Pod" {
				require.NoError(t, err)
				assert.Equal(t, schedulingv1alpha2.BindRequestPhaseSucceeded, actualRequest.Status.Phase)
				actualPod := &v1.Pod{}
				require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(pod), actualPod))
				assert.Equal(t, node.Name, actualPod.Spec.NodeName)
				return
			}
			require.Error(t, err)
			assert.NotErrorIs(t, err, reconcile.TerminalError(nil))
			assert.Positive(t, result.RequeueAfter)
			assert.Equal(t, schedulingv1alpha2.BindRequestPhaseFailed, actualRequest.Status.Phase)
			assert.EqualValues(t, 1, actualRequest.Status.FailedAttempts)
			if scenario == "deleted Pod progresses without reports" {
				assert.Empty(t, events.Events)
				// A new Pod at the same name still cannot accept the old UID/RV.
				replacement := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name}, Spec: pod.Spec}
				require.NoError(t, apiClient.Create(ctx, replacement))
				err := apiClient.SubResource("binding").Create(ctx, replacement, oldBinding)
				require.True(t, apierrors.IsConflict(err), "%v", err)
				require.NoError(t, apiClient.Delete(ctx, replacement, client.GracePeriodSeconds(0)))
			} else {
				err := apiClient.SubResource("binding").Create(ctx, pod, oldBinding)
				require.True(t, apierrors.IsConflict(err), "%v", err)
			}
			_, err = reconciler.Reconcile(ctx, req)
			require.Error(t, err)
			require.NoError(t, requests.Get(ctx, req.NamespacedName, actualRequest))
			assert.EqualValues(t, 2, actualRequest.Status.FailedAttempts)
			assert.False(t, isActionableBindRequest(actualRequest), "authoritative absence must eventually permit rescheduling")
		})
	}
}

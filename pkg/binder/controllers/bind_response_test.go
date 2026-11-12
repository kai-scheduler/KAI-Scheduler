// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	"github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

// The binding API can commit before its response is lost. A transport error alone
// must not permit deleting the allocation that the now-bound Pod is using.
func TestReconcileBindingResponse(t *testing.T) {
	for _, tt := range []struct {
		name           string
		commit         bool
		response       error
		preBindFailure bool
		wantBound      bool
		unknown        bool
	}{
		{name: "successful response", commit: true, wantBound: true},
		{name: "committed binding with lost response", commit: true, response: io.ErrUnexpectedEOF, wantBound: true},
		{name: "definite binding rejection", response: apierrors.NewBadRequest("binding rejected")},
		{name: "PreBind failure still rolls back", preBindFailure: true},
		{name: "unknown delayed binding preserves allocations", response: io.ErrUnexpectedEOF, unknown: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, v1.AddToScheme(scheme))
			require.NoError(t, schedulingv1alpha2.AddToScheme(scheme))
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "workloads", Name: "fractional-pod", UID: "pod-uid",
					Annotations: map[string]string{"runai/shared-gpu-configmap": "fractional-pod-gpu"},
				},
				Spec:   v1.PodSpec{Containers: []v1.Container{{Name: "worker"}}},
				Status: v1.PodStatus{Phase: v1.PodPending},
			}
			common.AddGPUSharingEnvVars(&pod.Spec.Containers[0], "fractional-pod-gpu-0")
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "selected-node", Labels: map[string]string{constants.NvidiaGpuMemory: "8192"},
			}}
			if tt.preBindFailure {
				delete(node.Labels, constants.NvidiaGpuMemory)
			}
			br := &schedulingv1alpha2.BindRequest{
				ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: "bind-request", UID: "request-uid"},
				Spec: schedulingv1alpha2.BindRequestSpec{
					PodName: pod.Name, SelectedNode: node.Name,
					ReceivedResourceType: common.ReceivedTypeFraction,
					SelectedGPUGroups:    []string{"allocated-group"},
					ReceivedGPU:          &schedulingv1alpha2.ReceivedGPU{Count: 1, Portion: "0.5"},
				},
				Status: schedulingv1alpha2.BindRequestStatus{Phase: schedulingv1alpha2.BindRequestPhasePending},
			}
			reservation := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: resourceReservationNameSpace, Name: "reservation", UID: "reservation-uid",
					Labels:      map[string]string{constants.GPUGroup: "allocated-group"},
					Annotations: map[string]string{"run.ai/reserve_for_gpu_index": "GPU-0"},
				},
				Spec:   v1.PodSpec{NodeName: node.Name},
				Status: v1.PodStatus{Phase: v1.PodRunning},
			}
			bindCalls := 0
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(pod, node, br, reservation).
				WithStatusSubresource(&schedulingv1alpha2.BindRequest{}, &v1.Pod{}).
				WithIndex(&v1.Pod{}, "spec.nodeName", nodeNameIndexer).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						// Simulate the identity assigned by the API, including for guarded rollback deletion.
						if cm, ok := obj.(*v1.ConfigMap); ok && cm.UID == "" {
							cm.UID = types.UID("configmap-" + cm.Namespace + "-" + cm.Name)
						}
						return c.Create(ctx, obj, opts...)
					},
					SubResource: func(c client.WithWatch, subresource string) client.SubResourceClient {
						if subresource != "binding" {
							return c.SubResource(subresource)
						}
						return &bindingResponseSubresource{
							SubResourceClient: c.SubResource(subresource),
							create: func(ctx context.Context, obj, subresource client.Object) error {
								bindCalls++
								request := subresource.(*v1.Binding)
								require.Equal(t, pod.UID, request.UID)
								require.NotEmpty(t, request.ResourceVersion)
								require.Equal(t, node.Name, request.Target.Name)
								storedPod := &v1.Pod{}
								require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), storedPod))
								require.Equal(t, "allocated-group", storedPod.Labels[constants.GPUGroup])
								for _, name := range []string{"fractional-pod-gpu-0", "fractional-pod-gpu-0-evar"} {
									require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: name}, &v1.ConfigMap{}))
								}
								if tt.commit {
									storedPod.Spec.NodeName = request.Target.Name
									require.NoError(t, c.Update(ctx, storedPod))
								}
								return tt.response
							},
						}
					},
				}).Build()
			rrs := resourcereservation.NewService(false, kubeClient, "", time.Second,
				resourceReservationNameSpace, resourceReservationServiceAccount, resourceReservationAppLabelValue,
				scalingPodsNamespace, "", nil, nil, nil, false)
			binderPlugins := plugins.New()
			binderPlugins.RegisterPlugin(gpusharing.New(kubeClient, false, false))
			confirmationReads := 0
			apiReader := interceptor.NewClient(kubeClient, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object,
					opts ...client.GetOption) error {
					confirmationReads++
					if tt.unknown && bindCalls > 0 {
						return io.ErrUnexpectedEOF
					}
					return c.Get(ctx, key, obj, opts...)
				},
			})
			binder := binding.NewBinder(kubeClient, apiReader, rrs, binderPlugins)
			events := record.NewFakeRecorder(10)
			reconciler := NewBindRequestReconciler(kubeClient, scheme, events,
				&ReconcilerParams{}, binder, rrs)

			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(br)})
			if tt.preBindFailure {
				assert.Zero(t, bindCalls)
			} else {
				assert.Equal(t, 1, bindCalls)
			}
			if tt.response != nil && tt.commit {
				assert.Equal(t, 3, confirmationReads)
			} else if tt.response == nil && !tt.preBindFailure {
				assert.Equal(t, 2, confirmationReads)
			}
			actualPod := &v1.Pod{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(pod), actualPod))
			actualRequest := &schedulingv1alpha2.BindRequest{}
			require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(br), actualRequest))
			if tt.unknown {
				assert.ErrorIs(t, err, binding.ErrBindingOutcomeUnknown)
				assert.Equal(t, schedulingv1alpha2.BindRequestPhasePending, actualRequest.Status.Phase)
				assert.Zero(t, actualRequest.Status.FailedAttempts)
				assert.Equal(t, "allocated-group", actualPod.Labels[constants.GPUGroup])
				assert.Empty(t, actualPod.Status.Conditions)
				assert.Empty(t, events.Events)
			} else if tt.wantBound {
				assert.NoError(t, err)
				assert.Equal(t, pod.UID, actualPod.UID)
				assert.Equal(t, node.Name, actualPod.Spec.NodeName)
				assert.Equal(t, "allocated-group", actualPod.Labels[constants.GPUGroup], "a committed bind must retain its GPU reservation connection")
				assert.Equal(t, schedulingv1alpha2.BindRequestPhaseSucceeded, actualRequest.Status.Phase)
			} else {
				assert.Error(t, err)
				assert.Empty(t, actualPod.Spec.NodeName)
				assert.Empty(t, actualPod.Labels[constants.GPUGroup])
				assert.Equal(t, schedulingv1alpha2.BindRequestPhaseFailed, actualRequest.Status.Phase)
			}
			for _, name := range []string{"fractional-pod-gpu-0", "fractional-pod-gpu-0-evar"} {
				cm := &v1.ConfigMap{}
				err := kubeClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: name}, cm)
				if tt.wantBound || tt.unknown {
					assert.NoError(t, err, "a committed bind must retain ConfigMap %s", name)
				} else {
					assert.True(t, apierrors.IsNotFound(err), "a rejected bind should clean up ConfigMap %s", name)
				}
			}
		})
	}
}

type bindingResponseSubresource struct {
	client.SubResourceClient
	create func(context.Context, client.Object, client.Object) error
}

func (s *bindingResponseSubresource) Create(ctx context.Context, obj, subresource client.Object,
	_ ...client.SubResourceCreateOption) error {
	return s.create(ctx, obj, subresource)
}

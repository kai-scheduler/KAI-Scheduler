// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	"github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

// Keep a real pre-bind Pod snapshot in the controller cache while allocation,
// Binding, and cleanup use the API server.
func TestStatusRetryPreservesBindingResourcesAPIServer(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("requires envtest API server assets")
	}
	environment := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "deployments", "kai-scheduler", "crds")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := environment.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, environment.Stop()) })
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	require.NoError(t, schedulingv1alpha2.AddToScheme(scheme))
	direct, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, direct.Create(ctx, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: resourceReservationNameSpace}}))

	for _, failFirstStatus := range []bool{false, true} {
		name := "ordinary-success-control"
		if failFirstStatus {
			name = "status-retry-preserves-resources"
		}
		t.Run(name, func(t *testing.T) {
			namespace := name
			require.NoError(t, direct.Create(ctx, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "fractional-pod",
					Annotations: map[string]string{"runai/shared-gpu-configmap": "fractional-pod-gpu"}},
				Spec: v1.PodSpec{SchedulerName: "kai-scheduler",
					Containers: []v1.Container{{Name: "worker", Image: "registry.k8s.io/pause:3.10"}}},
			}
			common.AddGPUSharingEnvVars(&pod.Spec.Containers[0], "fractional-pod-gpu-0")
			require.NoError(t, direct.Create(ctx, pod))
			pod.Status.Phase = v1.PodPending
			require.NoError(t, direct.Status().Update(ctx, pod))
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name,
				Labels: map[string]string{constants.NvidiaGpuMemory: "8192"}}}
			require.NoError(t, direct.Create(ctx, node))
			group := name + "-group"
			br := &schedulingv1alpha2.BindRequest{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "bind-request"},
				Spec: schedulingv1alpha2.BindRequestSpec{PodName: pod.Name, SelectedNode: node.Name,
					ReceivedResourceType: common.ReceivedTypeFraction,
					SelectedGPUGroups:    []string{group},
					ReceivedGPU:          &schedulingv1alpha2.ReceivedGPU{Count: 1, Portion: "0.5"}},
			}
			require.NoError(t, direct.Create(ctx, br))
			br.Status.Phase = schedulingv1alpha2.BindRequestPhasePending
			require.NoError(t, direct.Status().Update(ctx, br))
			reservation := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: resourceReservationNameSpace, Name: name,
					Labels:      map[string]string{constants.GPUGroup: group},
					Annotations: map[string]string{"run.ai/reserve_for_gpu_index": "GPU-0"}},
				Spec: v1.PodSpec{NodeName: node.Name,
					Containers: []v1.Container{{Name: "reservation", Image: "registry.k8s.io/pause:3.10"}}},
			}
			require.NoError(t, direct.Create(ctx, reservation))
			reservation.Status.Phase = v1.PodRunning
			require.NoError(t, direct.Status().Update(ctx, reservation))

			bindCalls, statusWrites, cachedGets, boundPatchResponses := 0, 0, 0, 0
			var cachedPod *v1.Pod
			var bindingErrors []string
			statusFailure := errors.New("injected first BindRequest status write failure")
			pipelineClient := interceptor.NewClient(direct, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object,
					opts ...client.GetOption) error {
					if target, ok := obj.(*v1.Pod); ok && key == client.ObjectKeyFromObject(pod) && cachedPod != nil {
						cachedGets++
						cachedPod.DeepCopyInto(target)
						return nil
					}
					return c.Get(ctx, key, obj, opts...)
				},
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch,
					opts ...client.PatchOption) error {
					err := c.Patch(ctx, obj, patch, opts...)
					if current, ok := obj.(*v1.Pod); ok && current.Name == pod.Name && current.Spec.NodeName != "" && err == nil {
						boundPatchResponses++
						t.Logf("Pod PATCH decoded authoritative NodeName=%q into the binder's Pod", current.Spec.NodeName)
					}
					return err
				},
				SubResource: func(c client.WithWatch, subresource string) client.SubResourceClient {
					base := c.SubResource(subresource)
					switch subresource {
					case "binding":
						return &statusRetrySubresource{SubResourceClient: base,
							create: func(ctx context.Context, obj, request client.Object, opts ...client.SubResourceCreateOption) error {
								bindCalls++
								if bindCalls == 1 {
									cachedPod = &v1.Pod{}
									require.NoError(t, direct.Get(ctx, client.ObjectKeyFromObject(pod), cachedPod))
									require.Empty(t, cachedPod.Spec.NodeName)
									require.Equal(t, group, cachedPod.Labels[constants.GPUGroup])
								}
								err := base.Create(ctx, obj, request, opts...)
								if err != nil {
									bindingErrors = append(bindingErrors, err.Error())
									t.Logf("Binding API response: conflict=%t error=%v", apierrors.IsConflict(err), err)
								}
								return err
							}}
					case "status":
						return &statusRetrySubresource{SubResourceClient: base,
							patch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
								if _, ok := obj.(*schedulingv1alpha2.BindRequest); ok {
									statusWrites++
									if failFirstStatus && statusWrites == 1 {
										return statusFailure
									}
								}
								return base.Patch(ctx, obj, patch, opts...)
							}}
					default:
						return base
					}
				},
			})
			rrs := resourcereservation.NewService(false, pipelineClient, "", time.Second,
				resourceReservationNameSpace, resourceReservationServiceAccount, resourceReservationAppLabelValue,
				scalingPodsNamespace, "", nil, nil, nil, false)
			binderPlugins := plugins.New()
			binderPlugins.RegisterPlugin(gpusharing.New(pipelineClient, false, false))
			// A nil reader isolates the controller's resource preservation
			// from the binder retry protocol.
			binder := binding.NewBinder(pipelineClient, nil, rrs, binderPlugins)
			reconciler := NewBindRequestReconciler(pipelineClient, scheme, record.NewFakeRecorder(20), &ReconcilerParams{}, binder, rrs)
			reconciler.apiReader = direct
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(br)}
			_, err := reconciler.Reconcile(ctx, request)
			if failFirstStatus {
				require.ErrorIs(t, err, statusFailure)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, bindCalls)
			assertStatusRetryResources(t, ctx, direct, pod, node.Name, group)
			actualRequest := &schedulingv1alpha2.BindRequest{}
			require.NoError(t, direct.Get(ctx, client.ObjectKeyFromObject(br), actualRequest))
			if failFirstStatus {
				require.Equal(t, schedulingv1alpha2.BindRequestPhasePending, actualRequest.Status.Phase)
			}
			_, retryErr := reconciler.Reconcile(ctx, request)
			require.NoError(t, direct.Get(ctx, client.ObjectKeyFromObject(br), actualRequest))
			t.Logf("after second reconcile: bindCalls=%d cachedGets=%d boundPatchResponses=%d statusWrites=%d phase=%s bindingErrors=%v retryError=%v",
				bindCalls, cachedGets, boundPatchResponses, statusWrites, actualRequest.Status.Phase, bindingErrors, retryErr)
			assert.NoError(t, retryErr)
			assert.Equal(t, 1, bindCalls, "a status-only retry must not bind the already-bound Pod")
			assert.Equal(t, schedulingv1alpha2.BindRequestPhaseSucceeded, actualRequest.Status.Phase)
			assertStatusRetryResources(t, ctx, direct, pod, node.Name, group)
			actualReservation := &v1.Pod{}
			require.NoError(t, direct.Get(ctx, client.ObjectKeyFromObject(reservation), actualReservation))
			t.Logf("reservation retained: uid=%s deletionTimestamp=%v", actualReservation.UID, actualReservation.DeletionTimestamp)
		})
	}
}

func assertStatusRetryResources(t *testing.T, ctx context.Context, direct client.Client, pod *v1.Pod, node, group string) {
	t.Helper()
	actual := &v1.Pod{}
	require.NoError(t, direct.Get(ctx, client.ObjectKeyFromObject(pod), actual))
	assert.Equal(t, pod.UID, actual.UID)
	assert.Equal(t, node, actual.Spec.NodeName)
	assert.Equal(t, group, actual.Labels[constants.GPUGroup], "bound Pod must retain its GPU-group connection")
	t.Logf("Pod resources: uid=%s node=%q group=%q", actual.UID, actual.Spec.NodeName, actual.Labels[constants.GPUGroup])
	for _, name := range []string{"fractional-pod-gpu-0", "fractional-pod-gpu-0-evar"} {
		cm := &v1.ConfigMap{}
		err := direct.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: name}, cm)
		assert.NoError(t, err, "bound Pod must retain ConfigMap %s", name)
		t.Logf("ConfigMap %s: uid=%s present=%t error=%v", name, cm.UID, err == nil, err)
	}
}

type statusRetrySubresource struct {
	client.SubResourceClient
	create func(context.Context, client.Object, client.Object, ...client.SubResourceCreateOption) error
	patch  func(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error
}

func (s *statusRetrySubresource) Create(ctx context.Context, obj, request client.Object, opts ...client.SubResourceCreateOption) error {
	if s.create == nil {
		return fmt.Errorf("unexpected subresource Create")
	}
	return s.create(ctx, obj, request, opts...)
}

func (s *statusRetrySubresource) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	if s.patch == nil {
		return fmt.Errorf("unexpected subresource Patch")
	}
	return s.patch(ctx, obj, patch, opts...)
}

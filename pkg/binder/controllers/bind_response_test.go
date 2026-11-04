// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"testing"

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
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	gpusharing "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	"github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
	"github.com/kai-scheduler/api/utilities/resources"
)

const sharedGPUConfigMapPrefix = "shared-gpu-settings"

// lostResponseError is a client-side connection failure returned after the
// apiserver has already accepted the binding.
type lostResponseError struct{}

func (lostResponseError) Error() string   { return "connection reset by peer" }
func (lostResponseError) Timeout() bool   { return false }
func (lostResponseError) Temporary() bool { return true }

// TestLostBindingResponseKeepsAssignedPodSettings reproduces a placement whose
// binding call applies the assignment and then loses the response. The running
// pod must keep its GPU-group label and sharing ConfigMaps, and the BindRequest
// must be recorded as successful.
func TestLostBindingResponseKeepsAssignedPodSettings(t *testing.T) {
	ctx := context.Background()
	apiClient, pod, node, request := reconcileFractionalBind(t, ctx, true)

	require.Equal(t, node.Name, pod.Spec.NodeName, "assignment must be stored before the client error")

	if got := pod.Labels[constants.GPUGroup]; got != request.Spec.SelectedGPUGroups[0] {
		t.Errorf("GPU-group label = %q, want %q; lost binding response cleaned up an assigned pod",
			got, request.Spec.SelectedGPUGroups[0])
	}
	assertSharingConfigMaps(t, ctx, apiClient, pod)

	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(request), request))
	if request.Status.Phase != schedulingv1alpha2.BindRequestPhaseSucceeded {
		t.Errorf("BindRequest phase = %q, want %q", request.Status.Phase, schedulingv1alpha2.BindRequestPhaseSucceeded)
	}
}

// TestBindingErrorWithoutAssignmentStillRollsBack keeps the failure path: a
// binding error with no assignment still drops the settings prepared for it.
func TestBindingErrorWithoutAssignmentStillRollsBack(t *testing.T) {
	ctx := context.Background()
	apiClient, pod, _, request := reconcileFractionalBind(t, ctx, false)

	require.Empty(t, pod.Spec.NodeName)
	require.Empty(t, pod.Labels[constants.GPUGroup])
	for _, name := range sharingConfigMapNames(t, pod) {
		err := apiClient.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: name}, &v1.ConfigMap{})
		require.Truef(t, apierrors.IsNotFound(err), "configmap %s still exists after a failed bind: %v", name, err)
	}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(request), request))
	require.Equal(t, schedulingv1alpha2.BindRequestPhaseFailed, request.Status.Phase)
}

// reconcileFractionalBind runs one bind reconcile. When applyAssignment is set,
// the binding subresource stores the selected node and then returns a
// connection error, matching a lost successful response.
func reconcileFractionalBind(t *testing.T, ctx context.Context, applyAssignment bool) (
	client.Client, *v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest,
) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	require.NoError(t, schedulingv1alpha2.AddToScheme(scheme))

	apiClient := fake.NewClientBuilder().WithScheme(scheme).
		WithIndex(&v1.Pod{}, "spec.nodeName", nodeNameIndexer).
		WithStatusSubresource(&schedulingv1alpha2.BindRequest{}, &v1.Pod{}).
		Build()

	pod, node, request := createFractionalBindObjects(t, apiClient)
	podKey := client.ObjectKeyFromObject(pod)
	// The binder client stands in for the informer cache: pod Gets keep the
	// pre-bind node name even after the binding subresource has stored it.
	binderClient := interceptor.NewClient(apiClient, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if err := c.Get(ctx, key, obj, opts...); err != nil {
				return err
			}
			if stale, ok := obj.(*v1.Pod); ok && key == podKey {
				stale.Spec.NodeName = ""
			}
			return nil
		},
		SubResourceCreate: func(
			ctx context.Context, c client.Client, subResource string, obj client.Object, body client.Object,
			opts ...client.SubResourceCreateOption,
		) error {
			if subResource != "binding" {
				return c.SubResource(subResource).Create(ctx, obj, body, opts...)
			}
			if !applyAssignment {
				return lostResponseError{}
			}
			binding, ok := body.(*v1.Binding)
			require.True(t, ok)
			latest := &v1.Pod{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(obj), latest); err != nil {
				return err
			}
			latest.Spec.NodeName = binding.Target.Name
			if err := c.Update(ctx, latest); err != nil {
				return err
			}
			return lostResponseError{}
		},
	})

	rrs := lifecycleReservationService(binderClient)
	binderPlugins := plugins.New()
	binderPlugins.RegisterPlugin(gpusharing.New(binderClient, false, false))
	binder := binding.NewBinder(binderClient, rrs, binderPlugins)
	binder.SetPodReader(apiClient)
	reconciler := NewBindRequestReconciler(
		apiClient, scheme, record.NewFakeRecorder(20), &ReconcilerParams{}, binder, rrs,
	)

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)})
	if applyAssignment {
		if err != nil {
			t.Errorf("reconcile returned %v after the pod was assigned", err)
		}
	} else {
		require.ErrorContains(t, err, "connection reset by peer")
	}

	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	return apiClient, pod, node, request
}

func createFractionalBindObjects(t *testing.T, c client.Client) (*v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest) {
	t.Helper()
	ctx := context.Background()
	ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fraction-" + t.Name()}}
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "node-" + t.Name(),
		Labels: map[string]string{constants.NvidiaGpuMemory: "1000"},
	}}
	group := "group-" + t.Name()
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker",
			Namespace: ns.Name,
			Annotations: map[string]string{
				constants.GpuSharingConfigMapAnnotation: sharedGPUConfigMapPrefix,
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "worker", Image: "test-image"}},
		},
		Status: v1.PodStatus{Phase: v1.PodPending},
	}
	request := &schedulingv1alpha2.BindRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "bind", Namespace: ns.Name},
		Spec: schedulingv1alpha2.BindRequestSpec{
			PodName:              pod.Name,
			SelectedNode:         node.Name,
			SelectedGPUGroups:    []string{group},
			ReceivedResourceType: common.ReceivedTypeFraction,
			ReceivedGPU:          &schedulingv1alpha2.ReceivedGPU{Count: 1, Portion: "0.5"},
		},
	}
	reservation := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gpu-reservation-" + t.Name(),
			Namespace: resourceReservationNameSpace,
			Labels:    map[string]string{constants.GPUGroup: group},
			Annotations: map[string]string{
				"run.ai/reserve_for_gpu_index": "0",
			},
		},
		Spec: v1.PodSpec{
			NodeName:   node.Name,
			Containers: []v1.Container{{Name: "reservation", Image: "test-image"}},
		},
	}
	for _, obj := range []client.Object{ns, node, pod, request, reservation} {
		require.NoError(t, c.Create(ctx, obj))
	}
	return pod, node, request
}

func assertSharingConfigMaps(t *testing.T, ctx context.Context, c client.Client, pod *v1.Pod) {
	t.Helper()
	for _, name := range sharingConfigMapNames(t, pod) {
		configMap := &v1.ConfigMap{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: name}, configMap); err != nil {
			t.Errorf("sharing configmap %s/%s missing after lost binding response: %v", pod.Namespace, name, err)
		}
	}
}

func sharingConfigMapNames(t *testing.T, pod *v1.Pod) []string {
	t.Helper()
	containerRef, err := resources.GetFractionContainerRef(pod)
	require.NoError(t, err)
	capabilities, err := gpusharingconfigmap.ExtractCapabilitiesConfigMapName(pod, containerRef)
	require.NoError(t, err)
	envVars, err := gpusharingconfigmap.ExtractDirectEnvVarsConfigMapName(pod, containerRef)
	require.NoError(t, err)
	return []string{capabilities, envVars}
}

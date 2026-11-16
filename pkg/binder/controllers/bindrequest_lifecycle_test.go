// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestBinderReservationLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	require.NoError(t, schedulingv1alpha2.AddToScheme(scheme))
	ctx := context.Background()

	for _, testCase := range []struct {
		name         string
		hideWorkload bool
	}{
		{name: "visible", hideWorkload: false},
		{name: "cache-lag", hideWorkload: true},
	} {
		t.Run("retry must retain reservation/"+testCase.name, func(t *testing.T) {
			apiClient := fake.NewClientBuilder().WithScheme(scheme).
				WithIndex(&v1.Pod{}, "spec.nodeName", nodeNameIndexer).
				WithIndex(&v1.Pod{}, "metadata.name", func(obj client.Object) []string { return []string{obj.GetName()} }).
				WithStatusSubresource(&schedulingv1alpha2.BindRequest{}, &v1.Pod{}).Build()
			pod, node, request, reservation := createLifecycleObjects(t, apiClient, "retry-"+testCase.name, ptr.To(int32(2)))
			hideWorkload := false
			workloadDeleteCalls := 0
			reservationClient := interceptor.NewClient(apiClient, interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if err := c.List(ctx, list, opts...); err != nil {
						return err
					}
					if pods, ok := list.(*v1.PodList); ok && hideWorkload {
						visible := pods.Items[:0]
						for _, item := range pods.Items {
							if client.ObjectKeyFromObject(&item) != client.ObjectKeyFromObject(pod) {
								visible = append(visible, item)
							}
						}
						pods.Items = visible
					}
					return nil
				},
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if _, ok := obj.(*v1.Pod); ok && client.ObjectKeyFromObject(obj) == client.ObjectKeyFromObject(pod) {
						workloadDeleteCalls++
					}
					return c.Delete(ctx, obj, opts...)
				},
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if p, ok := obj.(*v1.Pod); ok && p.Namespace == resourceReservationNameSpace {
						p.Annotations = map[string]string{"run.ai/reserve_for_gpu_index": "GPU-test"}
					}
					return c.Create(ctx, obj, opts...)
				},
				Watch: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) (watch.Interface, error) {
					pods := &v1.PodList{}
					if err := c.List(ctx, pods, opts...); err != nil {
						return nil, err
					}
					w := watch.NewRaceFreeFake()
					for i := range pods.Items {
						w.Add(pods.Items[i].DeepCopy())
					}
					return w, nil
				},
			})
			rrs := lifecycleReservationService(reservationClient)
			failAnnotationPatch := true
			binderClient := interceptor.NewClient(apiClient, interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					data, err := patch.Data(obj)
					if err != nil {
						return err
					}
					if failAnnotationPatch && client.ObjectKeyFromObject(obj) == client.ObjectKeyFromObject(pod) && strings.Contains(string(data), constants.ReceivedResourceType) {
						failAnnotationPatch = false
						return errors.New("injected admission webhook timeout")
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
				// The fake client lacks the binding subresource; preserve the selected-node effect.
				SubResourceCreate: func(ctx context.Context, c client.Client, subResource string, obj client.Object, body client.Object, opts ...client.SubResourceCreateOption) error {
					if subResource != "binding" {
						return c.SubResource(subResource).Create(ctx, obj, body, opts...)
					}
					bind := body.(*v1.Binding)
					require.Equal(t, client.ObjectKeyFromObject(pod), client.ObjectKeyFromObject(obj))
					require.Equal(t, client.ObjectKeyFromObject(pod), client.ObjectKeyFromObject(bind))
					require.Equal(t, "Node", bind.Target.Kind)
					require.Equal(t, node.Name, bind.Target.Name)
					boundPod := &v1.Pod{}
					if err := c.Get(ctx, client.ObjectKeyFromObject(obj), boundPod); err != nil {
						return err
					}
					boundPod.Spec.NodeName = bind.Target.Name
					return c.Update(ctx, boundPod)
				},
			})
			binderPlugins := plugins.New()
			preBindCalls := 0
			binderPlugins.RegisterPlugin(&lifecyclePlugin{preBind: func() error {
				preBindCalls++
				if preBindCalls != 2 {
					return nil
				}
				liveRequest := &schedulingv1alpha2.BindRequest{}
				require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(request), liveRequest))
				require.Equal(t, schedulingv1alpha2.BindRequestPhaseFailed, liveRequest.Status.Phase)
				require.EqualValues(t, 1, liveRequest.Status.FailedAttempts)
				hideWorkload = testCase.hideWorkload
				err := rrs.SyncForGpuGroup(ctx, reservation.Labels[constants.GPUGroup])
				hideWorkload = false
				return err
			}})
			binder := binding.NewBinder(binderClient, rrs, binderPlugins)
			reconciler := NewBindRequestReconciler(apiClient, scheme, record.NewFakeRecorder(20), &ReconcilerParams{}, binder, rrs)
			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)}
			result, err := reconciler.Reconcile(ctx, req)
			require.ErrorContains(t, err, "injected admission webhook timeout")
			require.Positive(t, result.RequeueAfter)
			require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			require.Empty(t, pod.Labels[constants.GPUGroup], "rollback must remove the group label")
			_, err = reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
			require.NoError(t, apiClient.Get(ctx, req.NamespacedName, request))
			require.Equal(t, schedulingv1alpha2.BindRequestPhaseSucceeded, request.Status.Phase)
			require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			require.Equal(t, node.Name, pod.Spec.NodeName)
			require.Equal(t, 2, preBindCalls)
			reservations := &v1.PodList{}
			require.NoError(t, apiClient.List(ctx, reservations, client.InNamespace(resourceReservationNameSpace), client.MatchingLabels{constants.GPUGroup: reservation.Labels[constants.GPUGroup]}))
			if len(reservations.Items) != 1 {
				t.Errorf("successful retry has %d reservation pods, want 1", len(reservations.Items))
			} else {
				require.Equal(t, reservation.Name, reservations.Items[0].Name)
			}
			pod.Status.Phase = v1.PodRunning
			require.NoError(t, apiClient.Status().Update(ctx, pod))
			require.NoError(t, rrs.SyncForNode(ctx, node.Name))
			remainingPod := &v1.Pod{}
			if err := apiClient.Get(ctx, client.ObjectKeyFromObject(pod), remainingPod); err != nil {
				t.Errorf("later node sync deleted the successfully bound Running workload: %v", err)
			} else if remainingPod.DeletionTimestamp != nil {
				t.Errorf("later node sync started deleting the successfully bound Running workload at %s", remainingPod.DeletionTimestamp)
			}
			require.Zero(t, workloadDeleteCalls, "reservation sync must not attempt to delete the bound workload")
		})
	}
}

func createLifecycleObjects(t *testing.T, c client.Client, name string, backoff *int32) (*v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest, *v1.Pod) {
	t.Helper()
	ctx := context.Background()
	ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "lifecycle-" + name}}
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: ns.Name}}
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: ns.Name},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "worker", Image: "test-image"}}}, Status: v1.PodStatus{Phase: v1.PodPending}}
	group := "group-" + name
	request := &schedulingv1alpha2.BindRequest{ObjectMeta: metav1.ObjectMeta{Name: "bind", Namespace: ns.Name}, Spec: schedulingv1alpha2.BindRequestSpec{
		PodName: pod.Name, SelectedNode: node.Name, SelectedGPUGroups: []string{group}, BackoffLimit: backoff,
		ReceivedResourceType: common.ReceivedTypeFraction, ReceivedGPU: &schedulingv1alpha2.ReceivedGPU{Count: 1, Portion: "0.5"},
	}}
	reservation := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "gpu-reservation-" + name, Namespace: resourceReservationNameSpace,
		Labels: map[string]string{constants.GPUGroup: group}, Annotations: map[string]string{"run.ai/reserve_for_gpu_index": "GPU-test"}},
		Spec: v1.PodSpec{NodeName: node.Name, Containers: []v1.Container{{Name: "reservation", Image: "test-image"}}}}
	for _, obj := range []client.Object{ns, node, pod, request, reservation} {
		require.NoError(t, c.Create(ctx, obj))
	}
	return pod, node, request, reservation
}

func lifecycleReservationService(c client.WithWatch) resourcereservation.Interface {
	return resourcereservation.NewService(false, c, "test-image", time.Second, resourceReservationNameSpace,
		resourceReservationServiceAccount, resourceReservationAppLabelValue, scalingPodsNamespace, "", nil, nil, nil, false)
}

type lifecyclePlugin struct{ preBind func() error }

func (*lifecyclePlugin) Name() string { return "controlled-reservation-sync" }
func (p *lifecyclePlugin) PreBind(context.Context, *v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest, *state.BindingState) error {
	return p.preBind()
}
func (*lifecyclePlugin) PostBind(context.Context, *v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest, *state.BindingState) {
}
func (*lifecyclePlugin) Rollback(context.Context, *v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest, *state.BindingState) error {
	return nil
}

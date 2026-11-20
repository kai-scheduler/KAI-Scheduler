// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestBinderReservationLifecycle(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "deployments", "kai-scheduler", "crds")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	require.NoError(t, schedulingv1alpha2.AddToScheme(scheme))
	apiClient, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, apiClient.Create(ctx, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: resourceReservationNameSpace}}))

	t.Run("terminal cache miss must not reenter binding", func(t *testing.T) {
		pod, _, request, reservation := createLifecycleObjects(t, apiClient, "cache-miss", nil)
		miss := true
		laggingClient := interceptor.NewClient(apiClient, interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*v1.Pod); ok && key == client.ObjectKeyFromObject(pod) && miss {
					miss = false
					return apierrors.NewNotFound(v1.Resource("pods"), pod.Name)
				}
				return c.Get(ctx, key, obj, opts...)
			},
		})
		rrs := lifecycleReservationService(apiClient)
		bindCalls := 0
		binder := &lifecycleBinder{bind: func() error { bindCalls++; return nil }}
		reconciler := NewBindRequestReconciler(laggingClient, scheme, record.NewFakeRecorder(20), &ReconcilerParams{}, binder, rrs)
		req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)}
		_, err := reconciler.Reconcile(ctx, req)
		require.True(t, apierrors.IsNotFound(err), "first reconcile must hit the controlled cache miss")
		require.NoError(t, apiClient.Get(ctx, req.NamespacedName, request))
		require.Equal(t, schedulingv1alpha2.BindRequestPhaseFailed, request.Status.Phase)
		t.Logf("Pod exists in API; cache miss marked BindRequest %s with no retry budget", request.Status.Phase)
		require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(pod), &v1.Pod{}))
		require.NoError(t, rrs.SyncForGpuGroup(ctx, reservation.Labels[constants.GPUGroup]))
		require.True(t, apierrors.IsNotFound(apiClient.Get(ctx, client.ObjectKeyFromObject(reservation), &v1.Pod{})))
		_, err = reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		require.Zero(t, bindCalls, "terminal Failed request reentered Bind after its reservation was deleted")
	})

	for _, testCase := range []struct {
		name         string
		hideWorkload bool
	}{
		{name: "visible", hideWorkload: false},
		{name: "cache-lag", hideWorkload: true},
	} {
		t.Run("retry must retain reservation/"+testCase.name, func(t *testing.T) {
			pod, node, request, reservation := createLifecycleObjects(t, apiClient, "retry-"+testCase.name, ptr.To(int32(2)))
			hideWorkload := false
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
			t.Log("Retry succeeded through the real binding API after concurrent reservation sync")
			reservations := &v1.PodList{}
			require.NoError(t, apiClient.List(ctx, reservations, client.InNamespace(resourceReservationNameSpace), client.MatchingLabels{constants.GPUGroup: reservation.Labels[constants.GPUGroup]}))
			if len(reservations.Items) != 1 {
				t.Errorf("successful retry has %d reservation pods, want 1", len(reservations.Items))
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
		})
	}

	t.Run("old reconcile must not mark a replacement request succeeded", func(t *testing.T) {
		pod, node, request, _ := createLifecycleObjects(t, apiClient, "replacement", nil)
		oldUID := request.UID
		replacement := request.DeepCopy()
		replacement.ResourceVersion = ""
		replacement.UID = ""
		replacement.Spec.SelectedNode = "replacement-node"
		replacement.Spec.SelectedGPUGroups = []string{"replacement-group"}
		binder := &lifecycleBinder{bind: func() error {
			require.NoError(t, apiClient.Delete(ctx, request))
			require.NoError(t, apiClient.Create(ctx, replacement))
			require.NotEqual(t, oldUID, replacement.UID)
			return apiClient.SubResource("binding").Create(ctx, pod, &v1.Binding{
				ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace, UID: pod.UID},
				Target:     v1.ObjectReference{Kind: "Node", Name: node.Name},
			})
		}}
		reconciler := NewBindRequestReconciler(apiClient, scheme, record.NewFakeRecorder(20), &ReconcilerParams{}, binder, lifecycleReservationService(apiClient))
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)})
		require.NoError(t, err)
		require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(pod), pod))
		require.Equal(t, node.Name, pod.Spec.NodeName)
		require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(replacement), replacement))
		require.Equal(t, "replacement-node", replacement.Spec.SelectedNode)
		require.NotEqual(t, schedulingv1alpha2.BindRequestPhaseSucceeded, replacement.Status.Phase,
			"outcome of old request marked a different UID/node/group decision succeeded")
	})
}

func createLifecycleObjects(t *testing.T, c client.Client, name string, backoff *int32) (*v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest, *v1.Pod) {
	t.Helper()
	ctx := context.Background()
	ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "lifecycle-" + name}}
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: ns.Name}}
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: ns.Name}, Spec: v1.PodSpec{Containers: []v1.Container{{Name: "worker", Image: "test-image"}}}}
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

type lifecycleBinder struct{ bind func() error }

func (b *lifecycleBinder) Bind(context.Context, *v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest) error {
	return b.bind()
}
func (b *lifecycleBinder) Rollback(context.Context, *v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest) error {
	return nil
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

// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding"
	rrmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	pluginmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/api/constants"
	"github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestReconcileRetainedBindingState(t *testing.T) {
	for _, scenario := range []string{
		"bound", "absent", "absent with stale bound cache", "deleted request", "deleted older request",
		"different node", "different GPU group", "different owner", "unreadable bound Pod", "absent Node",
	} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, v1.AddToScheme(scheme))
			require.NoError(t, v1alpha2.AddToScheme(scheme))
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Namespace: "workloads", Name: "pod", UID: "pod-uid",
				Labels: map[string]string{constants.GPUGroup: "selected-group"},
			}}
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "selected-node"}}
			request := &v1alpha2.BindRequest{
				ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: "request", UID: "request-uid"},
				Spec: v1alpha2.BindRequestSpec{
					PodName: pod.Name, SelectedNode: node.Name, BackoffLimit: ptr.To(int32(2)),
					ReceivedResourceType: common.ReceivedTypeFraction,
					SelectedGPUGroups:    []string{"selected-group"},
					ReceivedGPU:          &v1alpha2.ReceivedGPU{Count: 1, Portion: "0.5"},
				},
				Status: v1alpha2.BindRequestStatus{Phase: v1alpha2.BindRequestPhasePending},
			}
			backing := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, node, request).
				WithStatusSubresource(&v1alpha2.BindRequest{}, &v1.Pod{}).Build()
			bindingIssued := false
			readsAvailable := false
			kubeClient := interceptor.NewClient(backing, interceptor.Funcs{
				SubResourceCreate: func(context.Context, client.Client, string, client.Object, client.Object, ...client.SubResourceCreateOption) error {
					bindingIssued = true
					return io.ErrUnexpectedEOF
				},
			})
			apiReader := interceptor.NewClient(backing, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if bindingIssued && !readsAvailable {
						return io.ErrUnexpectedEOF
					}
					return c.Get(ctx, key, obj, opts...)
				},
			})
			mockController := gomock.NewController(t)
			rrs := rrmock.NewMockInterface(mockController)
			rrs.EXPECT().SyncForNode(gomock.Any(), node.Name).Return(nil).Times(1)
			rrs.EXPECT().ReserveGpuDevice(gomock.Any(), gomock.Any(), node.Name, gomock.Any()).Return("GPU-0", nil).Times(1)
			retained := &retainedStatePlugin{MockPlugin: pluginmock.NewMockPlugin(mockController)}
			binderPlugins := plugins.New()
			binderPlugins.RegisterPlugin(retained)
			// Ordinary plugins need no new interface and receive no synthetic callbacks.
			ordinaryPlugin := pluginmock.NewMockPlugin(mockController)
			ordinaryPlugin.EXPECT().PreBind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			binderPlugins.RegisterPlugin(ordinaryPlugin)
			binder := binding.NewBinder(kubeClient, apiReader, rrs, binderPlugins)
			reconciler := NewBindRequestReconciler(backing, scheme, record.NewFakeRecorder(10), &ReconcilerParams{}, binder, rrs)
			reconciler.apiReader = apiReader
			key := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)}
			_, err := reconciler.Reconcile(ctx, key)
			require.ErrorIs(t, err, binding.ErrBindingOutcomeUnknown)
			require.True(t, retained.retained)
			require.Zero(t, retained.forgetCalls)
			readsAvailable = true
			require.NoError(t, backing.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			require.NoError(t, backing.Get(ctx, key.NamespacedName, request))

			switch scenario {
			case "absent", "absent with stale bound cache":
				require.NoError(t, backing.Delete(ctx, pod))
				if scenario == "absent with stale bound cache" {
					cachedPod := pod.DeepCopy()
					cachedPod.Spec.NodeName = node.Name
					reconciler.Client = interceptor.NewClient(backing, interceptor.Funcs{
						Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if p, ok := obj.(*v1.Pod); ok {
								cachedPod.DeepCopyInto(p)
								return nil
							}
							return c.Get(ctx, key, obj, opts...)
						},
					})
				}
			case "deleted request", "deleted older request":
				rrs.EXPECT().SyncForGpuGroup(gomock.Any(), "selected-group").Return(nil).Times(1)
				deletedRequest := request.DeepCopy()
				deletedRequest.Status.Phase = v1alpha2.BindRequestPhaseSucceeded
				if scenario == "deleted older request" {
					deletedRequest.UID = "old-request-uid"
				}
				reconciler.deleteHandler(ctx, event.DeleteEvent{Object: deletedRequest}, nil)
			case "absent Node":
				require.NoError(t, backing.Delete(ctx, node))
			default:
				pod.Spec.NodeName = node.Name
				switch scenario {
				case "different node":
					pod.Spec.NodeName = "other-node"
				case "different GPU group":
					pod.Labels[constants.GPUGroup] = "other-group"
				case "different owner":
					request.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: "other-pod-uid"}}
					require.NoError(t, backing.Update(ctx, request))
				case "unreadable bound Pod":
					readsAvailable = false
				}
				require.NoError(t, backing.Update(ctx, pod))
			}
			if scenario != "deleted request" && scenario != "deleted older request" {
				_, err = reconciler.Reconcile(ctx, key)
				if scenario == "bound" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
			wantForgotten := scenario == "bound" || scenario == "absent" || scenario == "absent with stale bound cache" || scenario == "deleted request"
			require.Equal(t, !wantForgotten, retained.retained)
			if wantForgotten || scenario == "deleted older request" {
				require.Equal(t, 1, retained.forgetCalls)
			} else {
				require.Zero(t, retained.forgetCalls)
			}
		})
	}
}

type retainedStatePlugin struct {
	*pluginmock.MockPlugin
	requestUID, podUID types.UID
	retained           bool
	forgetCalls        int
}

func (p *retainedStatePlugin) PreBind(_ context.Context, pod *v1.Pod, _ *v1.Node, request *v1alpha2.BindRequest, _ *state.BindingState) error {
	p.requestUID, p.podUID, p.retained = request.UID, pod.UID, true
	return nil
}

func (p *retainedStatePlugin) ForgetBindingState(requestUID, podUID types.UID) {
	p.forgetCalls++
	if requestUID == p.requestUID && (podUID == "" || podUID == p.podUID) {
		p.retained = false
	}
}

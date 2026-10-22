// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	rrmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	mockplugins "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestBindConfirmsSuccessfulBindingAfterResponseError(t *testing.T) {
	preBindErr := errors.New("prebind rejected")
	for _, tt := range []struct {
		name             string
		response         error
		observedUID      types.UID
		observedNode     string
		readErr          error
		preBindErr       error
		cancelContext    bool
		missingUID       bool
		missingNode      bool
		shortDeadline    bool
		withoutAPIReader bool
		wantSuccess      bool
		wantReads        int
	}{
		{name: "normal success needs no confirmation", wantSuccess: true},
		{name: "same Pod bound to selected node", response: io.ErrUnexpectedEOF, observedUID: "pod-uid", observedNode: "selected-node", wantSuccess: true, wantReads: 1},
		{name: "replacement Pod is not confirmation", response: io.ErrUnexpectedEOF, observedUID: "replacement-uid", observedNode: "selected-node", wantReads: 1},
		{name: "different node is not confirmation", response: io.ErrUnexpectedEOF, observedUID: "pod-uid", observedNode: "other-node", wantReads: 1},
		{name: "unbound Pod is not confirmation", response: io.ErrUnexpectedEOF, observedUID: "pod-uid", wantReads: 1},
		{name: "read failure preserves binding error", response: io.ErrUnexpectedEOF, readErr: errors.New("API unavailable"), wantReads: 1},
		{name: "canceled context preserves binding error", response: io.ErrUnexpectedEOF, cancelContext: true, wantReads: 1},
		{name: "missing UID cannot confirm identity", response: io.ErrUnexpectedEOF, observedNode: "selected-node", missingUID: true},
		{name: "missing target node cannot confirm binding", response: io.ErrUnexpectedEOF, observedUID: "pod-uid", missingNode: true},
		{name: "parent deadline bounds confirmation", response: io.ErrUnexpectedEOF, observedUID: "pod-uid", observedNode: "selected-node", shortDeadline: true, wantSuccess: true, wantReads: 1},
		{name: "PreBind failure needs no confirmation", preBindErr: preBindErr},
		{name: "nil API reader preserves binding error", response: io.ErrUnexpectedEOF, observedUID: "pod-uid", observedNode: "selected-node", withoutAPIReader: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.shortDeadline {
				var cancelDeadline context.CancelFunc
				ctx, cancelDeadline = context.WithTimeout(ctx, time.Second)
				defer cancelDeadline()
			}
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "workloads", Name: "pod", UID: "pod-uid"}}
			if tt.missingUID {
				pod.UID = ""
			}
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "selected-node"}}
			if tt.missingNode {
				node.Name = ""
			}
			br := &v1alpha2.BindRequest{Spec: v1alpha2.BindRequestSpec{
				PodName: pod.Name, SelectedNode: node.Name, ReceivedResourceType: common.ReceivedTypeRegular,
			}}
			bindCalls := 0
			kubeClient := fake.NewClientBuilder().WithObjects(pod).WithInterceptorFuncs(interceptor.Funcs{
				SubResourceCreate: func(_ context.Context, c client.Client, subresource string,
					obj, body client.Object, _ ...client.SubResourceCreateOption) error {
					require.Equal(t, "binding", subresource)
					bindCalls++
					if tt.cancelContext {
						cancel()
					}
					return tt.response
				},
			}).Build()
			reads := 0
			var apiReader client.Reader = interceptor.NewClient(kubeClient, interceptor.Funcs{
				Get: func(ctx context.Context, _ client.WithWatch, key client.ObjectKey, obj client.Object,
					_ ...client.GetOption) error {
					reads++
					require.Equal(t, client.ObjectKeyFromObject(pod), key)
					if err := ctx.Err(); err != nil {
						return err
					}
					deadline, ok := ctx.Deadline()
					require.True(t, ok, "confirmation must have a deadline")
					maxDelay := 5 * time.Second
					if tt.shortDeadline {
						maxDelay = time.Second
					}
					assert.LessOrEqual(t, time.Until(deadline), maxDelay)
					if tt.readErr != nil {
						return tt.readErr
					}
					// The ordinary client still holds an unbound Pod. Confirmation must
					// use the separate authoritative reader.
					actualPod := &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name, UID: tt.observedUID},
						Spec:       v1.PodSpec{NodeName: tt.observedNode},
					}
					actualPod.DeepCopyInto(obj.(*v1.Pod))
					return nil
				},
			})
			mockController := gomock.NewController(t)
			rrs := rrmock.NewMockInterface(mockController)
			rrs.EXPECT().SyncForNode(gomock.Any(), node.Name).Return(nil)
			plugin := mockplugins.NewMockPlugin(mockController)
			plugin.EXPECT().Name().Return("observer").AnyTimes()
			var preparedState *state.BindingState
			plugin.EXPECT().PreBind(ctx, pod, node, br, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ *v1.Pod, _ *v1.Node, _ *v1alpha2.BindRequest, bindingState *state.BindingState) error {
					preparedState = bindingState
					bindingState.BindingPodAnnotations["test.example/prepared"] = "true"
					return tt.preBindErr
				})
			if tt.wantSuccess {
				plugin.EXPECT().PostBind(ctx, pod, node, br, gomock.Any()).Times(1).Do(
					func(_ context.Context, _ *v1.Pod, _ *v1.Node, _ *v1alpha2.BindRequest, bindingState *state.BindingState) {
						require.Same(t, preparedState, bindingState)
						assert.Equal(t, "true", bindingState.BindingPodAnnotations["test.example/prepared"])
					})
			}
			binderPlugins := plugins.New()
			binderPlugins.RegisterPlugin(plugin)
			if tt.withoutAPIReader {
				apiReader = nil
			}
			binder := NewBinder(kubeClient, apiReader, rrs, binderPlugins)

			err := binder.Bind(ctx, pod, node, br)
			assert.Equal(t, tt.wantReads, reads)
			if tt.preBindErr != nil {
				assert.Zero(t, bindCalls)
				assert.ErrorContains(t, err, tt.preBindErr.Error())
				return
			}
			assert.Equal(t, 1, bindCalls)
			if tt.wantSuccess {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.response)
				assert.ErrorContains(t, err, "failed to bind pod <workloads/pod> to node <"+node.Name+">")
			}
		})
	}
}

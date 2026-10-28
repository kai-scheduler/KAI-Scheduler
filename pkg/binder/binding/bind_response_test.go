// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"bytes"
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
	for _, tt := range []struct {
		name             string
		response         error
		observedUID      types.UID
		observedNode     string
		readErr          error
		changeVersion    bool
		loseFenceReply   bool
		cancelContext    bool
		withoutAPIReader bool
		wantSuccess      bool
		wantUnknown      bool
	}{
		{name: "normal success", wantSuccess: true},
		{name: "same Pod bound to selected node", response: io.ErrUnexpectedEOF, observedNode: "selected-node", wantSuccess: true},
		{name: "replacement Pod", response: io.ErrUnexpectedEOF, observedUID: "replacement-uid", observedNode: "selected-node", wantUnknown: true},
		{name: "different node", response: io.ErrUnexpectedEOF, observedNode: "other-node", wantUnknown: true},
		{name: "unbound Pod requires exclusion write", response: io.ErrUnexpectedEOF},
		{name: "unrelated version change excludes binding", response: io.ErrUnexpectedEOF, changeVersion: true},
		{name: "lost exclusion write response is read back", response: io.ErrUnexpectedEOF, loseFenceReply: true},
		{name: "read failure remains unknown", response: io.ErrUnexpectedEOF, readErr: errors.New("API unavailable"), wantUnknown: true},
		{name: "canceled context remains unknown", response: io.ErrUnexpectedEOF, cancelContext: true, wantUnknown: true},
		{name: "nil API reader preserves rejection behavior", response: io.ErrUnexpectedEOF, withoutAPIReader: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "workloads", Name: "pod", UID: "pod-uid"}}
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "selected-node"}}
			br := &v1alpha2.BindRequest{Spec: v1alpha2.BindRequestSpec{
				PodName: pod.Name, SelectedNode: node.Name, ReceivedResourceType: common.ReceivedTypeRegular,
			}}
			backing := fake.NewClientBuilder().WithObjects(pod).Build()
			bindCalls, fenceWrites := 0, 0
			var bindingVersion string
			kubeClient := interceptor.NewClient(backing, interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					data, err := patch.Data(obj)
					require.NoError(t, err)
					isFence := bytes.Contains(data, []byte(bindingAttemptAnnotation))
					if isFence {
						fenceWrites++
					}
					if err := c.Patch(ctx, obj, patch, opts...); err != nil {
						return err
					}
					if isFence && fenceWrites == 2 && tt.loseFenceReply {
						return io.ErrUnexpectedEOF
					}
					return nil
				},
				SubResourceCreate: func(ctx context.Context, c client.Client, subresource string,
					obj, body client.Object, _ ...client.SubResourceCreateOption) error {
					require.Equal(t, "binding", subresource)
					bindCalls++
					request := body.(*v1.Binding)
					require.Equal(t, types.UID("pod-uid"), request.UID)
					bindingVersion = request.ResourceVersion
					if !tt.withoutAPIReader {
						require.NotEmpty(t, bindingVersion)
						require.Equal(t, pod.ResourceVersion, bindingVersion)
					}
					if tt.observedNode != "" || tt.observedUID != "" || tt.changeVersion {
						stored := &v1.Pod{}
						require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), stored))
						stored.Spec.NodeName = tt.observedNode
						if tt.observedUID != "" {
							stored.UID = tt.observedUID
						}
						stored.Labels = map[string]string{"unrelated": "changed"}
						require.NoError(t, c.Update(ctx, stored))
					}
					if tt.cancelContext {
						cancel()
					}
					return tt.response
				},
			})
			var apiReader client.Reader = interceptor.NewClient(backing, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					assert.LessOrEqual(t, time.Until(deadline), time.Second)
					if err := ctx.Err(); err != nil {
						return err
					}
					if bindCalls > 0 && tt.readErr != nil {
						return tt.readErr
					}
					return c.Get(ctx, key, obj, opts...)
				},
			})
			controller := gomock.NewController(t)
			rrs := rrmock.NewMockInterface(controller)
			rrs.EXPECT().SyncForNode(gomock.Any(), node.Name).DoAndReturn(func(context.Context, string) error {
				if !tt.withoutAPIReader {
					require.Equal(t, 1, fenceWrites, "the entry write must precede reservation work")
				}
				return nil
			})
			plugin := mockplugins.NewMockPlugin(controller)
			plugin.EXPECT().Name().Return("observer").AnyTimes()
			var preparedState *state.BindingState
			plugin.EXPECT().PreBind(ctx, pod, node, br, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ *v1.Pod, _ *v1.Node, _ *v1alpha2.BindRequest, bindingState *state.BindingState) error {
					preparedState = bindingState
					bindingState.BindingPodAnnotations["test.example/prepared"] = "true"
					return nil
				})
			if tt.wantSuccess {
				plugin.EXPECT().PostBind(ctx, pod, node, br, gomock.Any()).Do(
					func(_ context.Context, _ *v1.Pod, _ *v1.Node, _ *v1alpha2.BindRequest, bindingState *state.BindingState) {
						require.Same(t, preparedState, bindingState)
					})
			}
			binderPlugins := plugins.New()
			binderPlugins.RegisterPlugin(plugin)
			if tt.withoutAPIReader {
				apiReader = nil
			}
			binder := NewBinder(kubeClient, apiReader, rrs, binderPlugins)
			err := binder.Bind(ctx, pod, node, br)
			require.Equal(t, 1, bindCalls)
			assert.Equal(t, tt.wantUnknown, errors.Is(err, ErrBindingOutcomeUnknown))
			if tt.wantSuccess {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.response)
			}
			if tt.response != nil && !tt.wantSuccess && !tt.wantUnknown && !tt.withoutAPIReader {
				stored := &v1.Pod{}
				require.NoError(t, backing.Get(context.Background(), client.ObjectKeyFromObject(pod), stored))
				assert.Empty(t, stored.Spec.NodeName)
				assert.NotEqual(t, bindingVersion, stored.ResourceVersion, "cleanup requires exclusion of the delayed Binding")
			}
		})
	}
}

func TestBindingPreflightPreservesUnknownAttempts(t *testing.T) {
	for _, scenario := range []string{"bound same node", "bound other node", "replaced Pod", "missing UID", "read error", "lost entry write response", "unchanged entry write"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "workloads", Name: "pod", UID: "expected"}}
			observed := pod.DeepCopy()
			switch scenario {
			case "bound same node":
				observed.Spec.NodeName = "node"
			case "bound other node":
				observed.Spec.NodeName = "other-node"
			case "replaced Pod":
				observed.UID = "replacement"
			case "missing UID":
				pod.UID = ""
			}
			backing := fake.NewClientBuilder().WithObjects(observed).Build()
			kubeClient := interceptor.NewClient(backing, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if scenario == "read error" {
						return io.ErrUnexpectedEOF
					}
					return c.Get(ctx, key, obj, opts...)
				},
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if scenario == "unchanged entry write" {
						return nil
					}
					require.NoError(t, c.Patch(ctx, obj, patch, opts...))
					return io.ErrUnexpectedEOF
				},
			})
			rrs := rrmock.NewMockInterface(gomock.NewController(t))
			binder := NewBinder(kubeClient, kubeClient, rrs, plugins.New())
			err := binder.Bind(ctx, pod, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}, &v1alpha2.BindRequest{})
			require.ErrorIs(t, err, ErrBindingOutcomeUnknown)
			stored := &v1.Pod{}
			require.NoError(t, backing.Get(ctx, client.ObjectKeyFromObject(observed), stored))
			assert.Equal(t, observed.UID, stored.UID)
			assert.Equal(t, observed.Spec.NodeName, stored.Spec.NodeName)
		})
	}
}

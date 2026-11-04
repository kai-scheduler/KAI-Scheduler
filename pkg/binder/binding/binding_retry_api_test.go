// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	rrmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	mockplugins "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/api/scheduling/v1alpha2"
)

// This exercises the actual API server's Binding/storage preconditions, which
// fake clients do not implement. Both write orderings are deterministic.
func TestBindingRetryProtocolAPIServer(t *testing.T) {
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
	namespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "binding-retry-"}}
	require.NoError(t, apiClient.Create(ctx, namespace))
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "selected-node"}}
	require.NoError(t, apiClient.Create(ctx, node))
	newPod := func(t *testing.T) *v1.Pod {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, GenerateName: "pod-", Annotations: map[string]string{"test.example/unchanged": "value"}},
			Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "worker", Image: "registry.k8s.io/pause:3.10"}}},
		}
		require.NoError(t, apiClient.Create(ctx, pod))
		return pod
	}
	requestFor := func(pod *v1.Pod) *v1.Binding {
		return &v1.Binding{
			ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name, UID: pod.UID, ResourceVersion: pod.ResourceVersion},
			Target:     v1.ObjectReference{Kind: "Node", Name: node.Name},
		}
	}
	readPod := func(t *testing.T, pod *v1.Pod) *v1.Pod {
		actual := &v1.Pod{}
		require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(pod), actual))
		return actual
	}
	protocol := NewBinder(apiClient, apiClient, nil, plugins.New())

	t.Run("changing Pod write excludes delayed Binding", func(t *testing.T) {
		pod := newPod(t)
		delayed := requestFor(pod)
		require.NoError(t, protocol.advanceBindingAttempt(ctx, pod))
		require.NotEqual(t, delayed.ResourceVersion, pod.ResourceVersion)
		err := apiClient.SubResource("binding").Create(ctx, pod, delayed)
		require.True(t, apierrors.IsConflict(err), "%v", err)
		assert.Empty(t, readPod(t, pod).Spec.NodeName)
	})

	t.Run("committed Binding excludes stale conditional Pod write", func(t *testing.T) {
		pod := newPod(t)
		delayed := requestFor(pod)
		require.NoError(t, apiClient.SubResource("binding").Create(ctx, pod, delayed))
		err := protocol.advanceBindingAttempt(ctx, pod)
		require.True(t, apierrors.IsConflict(err), "%v", err)
		assert.Equal(t, node.Name, readPod(t, pod).Spec.NodeName)
	})

	t.Run("no-op write does not exclude delayed Binding", func(t *testing.T) {
		pod := newPod(t)
		delayed := requestFor(pod)
		data, err := json.Marshal(map[string]interface{}{"metadata": map[string]interface{}{
			"resourceVersion": pod.ResourceVersion, "uid": pod.UID,
			"annotations": map[string]string{"test.example/unchanged": "value"},
		}})
		require.NoError(t, err)
		require.NoError(t, apiClient.Patch(ctx, pod, client.RawPatch(types.MergePatchType, data)))
		require.Equal(t, delayed.ResourceVersion, pod.ResourceVersion, "a no-op cannot establish exclusion")
		require.NoError(t, apiClient.SubResource("binding").Create(ctx, pod, delayed))
		assert.Equal(t, node.Name, readPod(t, pod).Spec.NodeName)
	})

	t.Run("unrelated status write also invalidates Binding version", func(t *testing.T) {
		pod := newPod(t)
		delayed := requestFor(pod)
		pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse, Reason: "TestStatusChange"}}
		require.NoError(t, apiClient.Status().Update(ctx, pod))
		require.NotEqual(t, delayed.ResourceVersion, pod.ResourceVersion)
		err := apiClient.SubResource("binding").Create(ctx, pod, delayed)
		require.True(t, apierrors.IsConflict(err), "%v", err)
	})

	t.Run("UID-only old binary request survives version change", func(t *testing.T) {
		pod := newPod(t)
		delayed := requestFor(pod)
		delayed.ResourceVersion = ""
		require.NoError(t, protocol.advanceBindingAttempt(ctx, pod))
		require.NoError(t, apiClient.SubResource("binding").Create(ctx, pod, delayed))
		assert.Equal(t, node.Name, readPod(t, pod).Spec.NodeName, "old binaries require a quiesced rollout")
	})

	for _, scenario := range []string{"changing write wins", "binding wins before exclusion write", "exclusion write response lost", "exclusion write rejected", "both exclusion observations fail", "replacement Pod returned during PreBind"} {
		t.Run(scenario, func(t *testing.T) {
			pod := newPod(t)
			originalUID := pod.UID
			br := &v1alpha2.BindRequest{Spec: v1alpha2.BindRequestSpec{
				PodName: pod.Name, SelectedNode: node.Name, ReceivedResourceType: common.ReceivedTypeRegular,
			}}
			var delayed *v1.Binding
			fenceWrites := 0
			writer := interceptor.NewClient(apiClient, interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					data, err := patch.Data(obj)
					require.NoError(t, err)
					isFence := bytes.Contains(data, []byte(bindingAttemptAnnotation))
					if isFence {
						fenceWrites++
					}
					if isFence && fenceWrites == 2 && scenario == "binding wins before exclusion write" {
						require.NotNil(t, delayed)
						require.NoError(t, c.SubResource("binding").Create(ctx, obj, delayed))
					}
					if isFence && fenceWrites == 2 && (scenario == "both exclusion observations fail" || scenario == "exclusion write rejected") {
						return io.ErrUnexpectedEOF
					}
					if err := c.Patch(ctx, obj, patch, opts...); err != nil {
						return err
					}
					if isFence && fenceWrites == 2 && scenario == "exclusion write response lost" {
						return io.ErrUnexpectedEOF
					}
					return nil
				},
				SubResourceCreate: func(_ context.Context, _ client.Client, subresource string, obj, body client.Object, _ ...client.SubResourceCreateOption) error {
					require.Equal(t, "binding", subresource)
					delayed = body.(*v1.Binding).DeepCopy()
					require.Equal(t, originalUID, delayed.UID)
					require.NotEmpty(t, delayed.ResourceVersion)
					return io.ErrUnexpectedEOF
				},
			})
			reader := interceptor.NewClient(apiClient, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if scenario == "both exclusion observations fail" && fenceWrites == 2 {
						return io.ErrUnexpectedEOF
					}
					return c.Get(ctx, key, obj, opts...)
				},
			})
			controller := gomock.NewController(t)
			rrs := rrmock.NewMockInterface(controller)
			rrs.EXPECT().SyncForNode(gomock.Any(), node.Name).Return(nil)
			plugin := mockplugins.NewMockPlugin(controller)
			plugin.EXPECT().Name().Return("observer").AnyTimes()
			plugin.EXPECT().PreBind(gomock.Any(), pod, node, br, gomock.Any()).DoAndReturn(
				func(ctx context.Context, pod *v1.Pod, _ *v1.Node, _ *v1alpha2.BindRequest, _ *state.BindingState) error {
					if scenario == "replacement Pod returned during PreBind" {
						pod.UID = "replacement-uid"
					}
					return nil
				})
			if scenario == "binding wins before exclusion write" {
				plugin.EXPECT().PostBind(gomock.Any(), pod, node, br, gomock.Any())
			}
			binderPlugins := plugins.New()
			binderPlugins.RegisterPlugin(plugin)
			binder := NewBinder(writer, reader, rrs, binderPlugins)
			err := binder.Bind(ctx, pod, node, br)
			switch scenario {
			case "binding wins before exclusion write":
				require.NoError(t, err)
				assert.Equal(t, node.Name, readPod(t, pod).Spec.NodeName)
			case "both exclusion observations fail", "exclusion write rejected":
				require.ErrorIs(t, err, ErrBindingOutcomeUnknown)
				// The delayed request remains capable of committing. The controller
				// therefore must not publish failure or invoke rollback.
				require.NoError(t, apiClient.SubResource("binding").Create(ctx, pod, delayed))
			case "replacement Pod returned during PreBind":
				require.ErrorIs(t, err, ErrBindingOutcomeUnknown)
				assert.Nil(t, delayed, "must never bind using a newly adopted UID")
				assert.Equal(t, originalUID, readPod(t, pod).UID)
			default:
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
				require.False(t, errors.Is(err, ErrBindingOutcomeUnknown))
				err := apiClient.SubResource("binding").Create(ctx, pod, delayed)
				require.True(t, apierrors.IsConflict(err), "%v", err)
				assert.Empty(t, readPod(t, pod).Spec.NodeName)
			}
		})
	}

	t.Run("restart excludes prior request before PreBind failure", func(t *testing.T) {
		pod := newPod(t)
		delayed := requestFor(pod)
		br := &v1alpha2.BindRequest{Spec: v1alpha2.BindRequestSpec{
			PodName: pod.Name, SelectedNode: node.Name, ReceivedResourceType: common.ReceivedTypeRegular,
		}}
		controller := gomock.NewController(t)
		rrs := rrmock.NewMockInterface(controller)
		rrs.EXPECT().SyncForNode(gomock.Any(), node.Name).DoAndReturn(func(context.Context, string) error {
			// This runs before PreBind and represents the old process's delayed
			// request finally reaching the real API server after restart.
			err := apiClient.SubResource("binding").Create(ctx, pod, delayed)
			require.True(t, apierrors.IsConflict(err), "%v", err)
			return nil
		})
		plugin := mockplugins.NewMockPlugin(controller)
		plugin.EXPECT().Name().Return("observer").AnyTimes()
		preBindErr := errors.New("PreBind failed after restart")
		plugin.EXPECT().PreBind(gomock.Any(), pod, node, br, gomock.Any()).Return(preBindErr)
		binderPlugins := plugins.New()
		binderPlugins.RegisterPlugin(plugin)
		binder := NewBinder(apiClient, apiClient, rrs, binderPlugins)
		err := binder.Bind(ctx, pod, node, br)
		require.ErrorContains(t, err, preBindErr.Error())
		assert.False(t, errors.Is(err, ErrBindingOutcomeUnknown))
		assert.Empty(t, readPod(t, pod).Spec.NodeName)
	})

	t.Run("PreBind status update through separate Pod preserves successful binding", func(t *testing.T) {
		pod := newPod(t)
		br := &v1alpha2.BindRequest{Spec: v1alpha2.BindRequestSpec{
			PodName: pod.Name, SelectedNode: node.Name, ReceivedResourceType: common.ReceivedTypeRegular,
		}}
		controller := gomock.NewController(t)
		rrs := rrmock.NewMockInterface(controller)
		rrs.EXPECT().SyncForNode(gomock.Any(), node.Name).Return(nil)
		plugin := mockplugins.NewMockPlugin(controller)
		plugin.EXPECT().Name().Return("status-writer").AnyTimes()
		plugin.EXPECT().PreBind(gomock.Any(), pod, node, br, gomock.Any()).DoAndReturn(
			func(ctx context.Context, input *v1.Pod, _ *v1.Node, _ *v1alpha2.BindRequest, bindingState *state.BindingState) error {
				fresh := readPod(t, input)
				fresh.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse, Reason: "PluginStatusUpdate"}}
				require.NoError(t, apiClient.Status().Update(ctx, fresh))
				require.NotEqual(t, input.ResourceVersion, fresh.ResourceVersion)
				bindingState.BindingPodAnnotations["test.example/prepared"] = "true"
				return nil
			})
		plugin.EXPECT().PostBind(gomock.Any(), pod, node, br, gomock.Any())
		binderPlugins := plugins.New()
		binderPlugins.RegisterPlugin(plugin)
		binder := NewBinder(apiClient, apiClient, rrs, binderPlugins)
		require.NoError(t, binder.Bind(ctx, pod, node, br))
		actual := readPod(t, pod)
		assert.Equal(t, node.Name, actual.Spec.NodeName)
		assert.Equal(t, "true", actual.Annotations["test.example/prepared"])
		assert.Equal(t, "PluginStatusUpdate", actual.Status.Conditions[0].Reason)
	})
}

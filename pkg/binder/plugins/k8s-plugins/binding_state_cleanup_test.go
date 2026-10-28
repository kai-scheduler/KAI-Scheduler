// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_plugins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	pluginmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/k8s-plugins/common/mock"
	"github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestForgetBindingState(t *testing.T) {
	for _, tt := range []struct {
		name         string
		requestUID   types.UID
		podUID       types.UID
		newerRequest bool
		mutateInputs bool
		wantRetained bool
	}{
		{name: "bound Pod", requestUID: "request", podUID: "pod"},
		{name: "absent Pod or deleted request", requestUID: "request"},
		{name: "other Pod", requestUID: "request", podUID: "other-pod", wantRetained: true},
		{name: "other request", requestUID: "old-request", podUID: "pod", wantRetained: true},
		{name: "empty request identity", podUID: "pod", wantRetained: true},
		{name: "newer request on same Pod", requestUID: "request", podUID: "pod", newerRequest: true, wantRetained: true},
		{name: "old request deleted after replacement", requestUID: "request", newerRequest: true, wantRetained: true},
		{name: "captured identities survive input mutation", requestUID: "request", podUID: "pod", mutateInputs: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			plugin := pluginmock.NewMockK8sPlugin(gomock.NewController(t))
			plugin.EXPECT().Name().Return("retained-plugin").AnyTimes()
			plugin.EXPECT().IsRelevant(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
			plugin.EXPECT().PreFilter(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, false).AnyTimes()
			plugin.EXPECT().Filter(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			plugin.EXPECT().Allocate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			plugin.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			// Neither PostBind nor UnAllocate is permitted by observation-only cleanup.
			k8sPlugins := NewWithPlugins("k8s-plugins", plugin)
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod"}}
			request := &v1alpha2.BindRequest{ObjectMeta: metav1.ObjectMeta{UID: "request"}}
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "selected-node"}}
			require.NoError(t, k8sPlugins.PreBind(ctx, pod, node, request, nil))
			if tt.newerRequest {
				newRequest := &v1alpha2.BindRequest{ObjectMeta: metav1.ObjectMeta{UID: "new-request"}}
				require.NoError(t, k8sPlugins.PreBind(ctx, pod, node, newRequest, nil))
			}
			expected, found := k8sPlugins.states.Load(types.UID("pod"))
			require.True(t, found)
			unrelatedPod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "unrelated-pod"}}
			unrelatedRequest := &v1alpha2.BindRequest{ObjectMeta: metav1.ObjectMeta{UID: "unrelated-request"}}
			require.NoError(t, k8sPlugins.PreBind(ctx, unrelatedPod, node, unrelatedRequest, nil))
			unrelated, found := k8sPlugins.states.Load(unrelatedPod.UID)
			require.True(t, found)
			if tt.mutateInputs {
				pod.UID = "replacement-pod"
				request.UID = "replacement-request"
			}

			k8sPlugins.ForgetBindingState(tt.requestUID, tt.podUID)

			actual, found := k8sPlugins.states.Load(types.UID("pod"))
			require.Equal(t, tt.wantRetained, found)
			if tt.wantRetained {
				require.Same(t, expected, actual)
			}
			actualUnrelated, found := k8sPlugins.states.Load(unrelatedPod.UID)
			require.True(t, found)
			require.Same(t, unrelated, actualUnrelated)
		})
	}
}

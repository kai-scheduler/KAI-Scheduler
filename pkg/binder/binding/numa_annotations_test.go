// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rrmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func TestPredictedMemoryGroupsAnnotation(t *testing.T) {
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", Annotations: map[string]string{constants.NumaMemoryGroupsObserved: "null"}}}
	kubeClient := fake.NewClientBuilder().WithObjects(pod).WithInterceptorFuncs(test_utils.EmptyBind).Build()
	reservationService := rrmock.NewMockInterface(gomock.NewController(t))
	reservationService.EXPECT().SyncForNode(gomock.Any(), "node").Return(nil)
	binder := NewBinder(kubeClient, reservationService, plugins.New())
	groups := []v1alpha2.NUMAMemoryGroupPlacement{{MemoryNodes: []string{"node-0", "node-1"}, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("120Gi")}}}
	request := &v1alpha2.BindRequest{Spec: v1alpha2.BindRequestSpec{PredictedNUMAMemoryGroups: groups, SelectedNode: "node", ReceivedResourceType: common.ReceivedTypeRegular}}
	require.NoError(t, binder.Bind(context.Background(), pod, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}, request))
	stored := &v1.Pod{}
	require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(pod), stored))
	var decoded []v1alpha2.NUMAMemoryGroupPlacement
	require.NoError(t, json.Unmarshal([]byte(stored.Annotations[constants.NumaMemoryGroupsPredicted]), &decoded))
	require.Equal(t, groups, decoded)
	require.Equal(t, "null", stored.Annotations[constants.NumaMemoryGroupsObserved])
}

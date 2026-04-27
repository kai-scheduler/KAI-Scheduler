// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgrouper_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/grouper"
)

// stubHub returns a fixed plugin for any GVK.
type stubHub struct{ plugin grouper.Grouper }

func (s stubHub) GetPodGrouperPlugin(metav1.GroupVersionKind) grouper.Grouper {
	return s.plugin
}

// stubGrouper returns a fixed base Metadata, simulating a top-owner plugin
// that has succeeded and produced a baseline PodGroup spec.
type stubGrouper struct{ base *podgroup.Metadata }

func (stubGrouper) Name() string { return "stub" }
func (s stubGrouper) GetPodGroupMetadata(*unstructured.Unstructured, *corev1.Pod, ...*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	return s.base, nil
}

// TestGetPGMetadata_MissingWorkload_WrapsAsErrDeferred verifies the cross-package
// contract: when the Workload referenced by a Pod doesn't yet exist, GetPGMetadata
// surfaces an error that satisfies errors.Is(err, podgrouper.ErrDeferred). The
// pod_controller relies on this sentinel to skip the reconcile without retrying;
// the recovery path is the Workload watch in workload_watch.go.
func TestGetPGMetadata_MissingWorkload_WrapsAsErrDeferred(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, schedulingv1alpha1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	hub := stubHub{plugin: stubGrouper{base: &podgroup.Metadata{
		Namespace: "ns", Name: "base", MinAvailable: 1,
	}}}
	grouper := podgrouper.NewPodgrouper(fakeClient, fakeClient, hub, true)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "p"},
		Spec: corev1.PodSpec{
			WorkloadRef: &corev1.WorkloadReference{Name: "missing", PodGroup: "g"},
		},
	}
	topOwner := &unstructured.Unstructured{}
	topOwner.SetNamespace("ns")
	topOwner.SetName("p")
	topOwner.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Pod"})

	_, err := grouper.GetPGMetadata(context.Background(), pod, topOwner, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, podgrouper.ErrDeferred),
		"GetPGMetadata error must wrap podgrouper.ErrDeferred for soft failures (got %v)", err)
}

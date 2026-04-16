// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
)

const nodePoolKey = "kai.scheduler/node-pool"

func TestAddNodePoolLabel(t *testing.T) {
	metadata := podgroup.Metadata{
		Annotations:       nil,
		Labels:            nil,
		PriorityClassName: "",
		Queue:             "",
		Namespace:         "",
		Name:              "",
		MinAvailable:      0,
		Owner:             metav1.OwnerReference{},
	}

	pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				nodePoolKey: "my-node-pool",
			},
		},
		Spec:   v1.PodSpec{},
		Status: v1.PodStatus{},
	}

	addNodePoolLabel(&metadata, &pod, nodePoolKey)
	assert.Equal(t, "my-node-pool", metadata.Labels[nodePoolKey])

	metadata.Labels = nil
	pod.Labels = nil

	addNodePoolLabel(&metadata, &pod, nodePoolKey)
	assert.Equal(t, "", metadata.Labels[nodePoolKey])

	metadata.Labels = map[string]string{
		nodePoolKey: "non-default-pool",
	}

	addNodePoolLabel(&metadata, &pod, nodePoolKey)
	assert.Equal(t, "non-default-pool", metadata.Labels[nodePoolKey])
}

func TestIsOrphanPodWithPodGroup(t *testing.T) {
	pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				constants.PodGroupAnnotationForPod: "blabla",
			},
		},
		Spec:   v1.PodSpec{},
		Status: v1.PodStatus{},
	}

	assert.True(t, isOrphanPodWithPodGroup(context.Background(), &pod))

	pod.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "gorilla",
			Name:       "harambe",
			UID:        "1",
		},
	}
	assert.False(t, isOrphanPodWithPodGroup(context.Background(), &pod))
}

func TestEventOnFailure(t *testing.T) {
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "my-namespace",
		},
		Spec: v1.PodSpec{
			SchedulerName: "kai-scheduler",
		},
		Status: v1.PodStatus{},
	}
	fakeClient := fake.NewClientBuilder().WithObjects(&pod).Build()
	fakeEventRecorder := record.NewFakeRecorder(10)

	podReconciler := PodReconciler{
		Client:          fakeClient,
		Scheme:          scheme.Scheme,
		podGrouper:      &fakePodGrouper{},
		PodGroupHandler: nil,
		configs: Configs{
			SchedulerName: "kai-scheduler",
		},
		eventRecorder: fakeEventRecorder,
	}

	podReconciler.Reconcile(context.TODO(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: pod.Namespace,
			Name:      pod.Name,
		},
	})

	if len(fakeEventRecorder.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(fakeEventRecorder.Events))
	}

}

type fakePodGrouper struct{}

func (*fakePodGrouper) GetPGMetadata(ctx context.Context, pod *v1.Pod, topOwner *unstructured.Unstructured, allOwners []*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	return nil, nil
}

func (*fakePodGrouper) GetPodOwners(ctx context.Context, pod *v1.Pod) (*unstructured.Unstructured, []*metav1.PartialObjectMetadata, error) {
	return nil, nil, fmt.Errorf("failed")
}

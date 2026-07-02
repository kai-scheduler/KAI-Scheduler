// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
)

func TestReconcileEmitsWarningEventFromMetadata(t *testing.T) {
	testScheme := runtime.NewScheme()
	assert.NoError(t, clientgoscheme.AddToScheme(testScheme))
	assert.NoError(t, schedulingv2alpha2.AddToScheme(testScheme))

	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "my-namespace"},
		Spec:       v1.PodSpec{SchedulerName: "kai-scheduler"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(&pod).Build()
	fakeRecorder := record.NewFakeRecorder(10)

	fakeGrouper := &fakePodGrouper{
		getPodOwnersFn: func(ctx context.Context, pod *v1.Pod) (*unstructured.Unstructured, []*metav1.PartialObjectMetadata, error) {
			return &unstructured.Unstructured{}, nil, nil
		},
		getPGMetadataFn: func(ctx context.Context, pod *v1.Pod, topOwner *unstructured.Unstructured, allOwners []*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
			return &podgroup.Metadata{
				Namespace:      pod.Namespace,
				Name:           "my-podgroup",
				MinAvailable:   1,
				Preemptibility: schedulingv2alpha2.SemiPreemptible,
				Warnings:       []string{"semi-preemptible is not compatible with automatic segmentation"},
			}, nil
		},
	}

	podReconciler := PodReconciler{
		Client:          fakeClient,
		Scheme:          testScheme,
		podGrouper:      fakeGrouper,
		PodGroupHandler: podgroup.NewHandler(fakeClient, "", ""),
		configs:         Configs{SchedulerName: "kai-scheduler"},
		eventRecorder:   fakeRecorder,
	}

	_, err := podReconciler.Reconcile(context.TODO(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name},
	})
	assert.NoError(t, err)

	assert.Len(t, fakeRecorder.Events, 1)
	event := <-fakeRecorder.Events
	assert.Contains(t, event, "semi-preemptible is not compatible with automatic segmentation")
}

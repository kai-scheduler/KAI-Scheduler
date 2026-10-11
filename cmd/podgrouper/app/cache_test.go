// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	controllers "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper"
)

func TestCacheTransformPreservesPodLifecycle(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "job", Namespace: "workloads",
			Annotations:     map[string]string{"pod-group-name": "pg-job"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Job", Name: "job"}},
			ManagedFields:   []metav1.ManagedFieldsEntry{{Manager: "controller"}},
		},
		Spec:   corev1.PodSpec{SchedulerName: "kai-scheduler"},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	expected := pod.DeepCopy()
	expected.ManagedFields = nil
	options := getCacheOptions(controllers.Configs{SchedulerName: "kai-scheduler"})
	transformed, err := options.DefaultTransform(pod)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, transformed) {
		t.Fatal("cache transform changed lifecycle, ownership, or grouping fields")
	}
}

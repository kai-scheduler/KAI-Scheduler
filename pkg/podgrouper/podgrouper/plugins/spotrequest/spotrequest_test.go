// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package spotrequest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestGetPodGroupMetadata(t *testing.T) {
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "test_namespace",
			Labels:    map[string]string{},
			UID:       "3",
		},
	}

	// ownerReference to unstructured
	rawObjectMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	assert.NoError(t, err)

	unstructuredPod := &unstructured.Unstructured{
		Object: rawObjectMap,
	}

	grouper := NewSpotRequestGrouper(defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient()))
	podGroupMetadata, err := grouper.GetPodGroupMetadata(unstructuredPod, pod)
	assert.NoError(t, err)
	assert.Equal(t, constants.InferencePriorityClass, podGroupMetadata.PriorityClassName)
}

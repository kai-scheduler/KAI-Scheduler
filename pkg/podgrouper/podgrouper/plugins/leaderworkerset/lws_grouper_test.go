// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package leader_worker_set

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func baseOwner(name string, startupPolicy string, replicas int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "LeaderWorkerSet",
			"apiVersion": "leaderworkerset.x-k8s.io/v1",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": "default",
				"uid":       name + "-uid",
			},
			"spec": map[string]interface{}{
				"startupPolicy": startupPolicy,
				"leaderWorkerTemplate": map[string]interface{}{
					"size": replicas,
				},
			},
		},
	}
}

func TestGetPodGroupMetadata_LeaderCreated(t *testing.T) {
	owner := baseOwner("lws-test", "LeaderCreated", 3)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{},
		},
	}

	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	podGroupMetadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(3), podGroupMetadata.MinAvailable)
	assert.Equal(t, "LeaderWorkerSet", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "leaderworkerset.x-k8s.io/v1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "lws-test", podGroupMetadata.Owner.Name)
	assert.Equal(t, "lws-test-uid", string(podGroupMetadata.Owner.UID))
}

func TestGetPodGroupMetadata_LeaderReady_LeaderPod(t *testing.T) {
	owner := baseOwner("lws-ready", "LeaderReady", 5)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Spec: v1.PodSpec{
			NodeName: "", // not scheduled => simulate leader
		},
	}

	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	podGroupMetadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(5), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_LeaderReady_WorkerPod(t *testing.T) {
	owner := baseOwner("lws-ready", "LeaderReady", 5)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"leaderworkerset.sigs.k8s.io/size": "5",
			},
			Labels: map[string]string{
				"leaderworkerset.sigs.k8s.io/group-index": "0",
			},
		},
		Spec: v1.PodSpec{
			NodeName: "worker-node", // scheduled => simulate worker
		},
	}

	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	podGroupMetadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(5), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_GroupIndex_Label(t *testing.T) {
	owner := baseOwner("lws-grouped", "LeaderCreated", 2)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"leaderworkerset.sigs.k8s.io/group-index": "1",
			},
		},
	}

	lwsGrouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
	podGroupMetadata, err := lwsGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Contains(t, podGroupMetadata.Name, "-group-1")
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jax_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow/jax"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func newTestJaxGrouper() *jax.JaxGrouper {
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	return jax.NewJaxGrouper(kubeFlowGrouper)
}

func newBasicJaxJob(workerReplicas int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "JAXJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-jax-job",
				"namespace": "test-namespace",
				"uid":       "jax-uid-1",
			},
			"spec": map[string]interface{}{
				"jaxReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{
						"replicas": workerReplicas,
					},
				},
			},
		},
	}
}

func TestName(t *testing.T) {
	grouper := newTestJaxGrouper()
	assert.Equal(t, "JAX Grouper", grouper.Name())
}

func TestGetPodGroupMetadata_BasicWorkerReplicas(t *testing.T) {
	jaxJob := newBasicJaxJob(4)
	pod := &v1.Pod{}
	grouper := newTestJaxGrouper()

	metadata, err := grouper.GetPodGroupMetadata(jaxJob, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(4), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_SingleWorkerReplica(t *testing.T) {
	jaxJob := newBasicJaxJob(1)
	pod := &v1.Pod{}
	grouper := newTestJaxGrouper()

	metadata, err := grouper.GetPodGroupMetadata(jaxJob, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(1), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_RunPolicyMinAvailable(t *testing.T) {
	jaxJob := newBasicJaxJob(4)
	err := unstructured.SetNestedField(jaxJob.Object, int64(2), "spec", "runPolicy", "schedulingPolicy", "minAvailable")
	assert.Nil(t, err)

	pod := &v1.Pod{}
	grouper := newTestJaxGrouper()

	metadata, err := grouper.GetPodGroupMetadata(jaxJob, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(2), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_MissingJaxReplicaSpecs(t *testing.T) {
	jaxJob := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "JAXJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-jax-job",
				"namespace": "test-namespace",
				"uid":       "jax-uid-2",
			},
			"spec": map[string]interface{}{},
		},
	}
	pod := &v1.Pod{}
	grouper := newTestJaxGrouper()

	_, err := grouper.GetPodGroupMetadata(jaxJob, pod)

	assert.NotNil(t, err)
}

func TestGetPodGroupMetadata_MissingWorkerSpec(t *testing.T) {
	// jaxReplicaSpecs exists but has no Worker key — Worker is mandatory
	jaxJob := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "JAXJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-jax-job",
				"namespace": "test-namespace",
				"uid":       "jax-uid-3",
			},
			"spec": map[string]interface{}{
				"jaxReplicaSpecs": map[string]interface{}{
					"SomeOtherReplica": map[string]interface{}{
						"replicas": int64(2),
					},
				},
			},
		},
	}
	pod := &v1.Pod{}
	grouper := newTestJaxGrouper()

	_, err := grouper.GetPodGroupMetadata(jaxJob, pod)

	assert.NotNil(t, err)
}

func TestGetPodGroupMetadata_ZeroWorkerReplicas(t *testing.T) {
	jaxJob := newBasicJaxJob(0)
	pod := &v1.Pod{}
	grouper := newTestJaxGrouper()

	_, err := grouper.GetPodGroupMetadata(jaxJob, pod)

	assert.NotNil(t, err)
}

func TestGetPodGroupMetadata_OwnerMetadata(t *testing.T) {
	jaxJob := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "JAXJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "my-jax-job",
				"namespace": "my-namespace",
				"uid":       "jax-uid-owner",
				"labels": map[string]interface{}{
					"app": "training",
				},
				"annotations": map[string]interface{}{
					"note": "test",
				},
			},
			"spec": map[string]interface{}{
				"jaxReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{
						"replicas": int64(2),
					},
				},
			},
		},
	}
	pod := &v1.Pod{}
	grouper := newTestJaxGrouper()

	metadata, err := grouper.GetPodGroupMetadata(jaxJob, pod)

	assert.Nil(t, err)
	assert.Equal(t, "JAXJob", metadata.Owner.Kind)
	assert.Equal(t, "kubeflow.org/v1", metadata.Owner.APIVersion)
	assert.Equal(t, "my-jax-job", metadata.Owner.Name)
	assert.Equal(t, "jax-uid-owner", string(metadata.Owner.UID))
	assert.Equal(t, int32(2), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_WorkerPodIsReferenced(t *testing.T) {
	jaxJob := newBasicJaxJob(3)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-jax-job-worker-0",
			Namespace: "test-namespace",
		},
	}
	grouper := newTestJaxGrouper()

	// GetPodGroupMetadata should not error even when pod has no special labels
	metadata, err := grouper.GetPodGroupMetadata(jaxJob, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(3), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_QueueFromOwnerLabel(t *testing.T) {
	jaxJob := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "JAXJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-jax-job",
				"namespace": "test-namespace",
				"uid":       "jax-uid-q",
				"labels": map[string]interface{}{
					queueLabelKey: "gpu-queue",
				},
			},
			"spec": map[string]interface{}{
				"jaxReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{
						"replicas": int64(2),
					},
				},
			},
		},
	}
	pod := &v1.Pod{}
	grouper := newTestJaxGrouper()

	metadata, err := grouper.GetPodGroupMetadata(jaxJob, pod)

	assert.Nil(t, err)
	assert.Equal(t, "gpu-queue", metadata.Queue)
}

func TestGetPodGroupMetadata_RunPolicyOverridesReplicas(t *testing.T) {
	// Even when Worker has many replicas, minAvailable from runPolicy wins
	jaxJob := newBasicJaxJob(100)
	err := unstructured.SetNestedField(jaxJob.Object, int64(10), "spec", "runPolicy", "schedulingPolicy", "minAvailable")
	assert.Nil(t, err)

	pod := &v1.Pod{}
	grouper := newTestJaxGrouper()

	metadata, err := grouper.GetPodGroupMetadata(jaxJob, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(10), metadata.MinAvailable)
}

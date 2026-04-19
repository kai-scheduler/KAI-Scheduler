// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package tensorflow_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow/tensorflow"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func newTestTFGrouper() *tensorflow.TensorFlowGrouper {
	dg := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kg := kubeflow.NewKubeflowDistributedGrouper(dg)
	return tensorflow.NewTensorFlowGrouper(kg)
}

// newTFJob builds a TFJob unstructured object from a map of replica-type → replica-count.
func newTFJob(name, namespace, uid string, replicaSpecs map[string]int64) *unstructured.Unstructured {
	specs := map[string]interface{}{}
	for k, v := range replicaSpecs {
		specs[k] = map[string]interface{}{
			"replicas": v,
		}
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"uid":       uid,
			},
			"spec": map[string]interface{}{
				"tfReplicaSpecs": specs,
			},
		},
	}
}

func newBasicTFJob() *unstructured.Unstructured {
	return newTFJob("test-tfjob", "test-namespace", "tf-uid-1", map[string]int64{
		"Worker": 4,
		"PS":     2,
	})
}

// ── Name ─────────────────────────────────────────────────────────────────────

func TestName(t *testing.T) {
	grouper := newTestTFGrouper()
	assert.Equal(t, "TensorFlow Grouper", grouper.Name())
}

// ── MinAvailable from replica specs ──────────────────────────────────────────

func TestGetPodGroupMetadata_WorkerOnly(t *testing.T) {
	job := newTFJob("tf-job", "ns", "uid-w", map[string]int64{"Worker": 3})
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(3), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_PSOnly(t *testing.T) {
	job := newTFJob("tf-job", "ns", "uid-ps", map[string]int64{"PS": 2})
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(2), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_WorkerAndPS(t *testing.T) {
	job := newTFJob("tf-job", "ns", "uid-wp", map[string]int64{"Worker": 4, "PS": 2})
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(6), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_WorkerPSChief(t *testing.T) {
	job := newTFJob("tf-job", "ns", "uid-wpc", map[string]int64{
		"Worker": 4,
		"PS":     2,
		"Chief":  1,
	})
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(7), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_AllReplicaTypes(t *testing.T) {
	job := newTFJob("tf-job", "ns", "uid-all", map[string]int64{
		"Worker":    4,
		"PS":        2,
		"Chief":     1,
		"Evaluator": 1,
	})
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(8), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_SingleWorker(t *testing.T) {
	job := newTFJob("tf-job", "ns", "uid-1w", map[string]int64{"Worker": 1})
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(1), metadata.MinAvailable)
}

// ── runPolicy.schedulingPolicy.minAvailable overrides ────────────────────────

func TestGetPodGroupMetadata_RunPolicyMinAvailableOverridesReplicas(t *testing.T) {
	job := newBasicTFJob() // Worker=4, PS=2  → total 6
	err := unstructured.SetNestedField(job.Object, int64(3),
		"spec", "runPolicy", "schedulingPolicy", "minAvailable")
	assert.Nil(t, err)

	grouper := newTestTFGrouper()

	metadata, metaErr := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, metaErr)
	assert.Equal(t, int32(3), metadata.MinAvailable)
}

// runPolicy wins even when replica counts would otherwise cause an error (e.g. zero replicas).
func TestGetPodGroupMetadata_RunPolicyMinAvailableOverridesZeroReplicas(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "tf-job",
				"namespace": "ns",
				"uid":       "uid-rp0",
			},
			"spec": map[string]interface{}{
				"runPolicy": map[string]interface{}{
					"schedulingPolicy": map[string]interface{}{
						"minAvailable": int64(5),
					},
				},
				"tfReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{
						"replicas": int64(0),
					},
				},
			},
		},
	}
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(5), metadata.MinAvailable)
}

// ── Error cases ───────────────────────────────────────────────────────────────

func TestGetPodGroupMetadata_MissingTfReplicaSpecs(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "tf-job",
				"namespace": "ns",
				"uid":       "uid-nospecs",
			},
			"spec": map[string]interface{}{},
		},
	}
	grouper := newTestTFGrouper()

	_, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "tfReplicaSpecs")
}

func TestGetPodGroupMetadata_ZeroWorkerReplicas(t *testing.T) {
	job := newTFJob("tf-job", "ns", "uid-zero", map[string]int64{"Worker": 0})
	grouper := newTestTFGrouper()

	_, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.NotNil(t, err)
}

func TestGetPodGroupMetadata_ZeroPSReplicas(t *testing.T) {
	job := newTFJob("tf-job", "ns", "uid-zerops", map[string]int64{"PS": 0})
	grouper := newTestTFGrouper()

	_, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.NotNil(t, err)
}

func TestGetPodGroupMetadata_MissingReplicasField(t *testing.T) {
	// Worker spec exists but has no "replicas" key → treated as 0 → error.
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "tf-job",
				"namespace": "ns",
				"uid":       "uid-noreplicas",
			},
			"spec": map[string]interface{}{
				"tfReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{},
				},
			},
		},
	}
	grouper := newTestTFGrouper()

	_, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.NotNil(t, err)
}

// ── Owner metadata ────────────────────────────────────────────────────────────

func TestGetPodGroupMetadata_OwnerMetadata(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "my-tfjob",
				"namespace": "my-namespace",
				"uid":       "tf-uid-owner",
			},
			"spec": map[string]interface{}{
				"tfReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{"replicas": int64(2)},
				},
			},
		},
	}
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "TFJob", metadata.Owner.Kind)
	assert.Equal(t, "kubeflow.org/v1", metadata.Owner.APIVersion)
	assert.Equal(t, "my-tfjob", metadata.Owner.Name)
	assert.Equal(t, "tf-uid-owner", string(metadata.Owner.UID))
}

func TestGetPodGroupMetadata_PodGroupNameContainsOwnerNameAndUID(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "named-job",
				"namespace": "ns",
				"uid":       "uid-named",
			},
			"spec": map[string]interface{}{
				"tfReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{"replicas": int64(1)},
				},
			},
		},
	}
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Contains(t, metadata.Name, "named-job")
	assert.Contains(t, metadata.Name, "uid-named")
}

// ── Queue resolution ──────────────────────────────────────────────────────────

func TestGetPodGroupMetadata_QueueFromOwnerLabel(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "tf-job",
				"namespace": "ns",
				"uid":       "uid-q",
				"labels": map[string]interface{}{
					queueLabelKey: "gpu-queue",
				},
			},
			"spec": map[string]interface{}{
				"tfReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{"replicas": int64(2)},
				},
			},
		},
	}
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "gpu-queue", metadata.Queue)
}

func TestGetPodGroupMetadata_QueueFromPodLabel(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{queueLabelKey: "pod-queue"},
		},
	}
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicTFJob(), pod)

	assert.Nil(t, err)
	assert.Equal(t, "pod-queue", metadata.Queue)
}

func TestGetPodGroupMetadata_DefaultQueue(t *testing.T) {
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicTFJob(), &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, constants.DefaultQueueName, metadata.Queue)
}

func TestGetPodGroupMetadata_OwnerQueueTakesPrecedenceOverPod(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "tf-job",
				"namespace": "ns",
				"uid":       "uid-q2",
				"labels": map[string]interface{}{
					queueLabelKey: "owner-queue",
				},
			},
			"spec": map[string]interface{}{
				"tfReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{"replicas": int64(2)},
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{queueLabelKey: "pod-queue"},
		},
	}
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, pod)

	assert.Nil(t, err)
	assert.Equal(t, "owner-queue", metadata.Queue)
}

// ── Priority class ────────────────────────────────────────────────────────────

// TensorFlowGrouper uses DefaultGrouper's default, which is "train" (not "build").
func TestGetPodGroupMetadata_DefaultPriorityClassIsTrain(t *testing.T) {
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicTFJob(), &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, constants.TrainPriorityClass, metadata.PriorityClassName)
}

// ── Labels / annotations propagation ─────────────────────────────────────────

func TestGetPodGroupMetadata_AnnotationsInheritedFromOwner(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "tf-job",
				"namespace": "ns",
				"uid":       "uid-ann",
				"annotations": map[string]interface{}{
					"my-annotation": "my-value",
				},
			},
			"spec": map[string]interface{}{
				"tfReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{"replicas": int64(1)},
				},
			},
		},
	}
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "my-value", metadata.Annotations["my-annotation"])
}

func TestGetPodGroupMetadata_LabelsInheritedFromOwner(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "tf-job",
				"namespace": "ns",
				"uid":       "uid-lbl",
				"labels": map[string]interface{}{
					"my-label": "my-label-value",
				},
			},
			"spec": map[string]interface{}{
				"tfReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{"replicas": int64(1)},
				},
			},
		},
	}
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "my-label-value", metadata.Labels["my-label"])
}

// ── Topology ──────────────────────────────────────────────────────────────────

func TestGetPodGroupMetadata_TopologyFromOwnerAnnotation(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TFJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "tf-job",
				"namespace": "ns",
				"uid":       "uid-topo",
				"annotations": map[string]interface{}{
					"kai.scheduler/topology":                    "cluster-topology",
					"kai.scheduler/topology-required-placement": "rack",
				},
			},
			"spec": map[string]interface{}{
				"tfReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{"replicas": int64(2)},
				},
			},
		},
	}
	grouper := newTestTFGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "cluster-topology", metadata.Topology)
	assert.Equal(t, "rack", metadata.RequiredTopologyLevel)
}

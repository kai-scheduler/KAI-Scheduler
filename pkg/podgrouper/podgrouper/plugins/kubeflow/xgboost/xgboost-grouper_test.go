// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package xgboost_test

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
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow/xgboost"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func newTestXGBGrouper() *xgboost.XGBoostGrouper {
	dg := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kg := kubeflow.NewKubeflowDistributedGrouper(dg)
	return xgboost.NewXGBoostGrouper(kg)
}

// newXGBJob builds an XGBoostJob from a replica map.
// Pass nil replicaSpecs to produce a job with no xgbReplicaSpecs field.
func newXGBJob(name, namespace, uid string, replicaSpecs map[string]int64) *unstructured.Unstructured {
	obj := map[string]interface{}{
		"kind":       "XGBoostJob",
		"apiVersion": "kubeflow.org/v1",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"uid":       uid,
		},
		"spec": map[string]interface{}{},
	}
	if replicaSpecs != nil {
		specs := map[string]interface{}{}
		for k, v := range replicaSpecs {
			specs[k] = map[string]interface{}{"replicas": v}
		}
		obj["spec"].(map[string]interface{})["xgbReplicaSpecs"] = specs
	}
	return &unstructured.Unstructured{Object: obj}
}

// newBasicXGBJob returns a valid job with 1 Master and 3 Workers.
func newBasicXGBJob() *unstructured.Unstructured {
	return newXGBJob("test-xgbjob", "test-namespace", "xgb-uid-1", map[string]int64{
		"Master": 1,
		"Worker": 3,
	})
}

// ── Name ─────────────────────────────────────────────────────────────────────

func TestName(t *testing.T) {
	assert.Equal(t, "XGBoost Grouper", newTestXGBGrouper().Name())
}

// ── MinAvailable from replica specs ──────────────────────────────────────────

func TestGetPodGroupMetadata_MasterAndWorker(t *testing.T) {
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicXGBJob(), &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(4), metadata.MinAvailable) // 1 Master + 3 Workers
}

func TestGetPodGroupMetadata_MultipleMasterAndWorker(t *testing.T) {
	job := newXGBJob("xgb", "ns", "uid-m", map[string]int64{
		"Master": 2,
		"Worker": 8,
	})
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(10), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_SingleMasterSingleWorker(t *testing.T) {
	job := newXGBJob("xgb", "ns", "uid-11", map[string]int64{
		"Master": 1,
		"Worker": 1,
	})
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(2), metadata.MinAvailable)
}

// ── runPolicy.schedulingPolicy.minAvailable ───────────────────────────────────

func TestGetPodGroupMetadata_RunPolicyOverridesReplicaSum(t *testing.T) {
	job := newBasicXGBJob() // would be 1+3=4 without runPolicy
	err := unstructured.SetNestedField(job.Object, int64(2),
		"spec", "runPolicy", "schedulingPolicy", "minAvailable")
	assert.Nil(t, err)

	grouper := newTestXGBGrouper()
	metadata, metaErr := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, metaErr)
	assert.Equal(t, int32(2), metadata.MinAvailable)
}

// When runPolicy is present the mandatory-spec check is skipped entirely.
func TestGetPodGroupMetadata_RunPolicySkipsMandatorySpecValidation(t *testing.T) {
	// Job has neither Master nor Worker, but runPolicy covers it.
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "XGBoostJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name": "xgb", "namespace": "ns", "uid": "uid-rp",
			},
			"spec": map[string]interface{}{
				"runPolicy": map[string]interface{}{
					"schedulingPolicy": map[string]interface{}{
						"minAvailable": int64(5),
					},
				},
				// No xgbReplicaSpecs at all — would normally error.
			},
		},
	}
	grouper := newTestXGBGrouper()
	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(5), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_RunPolicyOverridesZeroReplicas(t *testing.T) {
	// Zero Worker replicas would fail without runPolicy.
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "XGBoostJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name": "xgb", "namespace": "ns", "uid": "uid-rpz",
			},
			"spec": map[string]interface{}{
				"runPolicy": map[string]interface{}{
					"schedulingPolicy": map[string]interface{}{
						"minAvailable": int64(3),
					},
				},
				"xgbReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{"replicas": int64(0)},
					"Worker": map[string]interface{}{"replicas": int64(0)},
				},
			},
		},
	}
	grouper := newTestXGBGrouper()
	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(3), metadata.MinAvailable)
}

// ── Mandatory replica spec errors ─────────────────────────────────────────────

func TestGetPodGroupMetadata_MissingXgbReplicaSpecs(t *testing.T) {
	job := newXGBJob("xgb", "ns", "uid-nospecs", nil)
	grouper := newTestXGBGrouper()

	_, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "xgbReplicaSpecs")
}

func TestGetPodGroupMetadata_MissingMasterSpec(t *testing.T) {
	// Worker present but no Master → error because Master is mandatory.
	job := newXGBJob("xgb", "ns", "uid-nom", map[string]int64{"Worker": 3})
	grouper := newTestXGBGrouper()

	_, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Master")
}

func TestGetPodGroupMetadata_MissingWorkerSpec(t *testing.T) {
	// Master present but no Worker → error because Worker is mandatory.
	job := newXGBJob("xgb", "ns", "uid-now", map[string]int64{"Master": 1})
	grouper := newTestXGBGrouper()

	_, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Worker")
}

func TestGetPodGroupMetadata_ZeroMasterReplicas(t *testing.T) {
	job := newXGBJob("xgb", "ns", "uid-0m", map[string]int64{"Master": 0, "Worker": 3})
	grouper := newTestXGBGrouper()

	_, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.NotNil(t, err)
}

func TestGetPodGroupMetadata_ZeroWorkerReplicas(t *testing.T) {
	job := newXGBJob("xgb", "ns", "uid-0w", map[string]int64{"Master": 1, "Worker": 0})
	grouper := newTestXGBGrouper()

	_, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.NotNil(t, err)
}

func TestGetPodGroupMetadata_MissingReplicasField(t *testing.T) {
	// Spec exists but no "replicas" key → treated as 0 → error.
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "XGBoostJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name": "xgb", "namespace": "ns", "uid": "uid-noreplicas",
			},
			"spec": map[string]interface{}{
				"xgbReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{},
					"Worker": map[string]interface{}{"replicas": int64(2)},
				},
			},
		},
	}
	grouper := newTestXGBGrouper()

	_, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.NotNil(t, err)
}

// ── Owner metadata ────────────────────────────────────────────────────────────

func TestGetPodGroupMetadata_OwnerMetadata(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "XGBoostJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "my-xgbjob",
				"namespace": "my-namespace",
				"uid":       "xgb-uid-owner",
			},
			"spec": map[string]interface{}{
				"xgbReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{"replicas": int64(1)},
					"Worker": map[string]interface{}{"replicas": int64(2)},
				},
			},
		},
	}
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "XGBoostJob", metadata.Owner.Kind)
	assert.Equal(t, "kubeflow.org/v1", metadata.Owner.APIVersion)
	assert.Equal(t, "my-xgbjob", metadata.Owner.Name)
	assert.Equal(t, "xgb-uid-owner", string(metadata.Owner.UID))
}

func TestGetPodGroupMetadata_PodGroupNameContainsOwnerNameAndUID(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "XGBoostJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name": "named-xgb", "namespace": "ns", "uid": "uid-named",
			},
			"spec": map[string]interface{}{
				"xgbReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{"replicas": int64(1)},
					"Worker": map[string]interface{}{"replicas": int64(1)},
				},
			},
		},
	}
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Contains(t, metadata.Name, "named-xgb")
	assert.Contains(t, metadata.Name, "uid-named")
}

// ── Queue resolution ──────────────────────────────────────────────────────────

func TestGetPodGroupMetadata_QueueFromOwnerLabel(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "XGBoostJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name": "xgb", "namespace": "ns", "uid": "uid-q",
				"labels": map[string]interface{}{queueLabelKey: "gpu-queue"},
			},
			"spec": map[string]interface{}{
				"xgbReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{"replicas": int64(1)},
					"Worker": map[string]interface{}{"replicas": int64(2)},
				},
			},
		},
	}
	grouper := newTestXGBGrouper()

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
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicXGBJob(), pod)

	assert.Nil(t, err)
	assert.Equal(t, "pod-queue", metadata.Queue)
}

func TestGetPodGroupMetadata_DefaultQueue(t *testing.T) {
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicXGBJob(), &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, constants.DefaultQueueName, metadata.Queue)
}

func TestGetPodGroupMetadata_OwnerQueueTakesPrecedenceOverPod(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "XGBoostJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name": "xgb", "namespace": "ns", "uid": "uid-q2",
				"labels": map[string]interface{}{queueLabelKey: "owner-queue"},
			},
			"spec": map[string]interface{}{
				"xgbReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{"replicas": int64(1)},
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
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, pod)

	assert.Nil(t, err)
	assert.Equal(t, "owner-queue", metadata.Queue)
}

// ── Priority class ────────────────────────────────────────────────────────────

// XGBoostGrouper does not override priority class → defaults to "train".
func TestGetPodGroupMetadata_DefaultPriorityClassIsTrain(t *testing.T) {
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicXGBJob(), &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, constants.TrainPriorityClass, metadata.PriorityClassName)
}

// ── Labels / annotations propagation ─────────────────────────────────────────

func TestGetPodGroupMetadata_AnnotationsInheritedFromOwner(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "XGBoostJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name": "xgb", "namespace": "ns", "uid": "uid-ann",
				"annotations": map[string]interface{}{"my-key": "my-val"},
			},
			"spec": map[string]interface{}{
				"xgbReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{"replicas": int64(1)},
					"Worker": map[string]interface{}{"replicas": int64(1)},
				},
			},
		},
	}
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "my-val", metadata.Annotations["my-key"])
}

func TestGetPodGroupMetadata_LabelsInheritedFromOwner(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "XGBoostJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name": "xgb", "namespace": "ns", "uid": "uid-lbl",
				"labels": map[string]interface{}{"my-label": "my-label-val"},
			},
			"spec": map[string]interface{}{
				"xgbReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{"replicas": int64(1)},
					"Worker": map[string]interface{}{"replicas": int64(1)},
				},
			},
		},
	}
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "my-label-val", metadata.Labels["my-label"])
}

// ── Topology ──────────────────────────────────────────────────────────────────

func TestGetPodGroupMetadata_TopologyFromOwnerAnnotation(t *testing.T) {
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "XGBoostJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name": "xgb", "namespace": "ns", "uid": "uid-topo",
				"annotations": map[string]interface{}{
					"kai.scheduler/topology":                    "cluster-topology",
					"kai.scheduler/topology-required-placement": "rack",
				},
			},
			"spec": map[string]interface{}{
				"xgbReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{"replicas": int64(1)},
					"Worker": map[string]interface{}{"replicas": int64(2)},
				},
			},
		},
	}
	grouper := newTestXGBGrouper()

	metadata, err := grouper.GetPodGroupMetadata(job, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "cluster-topology", metadata.Topology)
	assert.Equal(t, "rack", metadata.RequiredTopologyLevel)
}

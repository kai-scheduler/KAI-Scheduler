// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podjob_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/podjob"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/spark"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func newTestGrouper() *podjob.PodJobGrouper {
	dg := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	sg := spark.NewSparkGrouper(dg)
	return podjob.NewPodJobGrouper(dg, sg)
}

// podToUnstructured converts a Pod to the unstructured form used as topOwner.
func podToUnstructured(pod *v1.Pod) *unstructured.Unstructured {
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: raw}
}

// newBasicPod builds a plain pod with no Spark labels.
func newBasicPod(name, namespace, uid string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(uid),
		},
	}
}

// newSparkPod builds a pod carrying both Spark labels required by IsSparkPod.
func newSparkPod(name, namespace, uid, appName, selector string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(uid),
			Labels: map[string]string{
				"spark-app-name":     appName,
				"spark-app-selector": selector,
			},
		},
	}
}

// ── Routing: non-Spark pods ───────────────────────────────────────────────────

func TestGetPodGroupMetadata_NonSparkPod_UsesDefaultGrouper(t *testing.T) {
	pod := newBasicPod("my-pod", "ns", "uid-basic")
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.NotNil(t, metadata)
	// DefaultGrouper names the PodGroup as pg-<name>-<uid>
	assert.Contains(t, metadata.Name, "my-pod")
	assert.Contains(t, metadata.Name, "uid-basic")
}

func TestGetPodGroupMetadata_PodWithOnlySparkAppName_UsesDefaultGrouper(t *testing.T) {
	// Only spark-app-name, no spark-app-selector → IsSparkPod returns false.
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "half-spark",
			Namespace: "ns",
			UID:       "uid-half",
			Labels:    map[string]string{"spark-app-name": "my-app"},
		},
	}
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	// SparkGrouper would set Name to the selector label; DefaultGrouper uses pg-<name>-<uid>.
	// Name should NOT equal the spark-app-selector (which is absent).
	assert.Contains(t, metadata.Name, "half-spark")
}

func TestGetPodGroupMetadata_PodWithOnlySparkAppSelector_UsesDefaultGrouper(t *testing.T) {
	// Only spark-app-selector, no spark-app-name → IsSparkPod returns false.
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sel-only",
			Namespace: "ns",
			UID:       "uid-sel",
			Labels:    map[string]string{"spark-app-selector": "my-selector"},
		},
	}
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	// Name must come from DefaultGrouper (not the spark selector).
	assert.NotEqual(t, "my-selector", metadata.Name)
	assert.Contains(t, metadata.Name, "sel-only")
}

// ── Routing: Spark pods ───────────────────────────────────────────────────────

func TestGetPodGroupMetadata_SparkPod_UsesSparkGrouper(t *testing.T) {
	pod := newSparkPod("driver-pod", "ns", "uid-spark", "my-spark-app", "spark-pg-selector")
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	// SparkGrouper sets Name to pod.Labels["spark-app-selector"].
	assert.Equal(t, "spark-pg-selector", metadata.Name)
}

func TestGetPodGroupMetadata_SparkPod_GroupNameEqualsSelector(t *testing.T) {
	const selector = "my-spark-pg-abc123"
	pod := newSparkPod("exec-0", "ns", "uid-sp2", "spark-job", selector)
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, selector, metadata.Name)
}

// ── Owner metadata (non-Spark path) ──────────────────────────────────────────

func TestGetPodGroupMetadata_OwnerMetadata(t *testing.T) {
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "my-namespace",
			UID:       "pod-uid-owner",
		},
	}
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "Pod", metadata.Owner.Kind)
	assert.Equal(t, "v1", metadata.Owner.APIVersion)
	assert.Equal(t, "my-pod", metadata.Owner.Name)
	assert.Equal(t, "pod-uid-owner", string(metadata.Owner.UID))
}

// ── PodGroup name format (non-Spark) ─────────────────────────────────────────

func TestGetPodGroupMetadata_NonSparkPodGroupNameFormat(t *testing.T) {
	pod := newBasicPod("worker-0", "ns", "uid-w0")
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "pg-worker-0-uid-w0", metadata.Name)
}

// ── MinAvailable default ──────────────────────────────────────────────────────

func TestGetPodGroupMetadata_NonSparkMinAvailableIsOne(t *testing.T) {
	pod := newBasicPod("pod-1", "ns", "uid-1")
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, int32(1), metadata.MinAvailable)
}

// ── Priority class ────────────────────────────────────────────────────────────

func TestGetPodGroupMetadata_NonSparkDefaultPriorityIsTrain(t *testing.T) {
	pod := newBasicPod("pod-p", "ns", "uid-p")
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, constants.TrainPriorityClass, metadata.PriorityClassName)
}

func TestGetPodGroupMetadata_SparkDefaultPriorityIsTrain(t *testing.T) {
	pod := newSparkPod("driver", "ns", "uid-spc", "app", "sel")
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, constants.TrainPriorityClass, metadata.PriorityClassName)
}

// ── Queue resolution ──────────────────────────────────────────────────────────

func TestGetPodGroupMetadata_QueueFromTopOwnerLabel(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-q",
			Namespace: "ns",
			UID:       "uid-q",
			Labels:    map[string]string{queueLabelKey: "gpu-queue"},
		},
	}
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "gpu-queue", metadata.Queue)
}

func TestGetPodGroupMetadata_DefaultQueue(t *testing.T) {
	pod := newBasicPod("pod-dq", "ns", "uid-dq")
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, constants.DefaultQueueName, metadata.Queue)
}

func TestGetPodGroupMetadata_SparkPodQueueFromLabel(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "spark-driver",
			Namespace: "ns",
			UID:       "uid-sq",
			Labels: map[string]string{
				"spark-app-name":     "app",
				"spark-app-selector": "selector",
				queueLabelKey:        "spark-queue",
			},
		},
	}
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "spark-queue", metadata.Queue)
	assert.Equal(t, "selector", metadata.Name)
}

// ── Labels / annotations propagation ─────────────────────────────────────────

func TestGetPodGroupMetadata_AnnotationsInheritedFromTopOwner(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pod-ann",
			Namespace:   "ns",
			UID:         "uid-ann",
			Annotations: map[string]string{"my-key": "my-val"},
		},
	}
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "my-val", metadata.Annotations["my-key"])
}

func TestGetPodGroupMetadata_LabelsInheritedFromTopOwner(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-lbl",
			Namespace: "ns",
			UID:       "uid-lbl",
			Labels:    map[string]string{"team": "ai"},
		},
	}
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "ai", metadata.Labels["team"])
}

// ── Topology (non-Spark) ──────────────────────────────────────────────────────

func TestGetPodGroupMetadata_TopologyFromTopOwnerAnnotation(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-topo",
			Namespace: "ns",
			UID:       "uid-topo",
			Annotations: map[string]string{
				"kai.scheduler/topology":                    "cluster-topo",
				"kai.scheduler/topology-required-placement": "rack",
			},
		},
	}
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "cluster-topo", metadata.Topology)
	assert.Equal(t, "rack", metadata.RequiredTopologyLevel)
}

// ── Namespace propagation ─────────────────────────────────────────────────────

func TestGetPodGroupMetadata_NamespaceFromPod(t *testing.T) {
	pod := newBasicPod("pod-ns", "special-ns", "uid-ns")
	topOwner := podToUnstructured(pod)
	grouper := newTestGrouper()

	metadata, err := grouper.GetPodGroupMetadata(topOwner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "special-ns", metadata.Namespace)
}

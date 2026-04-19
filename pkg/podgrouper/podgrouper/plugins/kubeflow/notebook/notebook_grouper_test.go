// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package notebook_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow/notebook"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func newGrouperWithPriorityClasses(pcs ...*schedulingv1.PriorityClass) *notebook.NotebookGrouper {
	scheme := runtime.NewScheme()
	_ = schedulingv1.AddToScheme(scheme)
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, pc := range pcs {
		builder = builder.WithObjects(pc)
	}
	dg := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, builder.Build())
	return notebook.NewNotebookGrouper(dg)
}

func newTestNotebookGrouper() *notebook.NotebookGrouper {
	return newGrouperWithPriorityClasses()
}

func newBasicNotebook() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Notebook",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-notebook",
				"namespace": "test-namespace",
				"uid":       "notebook-uid-1",
			},
		},
	}
}

func makePriorityClass(name string) *schedulingv1.PriorityClass {
	return &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Value:      1000,
	}
}

func TestName(t *testing.T) {
	grouper := newTestNotebookGrouper()
	assert.Equal(t, "Kubeflow Notebook Grouper", grouper.Name())
}

func TestGetPodGroupMetadata_DefaultPriorityClass(t *testing.T) {
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicNotebook(), &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, constants.BuildPriorityClass, metadata.PriorityClassName)
}

func TestGetPodGroupMetadata_DefaultPriorityClassIsBuildNotTrain(t *testing.T) {
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicNotebook(), &v1.Pod{})

	assert.Nil(t, err)
	assert.NotEqual(t, constants.TrainPriorityClass, metadata.PriorityClassName)
}

func TestGetPodGroupMetadata_ExplicitPriorityClassFromOwnerLabel(t *testing.T) {
	const customPriority = "my-custom-priority"
	grouper := newGrouperWithPriorityClasses(makePriorityClass(customPriority))

	nb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Notebook",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-notebook",
				"namespace": "test-namespace",
				"uid":       "notebook-uid-2",
				"labels": map[string]interface{}{
					constants.PriorityLabelKey: customPriority,
				},
			},
		},
	}

	metadata, err := grouper.GetPodGroupMetadata(nb, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, customPriority, metadata.PriorityClassName)
}

func TestGetPodGroupMetadata_InvalidPriorityLabelFallsBackToBuild(t *testing.T) {
	grouper := newTestNotebookGrouper()

	nb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Notebook",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-notebook",
				"namespace": "test-namespace",
				"uid":       "notebook-uid-3",
				"labels": map[string]interface{}{
					constants.PriorityLabelKey: "nonexistent-priority",
				},
			},
		},
	}

	metadata, err := grouper.GetPodGroupMetadata(nb, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, constants.BuildPriorityClass, metadata.PriorityClassName)
}

func TestGetPodGroupMetadata_MinAvailableIsOne(t *testing.T) {
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicNotebook(), &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, int32(1), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_OwnerMetadata(t *testing.T) {
	nb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Notebook",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "my-notebook",
				"namespace": "my-namespace",
				"uid":       "notebook-uid-owner",
			},
		},
	}
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(nb, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "Notebook", metadata.Owner.Kind)
	assert.Equal(t, "kubeflow.org/v1", metadata.Owner.APIVersion)
	assert.Equal(t, "my-notebook", metadata.Owner.Name)
	assert.Equal(t, "notebook-uid-owner", string(metadata.Owner.UID))
}

func TestGetPodGroupMetadata_QueueFromOwnerLabel(t *testing.T) {
	nb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Notebook",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-notebook",
				"namespace": "test-namespace",
				"uid":       "notebook-uid-q",
				"labels": map[string]interface{}{
					queueLabelKey: "gpu-queue",
				},
			},
		},
	}
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(nb, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "gpu-queue", metadata.Queue)
}

func TestGetPodGroupMetadata_QueueFromPodLabel(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{queueLabelKey: "pod-queue"},
		},
	}
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicNotebook(), pod)

	assert.Nil(t, err)
	assert.Equal(t, "pod-queue", metadata.Queue)
}

func TestGetPodGroupMetadata_DefaultQueue(t *testing.T) {
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(newBasicNotebook(), &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, constants.DefaultQueueName, metadata.Queue)
}

func TestGetPodGroupMetadata_OwnerQueueTakesPrecedenceOverPod(t *testing.T) {
	nb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Notebook",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-notebook",
				"namespace": "test-namespace",
				"uid":       "notebook-uid-q2",
				"labels": map[string]interface{}{
					queueLabelKey: "owner-queue",
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{queueLabelKey: "pod-queue"},
		},
	}
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(nb, pod)

	assert.Nil(t, err)
	assert.Equal(t, "owner-queue", metadata.Queue)
}

func TestGetPodGroupMetadata_PodGroupNameContainsOwnerNameAndUID(t *testing.T) {
	nb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Notebook",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "my-nb",
				"namespace": "ns",
				"uid":       "uid-abc",
			},
		},
	}
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(nb, &v1.Pod{})

	assert.Nil(t, err)
	assert.Contains(t, metadata.Name, "my-nb")
	assert.Contains(t, metadata.Name, "uid-abc")
}

func TestGetPodGroupMetadata_AnnotationsInheritedFromOwner(t *testing.T) {
	nb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Notebook",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-notebook",
				"namespace": "test-namespace",
				"uid":       "notebook-uid-ann",
				"annotations": map[string]interface{}{
					"my-annotation": "my-value",
				},
			},
		},
	}
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(nb, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "my-value", metadata.Annotations["my-annotation"])
}

func TestGetPodGroupMetadata_LabelsInheritedFromOwner(t *testing.T) {
	nb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Notebook",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-notebook",
				"namespace": "test-namespace",
				"uid":       "notebook-uid-lbl",
				"labels": map[string]interface{}{
					"my-label": "my-label-value",
				},
			},
		},
	}
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(nb, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "my-label-value", metadata.Labels["my-label"])
}

func TestGetPodGroupMetadata_TopologyFromOwnerAnnotation(t *testing.T) {
	nb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Notebook",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-notebook",
				"namespace": "test-namespace",
				"uid":       "notebook-uid-topo",
				"annotations": map[string]interface{}{
					"kai.scheduler/topology":                    "my-topology",
					"kai.scheduler/topology-required-placement": "rack",
				},
			},
		},
	}
	grouper := newTestNotebookGrouper()

	metadata, err := grouper.GetPodGroupMetadata(nb, &v1.Pod{})

	assert.Nil(t, err)
	assert.Equal(t, "my-topology", metadata.Topology)
	assert.Equal(t, "rack", metadata.RequiredTopologyLevel)
}

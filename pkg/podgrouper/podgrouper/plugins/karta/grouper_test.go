// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package karta

import (
	"fmt"
	"testing"
	"time"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/instructions"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

var defaultGrouper = defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())

func TestGetPodGroupMetadata_PodGroupV2(t *testing.T) {
	pyflow := getPyFlowObject()
	workerPod := getPyFlowPod("worker")
	kt := getPyFlowKarta()
	kt.Spec.Instructions = kartav1alpha1.OptimizationInstructions{
		GangScheduling: &kartav1alpha1.GangSchedulingInstruction{
			PodGroup: &kartav1alpha1.PodGroupComponentsMapping{
				Name: "training",
				Topology: &kartav1alpha1.TopologyConstraint{
					TopologyName:           "rack-topology",
					PreferredTopologyLevel: "rack",
					RequiredTopologyLevel:  "zone",
				},
				SubGroups: []kartav1alpha1.SubGroupComponentMapping{
					{ComponentName: "master"},
					{
						ComponentName: "worker",
						Topology: &kartav1alpha1.TopologyConstraint{
							TopologyName:           "gpu-topology",
							PreferredTopologyLevel: "host",
							RequiredTopologyLevel:  "rack",
						},
					},
				},
			},
		},
	}

	summary, err := instructions.NewStructureSummary(kt)
	assert.NoError(t, err)
	kartaGrouper := &KartaGrouper{
		kartaSummary:   summary,
		defaultGrouper: defaultGrouper,
	}

	metadata, err := kartaGrouper.GetPodGroupMetadata(pyflow, workerPod)
	assert.NoError(t, err)

	assert.Equal(t, fmt.Sprintf("pg-%s-training", pyflow.GetUID()), metadata.Name)
	assert.Equal(t, "rack-topology", metadata.Topology)
	assert.Equal(t, "rack", metadata.PreferredTopologyLevel)
	assert.Equal(t, "zone", metadata.RequiredTopologyLevel)
	assert.NotNil(t, metadata.MinSubGroup)
	assert.Equal(t, int32(2), *metadata.MinSubGroup)
	assert.Equal(t, int32(0), metadata.MinAvailable)

	assert.Len(t, metadata.SubGroups, 2)
	assert.Equal(t, "master", metadata.SubGroups[0].Name)
	assert.Equal(t, int32(1), metadata.SubGroups[0].MinAvailable)
	assert.Empty(t, metadata.SubGroups[0].PodsReferences)

	assert.Equal(t, "worker", metadata.SubGroups[1].Name)
	assert.Equal(t, int32(2), metadata.SubGroups[1].MinAvailable)
	assert.Equal(t, []string{workerPod.Name}, metadata.SubGroups[1].PodsReferences)
	assert.NotNil(t, metadata.SubGroups[1].TopologyConstraints)
	assert.Equal(t, "gpu-topology", metadata.SubGroups[1].TopologyConstraints.Topology)
	assert.Equal(t, "host", metadata.SubGroups[1].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "rack", metadata.SubGroups[1].TopologyConstraints.RequiredTopologyLevel)
}

func TestGetPodGroupMetadata_PodGroupV2DoesNotRequireSpecDefinition(t *testing.T) {
	pyflow := getPyFlowObject()
	workerPod := getPyFlowPod("worker")
	kt := getPyFlowKarta()
	for index := range kt.Spec.StructureDefinition.ChildComponents {
		kt.Spec.StructureDefinition.ChildComponents[index].SpecDefinition = nil
	}
	kt.Spec.Instructions = kartav1alpha1.OptimizationInstructions{
		GangScheduling: &kartav1alpha1.GangSchedulingInstruction{
			PodGroup: &kartav1alpha1.PodGroupComponentsMapping{
				Name: "training",
				SubGroups: []kartav1alpha1.SubGroupComponentMapping{
					{ComponentName: "master"},
					{ComponentName: "worker"},
				},
			},
		},
	}

	summary, err := instructions.NewStructureSummary(kt)
	assert.NoError(t, err)
	kartaGrouper := &KartaGrouper{
		kartaSummary:   summary,
		defaultGrouper: defaultGrouper,
	}

	metadata, err := kartaGrouper.GetPodGroupMetadata(pyflow, workerPod)
	assert.NoError(t, err)

	assert.Equal(t, fmt.Sprintf("pg-%s-training", pyflow.GetUID()), metadata.Name)
	assert.Len(t, metadata.SubGroups, 2)
	assert.Empty(t, metadata.SubGroups[0].PodsReferences)
	assert.Equal(t, []string{workerPod.Name}, metadata.SubGroups[1].PodsReferences)
}

func TestGetPodGroupMetadata_PodGroupV2TakesPrecedenceOverAlphaPodGroups(t *testing.T) {
	pyflow := getPyFlowObject()
	workerPod := getPyFlowPod("worker")
	kt := getPyFlowKarta()
	kt.Spec.Instructions = kartav1alpha1.OptimizationInstructions{
		GangScheduling: &kartav1alpha1.GangSchedulingInstruction{
			PodGroups: []kartav1alpha1.PodGroupDefinition{
				{
					Name: "alpha",
					Members: []kartav1alpha1.PodGroupMemberDefinition{
						{
							ComponentName:   "pyflow",
							GroupByKeyPaths: []string{".metadata.labels[\"job-name\"]"},
						},
					},
				},
			},
			PodGroup: &kartav1alpha1.PodGroupComponentsMapping{
				Name:      "v2",
				SubGroups: []kartav1alpha1.SubGroupComponentMapping{{ComponentName: "worker"}},
			},
		},
	}

	summary, err := instructions.NewStructureSummary(kt)
	assert.NoError(t, err)
	kartaGrouper := &KartaGrouper{
		kartaSummary:   summary,
		defaultGrouper: defaultGrouper,
	}

	metadata, err := kartaGrouper.GetPodGroupMetadata(pyflow, workerPod)
	assert.NoError(t, err)

	assert.Equal(t, fmt.Sprintf("pg-%s-v2", pyflow.GetUID()), metadata.Name)
	assert.Len(t, metadata.SubGroups, 1)
	assert.NotNil(t, metadata.MinSubGroup)
}

func TestGetPodGroupMetadata_AlphaPodGroups(t *testing.T) {
	pyflow := getPyFlowObject()
	workerPod := getPyFlowPod("worker")
	kt := getPyFlowKarta()
	kt.Spec.Instructions = kartav1alpha1.OptimizationInstructions{
		GangScheduling: &kartav1alpha1.GangSchedulingInstruction{
			PodGroups: []kartav1alpha1.PodGroupDefinition{
				{
					Name: "job",
					Members: []kartav1alpha1.PodGroupMemberDefinition{
						{
							ComponentName:   "pyflow",
							GroupByKeyPaths: []string{".metadata.labels[\"job-name\"]"},
						},
					},
				},
			},
		},
	}

	summary, err := instructions.NewStructureSummary(kt)
	assert.NoError(t, err)
	kartaGrouper := &KartaGrouper{
		kartaSummary:   summary,
		defaultGrouper: defaultGrouper,
	}

	metadata, err := kartaGrouper.GetPodGroupMetadata(pyflow, workerPod)
	assert.NoError(t, err)

	assert.Equal(t, int32(3), metadata.MinAvailable)
	assert.Nil(t, metadata.MinSubGroup)
	assert.Empty(t, metadata.SubGroups)
	assert.Equal(t, fmt.Sprintf("pg-%s-%s", pyflow.GetUID(), workerPod.Labels["job-name"]), metadata.Name)
}

func newFakeClientWithScheme(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = kartav1alpha1.AddToScheme(scheme)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

func newFakeClientWithSchemeAndInterceptor(interceptorFuncs interceptor.Funcs, objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = kartav1alpha1.AddToScheme(scheme)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithInterceptorFuncs(interceptorFuncs).
		Build()
}

func createTestGVK() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   "test.example.com",
		Version: "v1",
		Kind:    "TestResource",
	}
}

func createTestKarta(gvk metav1.GroupVersionKind, isDeleted bool) *kartav1alpha1.Karta {
	uid := types.UID("test-uid-" + gvk.Group + "-" + gvk.Version + "-" + gvk.Kind)
	return createTestKartaWithNameAndUID(gvk, "test-ri-"+string(uid), uid, isDeleted)
}

func createTestKartaWithNameAndUID(gvk metav1.GroupVersionKind, name string, uid types.UID, isDeleted ...bool) *kartav1alpha1.Karta {
	karta := &kartav1alpha1.Karta{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kartav1alpha1.GroupVersion.String(),
			Kind:       "Karta",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  uid,
			Labels: map[string]string{
				KartaGroupLabel:   gvk.Group,
				KartaVersionLabel: gvk.Version,
				KartaKindLabel:    gvk.Kind,
			},
		},
		Spec: kartav1alpha1.KartaSpec{
			StructureDefinition: kartav1alpha1.StructureDefinition{
				RootComponent: kartav1alpha1.ComponentDefinition{
					Name: "test-root",
					Kind: &kartav1alpha1.GroupVersionKind{
						Group:   gvk.Group,
						Version: gvk.Version,
						Kind:    gvk.Kind,
					},
					StatusDefinition: &kartav1alpha1.StatusDefinition{
						StatusMappings: kartav1alpha1.StatusMappings{},
					},
				},
				ChildComponents: []kartav1alpha1.ComponentDefinition{
					{
						Name:     "test-component",
						OwnerRef: stringPtr("test-root"),
						SpecDefinition: &kartav1alpha1.SpecDefinition{
							PodTemplateSpecPath: stringPtr(".spec.template"),
						},
						ScaleDefinition: &kartav1alpha1.ScaleDefinition{
							ReplicasPath: stringPtr(".spec.replicas"),
						},
						PodSelector: &kartav1alpha1.PodSelector{
							ComponentTypeSelector: &kartav1alpha1.ComponentTypeSelector{
								KeyPath: ".metadata.labels.role",
								Value:   stringPtr("worker"),
							},
						},
					},
				},
			},
			Instructions: kartav1alpha1.OptimizationInstructions{
				GangScheduling: &kartav1alpha1.GangSchedulingInstruction{
					PodGroup: &kartav1alpha1.PodGroupComponentsMapping{
						Name: "test",
					},
				},
			},
		},
	}

	if len(isDeleted) > 0 && isDeleted[0] {
		now := metav1.NewTime(time.Now())
		karta.SetDeletionTimestamp(&now)
		karta.SetFinalizers([]string{"test-finalizer"})
	}

	return karta
}

func setKartaComponentTypeKeyPath(t *testing.T, kt *kartav1alpha1.Karta, keyPath string) {
	t.Helper()
	assert.NotEmpty(t, kt.Spec.StructureDefinition.ChildComponents)
	kt.Spec.StructureDefinition.ChildComponents[0].PodSelector.ComponentTypeSelector.KeyPath = keyPath
}

func stringPtr(value string) *string {
	return &value
}

func getPyFlowPod(role string) *v1.Pod {
	return &v1.Pod{
		TypeMeta: metav1.TypeMeta{Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pyflow-pod",
			Namespace: "test-ns",
			Labels: map[string]string{
				queueLabelKey: "test-queue",
				"role":        role,
				"job-name":    "pyflow-example",
			},
			UID: "100",
		},
	}
}

func getPyFlowKarta() *kartav1alpha1.Karta {
	return &kartav1alpha1.Karta{
		ObjectMeta: metav1.ObjectMeta{Name: "pyflow"},
		Spec: kartav1alpha1.KartaSpec{
			StructureDefinition: kartav1alpha1.StructureDefinition{
				RootComponent: kartav1alpha1.ComponentDefinition{
					Name: "pyflow",
					Kind: &kartav1alpha1.GroupVersionKind{
						Group:   "jobs.example.com",
						Version: "v1",
						Kind:    "PyFlow",
					},
				},
				ChildComponents: []kartav1alpha1.ComponentDefinition{
					{
						Name:     "master",
						OwnerRef: ptr.To("pyflow"),
						SpecDefinition: &kartav1alpha1.SpecDefinition{
							PodTemplateSpecPath: ptr.To(".spec.master.template"),
						},
						ScaleDefinition: &kartav1alpha1.ScaleDefinition{
							ReplicasPath: ptr.To(".spec.master.replicas"),
						},
						PodSelector: &kartav1alpha1.PodSelector{
							ComponentTypeSelector: &kartav1alpha1.ComponentTypeSelector{
								KeyPath: ".metadata.labels.role",
								Value:   ptr.To("master"),
							},
						},
					},
					{
						Name:     "worker",
						OwnerRef: ptr.To("pyflow"),
						SpecDefinition: &kartav1alpha1.SpecDefinition{
							PodTemplateSpecPath: ptr.To(".spec.worker.template"),
						},
						ScaleDefinition: &kartav1alpha1.ScaleDefinition{
							ReplicasPath: ptr.To(".spec.worker.replicas"),
						},
						PodSelector: &kartav1alpha1.PodSelector{
							ComponentTypeSelector: &kartav1alpha1.ComponentTypeSelector{
								KeyPath: ".metadata.labels.role",
								Value:   ptr.To("worker"),
							},
						},
					},
				},
			},
		},
	}
}

func getPyFlowObject() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "jobs.example.com/v1",
		"kind":       "PyFlow",
		"metadata": map[string]any{
			"name": "pyflow-example",
			"uid":  "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		},
		"spec": map[string]any{
			"master": map[string]any{
				"replicas": 1,
			},
			"worker": map[string]any{
				"replicas": 2,
			},
		},
	}}
}

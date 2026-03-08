// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package grove

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestGetPodGroupMetadata(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"podgroups": []interface{}{
					map[string]interface{}{
						"name": "pgs1-pga",
						"podReferences": []interface{}{
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pga1",
							},
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pga2",
							},
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pga3",
							},
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pga4",
							},
						},
						"minReplicas": int64(4),
					},
					map[string]interface{}{
						"name": "pgs1-pgb",
						"podReferences": []interface{}{
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pgb1",
							},
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pgb2",
							},
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pgb3",
							},
						},
						"minReplicas": int64(3),
					},
					map[string]interface{}{
						"name": "pgs1-pgc",
						"podReferences": []interface{}{
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pgc1",
							},
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pgc2",
							},
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pgc3",
							},
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pgc4",
							},
							map[string]interface{}{
								"namespace": "test-ns",
								"name":      "pgs1-pgc5",
							},
						},
						"minReplicas": int64(5),
					},
				},
				"priorityClassName": "inference",
			},
		},
	}

	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pgs1-pga1",
			Namespace: "test-ns",
			Labels: map[string]string{
				queueLabelKey:       "test_queue",
				labelKeyPodGangName: "pgs1",
			},
			UID: "100",
		},
		Spec:   v1.PodSpec{},
		Status: v1.PodStatus{},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.Nil(t, err)
	assert.Equal(t, int32(12), metadata.MinAvailable)
	assert.Equal(t, 3, len(metadata.SubGroups))
	for index, subGroup := range metadata.SubGroups {
		if index == 0 {
			assert.Equal(t, "pgs1-pga", subGroup.Name)
			assert.Equal(t, int32(4), subGroup.MinAvailable)
		} else if index == 1 {
			assert.Equal(t, "pgs1-pgb", subGroup.Name)
			assert.Equal(t, int32(3), subGroup.MinAvailable)
		} else if index == 2 {
			assert.Equal(t, "pgs1-pgc", subGroup.Name)
			assert.Equal(t, int32(5), subGroup.MinAvailable)
		} else {
			t.Fail()
		}
	}
	assert.Equal(t, constants.InferencePriorityClass, metadata.PriorityClassName)
	assert.Equal(t, "test_queue", metadata.Queue)
}

func TestGetPodGroupMetadata_NestedValueErrors(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"priorityClassName": "inference",
				"podgroups":         map[string]interface{}{"x": "1"}, // Not a slice
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	_, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get spec.podgroups from PodGang test-ns/pgs1")
}

func TestParseGroveSubGroup_Success(t *testing.T) {
	input := map[string]interface{}{
		"name":        "mysubgroup",
		"minReplicas": int64(2),
		"podReferences": []interface{}{
			map[string]interface{}{"namespace": "ns", "name": "a"},
			map[string]interface{}{"namespace": "ns", "name": "b"},
		},
	}
	subgroup, err := parseGroveSubGroup(input, 0, "ns", "pg", "")
	assert.NoError(t, err)
	assert.Equal(t, "mysubgroup", subgroup.Name)
	assert.Equal(t, int32(2), subgroup.MinAvailable)
	assert.Equal(t, 2, len(subgroup.PodsReferences))
	assert.Equal(t, "a", subgroup.PodsReferences[0])
	assert.Equal(t, "b", subgroup.PodsReferences[1])
}

func TestParseGroveSubGroup_MissingFields(t *testing.T) {
	// Missing name
	input := map[string]interface{}{
		"minReplicas": int64(1),
		"podReferences": []interface{}{
			map[string]interface{}{"namespace": "ns", "name": "p"},
		},
	}
	_, err := parseGroveSubGroup(input, 0, "ns", "gang", "")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "missing required 'name' field")

	// Missing minReplicas
	input = map[string]interface{}{
		"name": "sg",
		"podReferences": []interface{}{
			map[string]interface{}{"namespace": "ns", "name": "p"},
		},
	}
	_, err = parseGroveSubGroup(input, 0, "ns", "gang", "")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "missing required 'minReplicas' field")

	// Missing podReferences
	input = map[string]interface{}{
		"name":        "sg",
		"minReplicas": int64(1),
	}
	_, err = parseGroveSubGroup(input, 0, "ns", "gang", "")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "missing required 'podReferences' field")
}

func TestParseGroveSubGroup_NegativeMinAvailable(t *testing.T) {
	input := map[string]interface{}{
		"name":        "sg",
		"minReplicas": int64(-1),
		"podReferences": []interface{}{
			map[string]interface{}{"namespace": "ns"},
		},
	}
	_, err := parseGroveSubGroup(input, 1, "ns", "gang", "")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "invalid 'minReplicas' field. Must be greater than 0")
}

func TestParseGroveSubGroup_InvalidPodReference(t *testing.T) {
	input := map[string]interface{}{
		"name":        "sg",
		"minReplicas": int64(1),
		"podReferences": []interface{}{
			"notamap",
		},
	}
	_, err := parseGroveSubGroup(input, 2, "ns", "gg", "")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "invalid spec.podgroup[2].podReferences[0] in PodGang ns/gg")
}

func TestParseGroveSubGroup_CrossNamespaceReferenceRejected(t *testing.T) {
	// This test verifies that cross-namespace pod references are rejected
	// to prevent namespace isolation bypass (security vulnerability)
	input := map[string]interface{}{
		"name":        "sg",
		"minReplicas": int64(1),
		"podReferences": []interface{}{
			map[string]interface{}{"namespace": "other-namespace", "name": "victim-pod"},
		},
	}
	// PodGang is in namespace "attacker-ns" but references pod in "other-namespace"
	_, err := parseGroveSubGroup(input, 0, "attacker-ns", "malicious-gang", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cross-namespace pod reference not allowed")
}

func TestParsePodReference_Success(t *testing.T) {
	ref := map[string]interface{}{
		"namespace": "ns1",
		"name":      "mypod",
	}
	nn, err := parsePodReference(ref)
	assert.NoError(t, err)
	assert.Equal(t, &types.NamespacedName{Namespace: "ns1", Name: "mypod"}, nn)
}

func TestParseGroveSubGroup_ParsePodReferenceError(t *testing.T) {
	input := map[string]interface{}{
		"name":        "sg",
		"minReplicas": int64(1),
		"podReferences": []interface{}{
			map[string]interface{}{"namespace": "ns"},
		},
	}
	_, err := parseGroveSubGroup(input, 1, "ns", "gang", "")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "failed to parse spec.podgroups[1].podreferences[0] from PodGang ns/gang. Err: missing required 'name' field")
}

func TestParsePodReference_MissingFields(t *testing.T) {
	// Missing namespace
	ref := map[string]interface{}{"name": "pod"}
	_, err := parsePodReference(ref)
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "missing required 'namespace' field")

	// Missing name
	ref = map[string]interface{}{"namespace": "ns"}
	_, err = parsePodReference(ref)
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "missing required 'name' field")
}

func TestGetPodGroupMetadata_WithTopologyHierarchy(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraint": map[string]interface{}{
					"packConstraint": map[string]interface{}{
						"preferred": "rack",
						"required":  "zone",
					},
				},
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{"pg1", "pg2"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
								"required":  "rack",
							},
						},
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name": "pg1",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
								"required":  "",
							},
						},
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
					map[string]interface{}{
						"name": "pg2",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "",
								"required":  "node",
							},
						},
						"minReplicas": int64(3),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod2"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "rack", metadata.PreferredTopologyLevel)
	assert.Equal(t, "zone", metadata.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.Topology)
	assert.Equal(t, 3, len(metadata.SubGroups))

	// Parent SubGroup
	assert.Equal(t, "group1", metadata.SubGroups[0].Name)
	assert.Equal(t, int32(0), metadata.SubGroups[0].MinAvailable)
	assert.Nil(t, metadata.SubGroups[0].Parent)
	assert.NotNil(t, metadata.SubGroups[0].TopologyConstraints)
	assert.Equal(t, "node", metadata.SubGroups[0].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "rack", metadata.SubGroups[0].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[0].TopologyConstraints.Topology)

	// Child SubGroup pg1
	assert.Equal(t, "pg1", metadata.SubGroups[1].Name)
	assert.Equal(t, int32(2), metadata.SubGroups[1].MinAvailable)
	assert.NotNil(t, metadata.SubGroups[1].Parent)
	assert.Equal(t, "group1", *metadata.SubGroups[1].Parent)
	assert.NotNil(t, metadata.SubGroups[1].TopologyConstraints)
	assert.Equal(t, "node", metadata.SubGroups[1].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "", metadata.SubGroups[1].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[1].TopologyConstraints.Topology)

	// Child SubGroup pg2
	assert.Equal(t, "pg2", metadata.SubGroups[2].Name)
	assert.Equal(t, int32(3), metadata.SubGroups[2].MinAvailable)
	assert.NotNil(t, metadata.SubGroups[2].Parent)
	assert.Equal(t, "group1", *metadata.SubGroups[2].Parent)
	assert.NotNil(t, metadata.SubGroups[2].TopologyConstraints)
	assert.Equal(t, "", metadata.SubGroups[2].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "node", metadata.SubGroups[2].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[2].TopologyConstraints.Topology)

	// MinAvailable is sum of children only
	assert.Equal(t, int32(5), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_WithoutTopologyConstraintGroupConfigs(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"podgroups": []interface{}{
					map[string]interface{}{
						"name": "pg1",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
							},
						},
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
					map[string]interface{}{
						"name": "pg2",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"required": "rack",
							},
						},
						"minReplicas": int64(3),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod2"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.Topology)
	assert.Equal(t, 2, len(metadata.SubGroups))

	// Both SubGroups have no parent
	assert.Equal(t, "pg1", metadata.SubGroups[0].Name)
	assert.Nil(t, metadata.SubGroups[0].Parent)
	assert.NotNil(t, metadata.SubGroups[0].TopologyConstraints)
	assert.Equal(t, "node", metadata.SubGroups[0].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[0].TopologyConstraints.Topology)

	assert.Equal(t, "pg2", metadata.SubGroups[1].Name)
	assert.Nil(t, metadata.SubGroups[1].Parent)
	assert.NotNil(t, metadata.SubGroups[1].TopologyConstraints)
	assert.Equal(t, "rack", metadata.SubGroups[1].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[1].TopologyConstraints.Topology)

	assert.Equal(t, int32(5), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_MultipleParentGroups(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{"pg1", "pg2"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "rack",
							},
						},
					},
					map[string]interface{}{
						"name":          "group2",
						"podGroupNames": []interface{}{"pg3"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"required": "zone",
							},
						},
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(1),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
					map[string]interface{}{
						"name":        "pg2",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod2"},
						},
					},
					map[string]interface{}{
						"name":        "pg3",
						"minReplicas": int64(3),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod3"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.Topology)
	assert.Equal(t, 5, len(metadata.SubGroups))

	// Parents first
	assert.Equal(t, "group1", metadata.SubGroups[0].Name)
	assert.Equal(t, int32(0), metadata.SubGroups[0].MinAvailable)
	assert.Equal(t, "group2", metadata.SubGroups[1].Name)
	assert.Equal(t, int32(0), metadata.SubGroups[1].MinAvailable)

	// Children
	assert.Equal(t, "pg1", metadata.SubGroups[2].Name)
	assert.Equal(t, "group1", *metadata.SubGroups[2].Parent)
	assert.Equal(t, "pg2", metadata.SubGroups[3].Name)
	assert.Equal(t, "group1", *metadata.SubGroups[3].Parent)
	assert.Equal(t, "pg3", metadata.SubGroups[4].Name)
	assert.Equal(t, "group2", *metadata.SubGroups[4].Parent)
}

func TestGetPodGroupMetadata_MixedParenting(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{"pg1"},
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
					map[string]interface{}{
						"name":        "pg2",
						"minReplicas": int64(3),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod2"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.Topology)
	assert.Equal(t, 3, len(metadata.SubGroups))

	// Parent
	assert.Equal(t, "group1", metadata.SubGroups[0].Name)

	// Children
	assert.Equal(t, "pg1", metadata.SubGroups[1].Name)
	assert.Equal(t, "group1", *metadata.SubGroups[1].Parent)
	assert.Equal(t, "pg2", metadata.SubGroups[2].Name)
	assert.Nil(t, metadata.SubGroups[2].Parent)
}

func TestGetPodGroupMetadata_EmptyPodGroupNames(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{},
					},
					map[string]interface{}{
						"name":          "group2",
						"podGroupNames": []interface{}{"pg1"},
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.Topology)
	assert.Equal(t, 2, len(metadata.SubGroups))

	// Only group2 appears as parent (group1 skipped due to empty podGroupNames)
	assert.Equal(t, "group2", metadata.SubGroups[0].Name)
	assert.Equal(t, "pg1", metadata.SubGroups[1].Name)
	assert.Equal(t, "group2", *metadata.SubGroups[1].Parent)
}

func TestGetPodGroupMetadata_MissingTopologyConstraints(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{"pg1"},
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.Topology)
	assert.Equal(t, "", metadata.PreferredTopologyLevel)
	assert.Equal(t, "", metadata.RequiredTopologyLevel)
	assert.Nil(t, metadata.SubGroups[0].TopologyConstraints)
	assert.Nil(t, metadata.SubGroups[1].TopologyConstraints)
}

func TestGetPodGroupMetadata_NilTopologyConstraintInConfig(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":               "group1",
						"podGroupNames":      []interface{}{"pg1"},
						"topologyConstraint": nil,
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.Topology)
	assert.Nil(t, metadata.SubGroups[0].TopologyConstraints)
}

func TestGetPodGroupMetadata_NilTopologyConstraintInPodGroup(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":               "pg1",
						"topologyConstraint": nil,
						"minReplicas":        int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.Topology)
	assert.Nil(t, metadata.SubGroups[0].TopologyConstraints)
}

func TestGetPodGroupMetadata_InvalidTopologyConstraintGroupConfigsType(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraintGroupConfigs": map[string]interface{}{
					"invalid": "type",
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	_, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse 'topologyConstraintGroupConfigs' field")
}

func TestGetPodGroupMetadata_ConfigReferencesNonexistentPodGroup(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{"pg1", "pg2"},
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.Topology)
	assert.Equal(t, 2, len(metadata.SubGroups))

	// Parent group created
	assert.Equal(t, "group1", metadata.SubGroups[0].Name)

	// Only pg1 appears (pg2 doesn't exist)
	assert.Equal(t, "pg1", metadata.SubGroups[1].Name)
	assert.Equal(t, "group1", *metadata.SubGroups[1].Parent)
}

func TestGetPodGroupMetadata_PodGangPreferredOnly(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraint": map[string]interface{}{
					"packConstraint": map[string]interface{}{
						"preferred": "rack",
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "rack", metadata.PreferredTopologyLevel)
	assert.Equal(t, "", metadata.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.Topology)
	assert.Equal(t, 1, len(metadata.SubGroups))
}

func TestGetPodGroupMetadata_PodGangRequiredOnly(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraint": map[string]interface{}{
					"packConstraint": map[string]interface{}{
						"required": "zone",
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.PreferredTopologyLevel)
	assert.Equal(t, "zone", metadata.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.Topology)
	assert.Equal(t, 1, len(metadata.SubGroups))
}

func TestGetPodGroupMetadata_ParentGroupBothTopologies(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{"pg1"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "rack",
								"required":  "zone",
							},
						},
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.Topology)
	assert.Equal(t, 2, len(metadata.SubGroups))

	// Parent SubGroup
	assert.Equal(t, "group1", metadata.SubGroups[0].Name)
	assert.NotNil(t, metadata.SubGroups[0].TopologyConstraints)
	assert.Equal(t, "rack", metadata.SubGroups[0].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "zone", metadata.SubGroups[0].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[0].TopologyConstraints.Topology)

	// Child SubGroup
	assert.Equal(t, "pg1", metadata.SubGroups[1].Name)
	assert.Equal(t, "group1", *metadata.SubGroups[1].Parent)
}

func TestGetPodGroupMetadata_PodGroupBothTopologies(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"podgroups": []interface{}{
					map[string]interface{}{
						"name": "pg1",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
								"required":  "rack",
							},
						},
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)
	assert.Equal(t, "", metadata.Topology)
	assert.Equal(t, 1, len(metadata.SubGroups))

	// SubGroup with both topology constraints
	assert.Equal(t, "pg1", metadata.SubGroups[0].Name)
	assert.NotNil(t, metadata.SubGroups[0].TopologyConstraints)
	assert.Equal(t, "node", metadata.SubGroups[0].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "rack", metadata.SubGroups[0].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[0].TopologyConstraints.Topology)
}

func TestGetPodGroupMetadata_ComplexHierarchyMixedTopologies(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				// Level 1: PodGang - Both
				"topologyConstraint": map[string]interface{}{
					"packConstraint": map[string]interface{}{
						"preferred": "cluster",
						"required":  "datacenter",
					},
				},
				// Level 2: Parent groups - Different variants
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{"pg1", "pg2"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "rack",
								"required":  "zone",
							},
						},
					},
					map[string]interface{}{
						"name":          "group2",
						"podGroupNames": []interface{}{"pg3"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
							},
						},
					},
				},
				// Level 3: Child PodGroups - Different variants
				"podgroups": []interface{}{
					map[string]interface{}{
						"name": "pg1",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
								"required":  "rack",
							},
						},
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
					map[string]interface{}{
						"name": "pg2",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"required": "node",
							},
						},
						"minReplicas": int64(3),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod2"},
						},
					},
					map[string]interface{}{
						"name": "pg3",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "socket",
							},
						},
						"minReplicas": int64(1),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod3"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)

	// PodGang level: both preferred and required
	assert.Equal(t, "test-topology", metadata.Topology)
	assert.Equal(t, "cluster", metadata.PreferredTopologyLevel)
	assert.Equal(t, "datacenter", metadata.RequiredTopologyLevel)

	// Total SubGroups: 2 parents + 3 children = 5
	assert.Equal(t, 5, len(metadata.SubGroups))

	// Parents should come first
	// Parent SubGroup group1: both preferred and required
	assert.Equal(t, "group1", metadata.SubGroups[0].Name)
	assert.NotNil(t, metadata.SubGroups[0].TopologyConstraints)
	assert.Equal(t, "rack", metadata.SubGroups[0].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "zone", metadata.SubGroups[0].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[0].TopologyConstraints.Topology)
	assert.Nil(t, metadata.SubGroups[0].Parent)

	// Parent SubGroup group2: preferred-only
	assert.Equal(t, "group2", metadata.SubGroups[1].Name)
	assert.NotNil(t, metadata.SubGroups[1].TopologyConstraints)
	assert.Equal(t, "node", metadata.SubGroups[1].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "", metadata.SubGroups[1].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[1].TopologyConstraints.Topology)
	assert.Nil(t, metadata.SubGroups[1].Parent)

	// Child SubGroup pg1: both, parent=group1
	assert.Equal(t, "pg1", metadata.SubGroups[2].Name)
	assert.NotNil(t, metadata.SubGroups[2].TopologyConstraints)
	assert.Equal(t, "node", metadata.SubGroups[2].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "rack", metadata.SubGroups[2].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[2].TopologyConstraints.Topology)
	assert.NotNil(t, metadata.SubGroups[2].Parent)
	assert.Equal(t, "group1", *metadata.SubGroups[2].Parent)
	assert.Equal(t, int32(2), metadata.SubGroups[2].MinAvailable)

	// Child SubGroup pg2: required-only, parent=group1
	assert.Equal(t, "pg2", metadata.SubGroups[3].Name)
	assert.NotNil(t, metadata.SubGroups[3].TopologyConstraints)
	assert.Equal(t, "", metadata.SubGroups[3].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "node", metadata.SubGroups[3].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[3].TopologyConstraints.Topology)
	assert.NotNil(t, metadata.SubGroups[3].Parent)
	assert.Equal(t, "group1", *metadata.SubGroups[3].Parent)
	assert.Equal(t, int32(3), metadata.SubGroups[3].MinAvailable)

	// Child SubGroup pg3: preferred-only, parent=group2
	assert.Equal(t, "pg3", metadata.SubGroups[4].Name)
	assert.NotNil(t, metadata.SubGroups[4].TopologyConstraints)
	assert.Equal(t, "socket", metadata.SubGroups[4].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "", metadata.SubGroups[4].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[4].TopologyConstraints.Topology)
	assert.NotNil(t, metadata.SubGroups[4].Parent)
	assert.Equal(t, "group2", *metadata.SubGroups[4].Parent)
	assert.Equal(t, int32(1), metadata.SubGroups[4].MinAvailable)

	// MinAvailable = sum of children only (2+3+1 = 6)
	assert.Equal(t, int32(6), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_MultipleParentGroupsVariantMix(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{"pg1"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "rack",
								"required":  "zone",
							},
						},
					},
					map[string]interface{}{
						"name":          "group2",
						"podGroupNames": []interface{}{"pg2"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
							},
						},
					},
					map[string]interface{}{
						"name":          "group3",
						"podGroupNames": []interface{}{"pg3"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"required": "socket",
							},
						},
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(1),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
					map[string]interface{}{
						"name":        "pg2",
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod2"},
						},
					},
					map[string]interface{}{
						"name":        "pg3",
						"minReplicas": int64(3),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod3"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)

	// 6 SubGroups total (3 parents + 3 children)
	assert.Equal(t, "", metadata.Topology)
	assert.Equal(t, 6, len(metadata.SubGroups))

	// Parent SubGroup group1: both preferred and required
	assert.Equal(t, "group1", metadata.SubGroups[0].Name)
	assert.NotNil(t, metadata.SubGroups[0].TopologyConstraints)
	assert.Equal(t, "rack", metadata.SubGroups[0].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "zone", metadata.SubGroups[0].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[0].TopologyConstraints.Topology)

	// Parent SubGroup group2: preferred-only
	assert.Equal(t, "group2", metadata.SubGroups[1].Name)
	assert.NotNil(t, metadata.SubGroups[1].TopologyConstraints)
	assert.Equal(t, "node", metadata.SubGroups[1].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "", metadata.SubGroups[1].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[1].TopologyConstraints.Topology)

	// Parent SubGroup group3: required-only
	assert.Equal(t, "group3", metadata.SubGroups[2].Name)
	assert.NotNil(t, metadata.SubGroups[2].TopologyConstraints)
	assert.Equal(t, "", metadata.SubGroups[2].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "socket", metadata.SubGroups[2].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[2].TopologyConstraints.Topology)

	// Child SubGroup pg1: parent=group1
	assert.Equal(t, "pg1", metadata.SubGroups[3].Name)
	assert.NotNil(t, metadata.SubGroups[3].Parent)
	assert.Equal(t, "group1", *metadata.SubGroups[3].Parent)
	assert.Equal(t, int32(1), metadata.SubGroups[3].MinAvailable)

	// Child SubGroup pg2: parent=group2
	assert.Equal(t, "pg2", metadata.SubGroups[4].Name)
	assert.NotNil(t, metadata.SubGroups[4].Parent)
	assert.Equal(t, "group2", *metadata.SubGroups[4].Parent)
	assert.Equal(t, int32(2), metadata.SubGroups[4].MinAvailable)

	// Child SubGroup pg3: parent=group3
	assert.Equal(t, "pg3", metadata.SubGroups[5].Name)
	assert.NotNil(t, metadata.SubGroups[5].Parent)
	assert.Equal(t, "group3", *metadata.SubGroups[5].Parent)
	assert.Equal(t, int32(3), metadata.SubGroups[5].MinAvailable)

	// MinAvailable = 1+2+3 = 6
	assert.Equal(t, int32(6), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_MultiplePodGroupsVariantMix(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				"podgroups": []interface{}{
					map[string]interface{}{
						"name": "pg1",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "rack",
								"required":  "zone",
							},
						},
						"minReplicas": int64(1),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
					map[string]interface{}{
						"name": "pg2",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
							},
						},
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod2"},
						},
					},
					map[string]interface{}{
						"name": "pg3",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"required": "socket",
							},
						},
						"minReplicas": int64(3),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod3"},
						},
					},
					map[string]interface{}{
						"name":        "pg4",
						"minReplicas": int64(4),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod4"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)

	// 4 SubGroups total (all children, no parents)
	assert.Equal(t, "", metadata.Topology)
	assert.Equal(t, 4, len(metadata.SubGroups))

	// SubGroup pg1: both preferred and required, no parent
	assert.Equal(t, "pg1", metadata.SubGroups[0].Name)
	assert.NotNil(t, metadata.SubGroups[0].TopologyConstraints)
	assert.Equal(t, "rack", metadata.SubGroups[0].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "zone", metadata.SubGroups[0].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[0].TopologyConstraints.Topology)
	assert.Nil(t, metadata.SubGroups[0].Parent)
	assert.Equal(t, int32(1), metadata.SubGroups[0].MinAvailable)

	// SubGroup pg2: preferred-only, no parent
	assert.Equal(t, "pg2", metadata.SubGroups[1].Name)
	assert.NotNil(t, metadata.SubGroups[1].TopologyConstraints)
	assert.Equal(t, "node", metadata.SubGroups[1].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "", metadata.SubGroups[1].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[1].TopologyConstraints.Topology)
	assert.Nil(t, metadata.SubGroups[1].Parent)
	assert.Equal(t, int32(2), metadata.SubGroups[1].MinAvailable)

	// SubGroup pg3: required-only, no parent
	assert.Equal(t, "pg3", metadata.SubGroups[2].Name)
	assert.NotNil(t, metadata.SubGroups[2].TopologyConstraints)
	assert.Equal(t, "", metadata.SubGroups[2].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "socket", metadata.SubGroups[2].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[2].TopologyConstraints.Topology)
	assert.Nil(t, metadata.SubGroups[2].Parent)
	assert.Equal(t, int32(3), metadata.SubGroups[2].MinAvailable)

	// SubGroup pg4: neither (no topology constraint), no parent
	assert.Equal(t, "pg4", metadata.SubGroups[3].Name)
	assert.Nil(t, metadata.SubGroups[3].TopologyConstraints)
	assert.Nil(t, metadata.SubGroups[3].Parent)
	assert.Equal(t, int32(4), metadata.SubGroups[3].MinAvailable)

	// MinAvailable = 1+2+3+4 = 10
	assert.Equal(t, int32(10), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_WithTopologyAnnotation(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "custom-topology",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraint": map[string]interface{}{
					"packConstraint": map[string]interface{}{
						"preferred": "rack",
						"required":  "zone",
					},
				},
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{"pg1"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
							},
						},
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name": "pg1",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"required": "socket",
							},
						},
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)

	// Verify topology annotation value is used at PodGang level
	assert.Equal(t, "rack", metadata.PreferredTopologyLevel)
	assert.Equal(t, "zone", metadata.RequiredTopologyLevel)
	assert.Equal(t, "custom-topology", metadata.Topology)

	// Verify all topology constraints have the topology field set
	assert.Equal(t, 2, len(metadata.SubGroups))

	// Parent group
	assert.Equal(t, "group1", metadata.SubGroups[0].Name)
	assert.NotNil(t, metadata.SubGroups[0].TopologyConstraints)
	assert.Equal(t, "custom-topology", metadata.SubGroups[0].TopologyConstraints.Topology)

	// Child group
	assert.Equal(t, "pg1", metadata.SubGroups[1].Name)
	assert.NotNil(t, metadata.SubGroups[1].TopologyConstraints)
	assert.Equal(t, "custom-topology", metadata.SubGroups[1].TopologyConstraints.Topology)
}

func TestGetPodGroupMetadata_SpecOverridesAnnotation(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name":                     "spec-topology",
					"kai.scheduler/topology":                     "anno-topology",
					"kai.scheduler/topology-preferred-placement": "datacenter",
					"kai.scheduler/topology-required-placement":  "cluster",
				},
			},
			"spec": map[string]interface{}{
				"topologyConstraint": map[string]interface{}{
					"packConstraint": map[string]interface{}{
						"preferred": "rack",
						"required":  "zone",
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name":        "pg1",
						"minReplicas": int64(1),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)

	// Verify spec constraints override annotation constraints
	assert.Equal(t, "rack", metadata.PreferredTopologyLevel, "spec preferred should override annotation")
	assert.Equal(t, "zone", metadata.RequiredTopologyLevel, "spec required should override annotation")

	// Verify grove.io/topology-name takes precedence over kai.scheduler/topology
	assert.Equal(t, "spec-topology", metadata.Topology, "grove.io/topology-name should override kai.scheduler/topology")
}

func TestGetPodGroupMetadata_ThreeLevelTopologyWithLeafVerification(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
				"annotations": map[string]interface{}{
					"grove.io/topology-name": "test-topology",
				},
			},
			"spec": map[string]interface{}{
				// Level 1: PodGang constraints
				"topologyConstraint": map[string]interface{}{
					"packConstraint": map[string]interface{}{
						"preferred": "datacenter",
						"required":  "cluster",
					},
				},
				// Level 2: Parent groups
				"topologyConstraintGroupConfigs": []interface{}{
					map[string]interface{}{
						"name":          "group1",
						"podGroupNames": []interface{}{"pg1", "pg2"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "rack",
								"required":  "zone",
							},
						},
					},
					map[string]interface{}{
						"name":          "group2",
						"podGroupNames": []interface{}{"pg3"},
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
							},
						},
					},
				},
				// Level 3: Child PodGroups (leaves)
				"podgroups": []interface{}{
					map[string]interface{}{
						"name": "pg1",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "node",
								"required":  "rack",
							},
						},
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
					map[string]interface{}{
						"name": "pg2",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"required": "socket",
							},
						},
						"minReplicas": int64(3),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod2"},
						},
					},
					map[string]interface{}{
						"name": "pg3",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "socket",
							},
						},
						"minReplicas": int64(1),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod3"},
						},
					},
					map[string]interface{}{
						"name": "pg4",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"preferred": "rack",
								"required":  "node",
							},
						},
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod4"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	metadata, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.NoError(t, err)

	// Level 1: Verify PodGang level topology
	assert.Equal(t, "datacenter", metadata.PreferredTopologyLevel)
	assert.Equal(t, "cluster", metadata.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.Topology)

	// Total: 2 parents + 4 children = 6 SubGroups
	assert.Equal(t, 6, len(metadata.SubGroups))

	// Level 2: Verify Parent group1
	assert.Equal(t, "group1", metadata.SubGroups[0].Name)
	assert.NotNil(t, metadata.SubGroups[0].TopologyConstraints)
	assert.Equal(t, "rack", metadata.SubGroups[0].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "zone", metadata.SubGroups[0].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[0].TopologyConstraints.Topology)
	assert.Nil(t, metadata.SubGroups[0].Parent)
	assert.Equal(t, int32(0), metadata.SubGroups[0].MinAvailable)

	// Level 2: Verify Parent group2
	assert.Equal(t, "group2", metadata.SubGroups[1].Name)
	assert.NotNil(t, metadata.SubGroups[1].TopologyConstraints)
	assert.Equal(t, "node", metadata.SubGroups[1].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "", metadata.SubGroups[1].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[1].TopologyConstraints.Topology)
	assert.Nil(t, metadata.SubGroups[1].Parent)
	assert.Equal(t, int32(0), metadata.SubGroups[1].MinAvailable)

	// Level 3 (LEAF): Verify Child pg1 under group1
	assert.Equal(t, "pg1", metadata.SubGroups[2].Name)
	assert.NotNil(t, metadata.SubGroups[2].TopologyConstraints)
	assert.Equal(t, "node", metadata.SubGroups[2].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "rack", metadata.SubGroups[2].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[2].TopologyConstraints.Topology, "leaf pg1 should have topology")
	assert.NotNil(t, metadata.SubGroups[2].Parent)
	assert.Equal(t, "group1", *metadata.SubGroups[2].Parent)
	assert.Equal(t, int32(2), metadata.SubGroups[2].MinAvailable)

	// Level 3 (LEAF): Verify Child pg2 under group1
	assert.Equal(t, "pg2", metadata.SubGroups[3].Name)
	assert.NotNil(t, metadata.SubGroups[3].TopologyConstraints)
	assert.Equal(t, "", metadata.SubGroups[3].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "socket", metadata.SubGroups[3].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[3].TopologyConstraints.Topology, "leaf pg2 should have topology")
	assert.NotNil(t, metadata.SubGroups[3].Parent)
	assert.Equal(t, "group1", *metadata.SubGroups[3].Parent)
	assert.Equal(t, int32(3), metadata.SubGroups[3].MinAvailable)

	// Level 3 (LEAF): Verify Child pg3 under group2
	assert.Equal(t, "pg3", metadata.SubGroups[4].Name)
	assert.NotNil(t, metadata.SubGroups[4].TopologyConstraints)
	assert.Equal(t, "socket", metadata.SubGroups[4].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "", metadata.SubGroups[4].TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "test-topology", metadata.SubGroups[4].TopologyConstraints.Topology, "leaf pg3 should have topology")
	assert.NotNil(t, metadata.SubGroups[4].Parent)
	assert.Equal(t, "group2", *metadata.SubGroups[4].Parent)
	assert.Equal(t, int32(1), metadata.SubGroups[4].MinAvailable)

	// Verify pg4 (standalone, not part of any group)
	assert.Equal(t, "pg4", metadata.SubGroups[5].Name)
	assert.Equal(t, int32(2), metadata.SubGroups[5].MinAvailable)
	assert.Nil(t, metadata.SubGroups[5].Parent)
	assert.Equal(t, 1, len(metadata.SubGroups[5].PodsReferences))
	assert.Equal(t, "pod4", metadata.SubGroups[5].PodsReferences[0])

	// Verify pg4 inherits topology annotation
	assert.NotNil(t, metadata.SubGroups[5].TopologyConstraints)
	assert.Equal(t, "test-topology", metadata.SubGroups[5].TopologyConstraints.Topology)
	assert.Equal(t, "rack", metadata.SubGroups[5].TopologyConstraints.PreferredTopologyLevel)
	assert.Equal(t, "node", metadata.SubGroups[5].TopologyConstraints.RequiredTopologyLevel)
	assert.Nil(t, metadata.SubGroups[5].Parent)

	// Verify total MinAvailable = sum of children (2+3+1+2 = 8)
	assert.Equal(t, int32(8), metadata.MinAvailable)
}

func TestGetPodGroupMetadata_EmptyTopology(t *testing.T) {
	podGang := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PodGang",
			"apiVersion": "scheduler.grove.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "pgs1",
				"namespace": "test-ns",
				"uid":       "1",
			},
			"spec": map[string]interface{}{
				"topologyConstraint": map[string]interface{}{
					"packConstraint": map[string]interface{}{
						"preferred": "rack",
					},
				},
				"podgroups": []interface{}{
					map[string]interface{}{
						"name": "pg1",
						"topologyConstraint": map[string]interface{}{
							"packConstraint": map[string]interface{}{
								"required": "node",
							},
						},
						"minReplicas": int64(2),
						"podReferences": []interface{}{
							map[string]interface{}{"namespace": "test-ns", "name": "pod1"},
						},
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelKeyPodGangName: "pgs1",
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(podGang).Build()
	grouper := NewGroveGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	_, err := grouper.GetPodGroupMetadata(nil, pod)
	assert.Error(t, err)
}

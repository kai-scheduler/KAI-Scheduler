// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package grove

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
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
	metadata, err := grouper.GetPodGroupMetadata(podGang, pod)
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
	_, err := grouper.GetPodGroupMetadata(podGang, pod)
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
	subgroup, err := parseGroveSubGroup(input, 0, "ns", "pg")
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
	_, err := parseGroveSubGroup(input, 0, "ns", "gang")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "missing required 'name' field")

	// Missing minReplicas
	input = map[string]interface{}{
		"name": "sg",
		"podReferences": []interface{}{
			map[string]interface{}{"namespace": "ns", "name": "p"},
		},
	}
	_, err = parseGroveSubGroup(input, 0, "ns", "gang")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "missing required 'minReplicas' field")

	// Missing podReferences
	input = map[string]interface{}{
		"name":        "sg",
		"minReplicas": int64(1),
	}
	_, err = parseGroveSubGroup(input, 0, "ns", "gang")
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
	_, err := parseGroveSubGroup(input, 1, "ns", "gang")
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
	_, err := parseGroveSubGroup(input, 2, "ns", "gg")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "invalid spec.podgroup[2].podReferences[0] in PodGang ns/gg")
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
	_, err := parseGroveSubGroup(input, 1, "ns", "gang")
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

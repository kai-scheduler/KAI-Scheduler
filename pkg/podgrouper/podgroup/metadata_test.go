// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestFindSubGroupForPod(t *testing.T) {
	tests := []struct {
		name             string
		metadata         Metadata
		podNamespace     string
		podName          string
		expectedSubGroup *SubGroupMetadata
	}{
		{
			name: "pod found in first subgroup",
			metadata: Metadata{
				Namespace: "ns1",
				SubGroups: []*SubGroupMetadata{
					{
						Name:           "subgroup-1",
						PodsReferences: []string{"pod-a", "pod-b"},
					},
					{
						Name:           "subgroup-2",
						PodsReferences: []string{"pod-c"},
					},
				},
			},
			podNamespace:     "ns1",
			podName:          "pod-a",
			expectedSubGroup: &SubGroupMetadata{Name: "subgroup-1"},
		},
		{
			name: "pod found in second subgroup",
			metadata: Metadata{
				Namespace: "ns1",
				SubGroups: []*SubGroupMetadata{
					{
						Name:           "subgroup-1",
						PodsReferences: []string{"pod-a"},
					},
					{
						Name:           "subgroup-2",
						PodsReferences: []string{"pod-c"},
					},
				},
			},
			podNamespace:     "ns1",
			podName:          "pod-c",
			expectedSubGroup: &SubGroupMetadata{Name: "subgroup-2"},
		},
		{
			name: "pod not found - different namespace",
			metadata: Metadata{
				Namespace: "ns1",
				SubGroups: []*SubGroupMetadata{
					{
						Name:           "subgroup-1",
						PodsReferences: []string{"pod-a"},
					},
				},
			},
			podNamespace:     "ns2",
			podName:          "pod-a",
			expectedSubGroup: nil,
		},
		{
			name: "pod not found - different name",
			metadata: Metadata{
				Namespace: "ns1",
				SubGroups: []*SubGroupMetadata{
					{
						Name:           "subgroup-1",
						PodsReferences: []string{"pod-a"},
					},
				},
			},
			podNamespace:     "ns1",
			podName:          "pod-b",
			expectedSubGroup: nil,
		},
		{
			name: "empty subgroups",
			metadata: Metadata{
				SubGroups: []*SubGroupMetadata{},
			},
			podNamespace:     "ns1",
			podName:          "pod-a",
			expectedSubGroup: nil,
		},
		{
			name: "nil subgroups",
			metadata: Metadata{
				SubGroups: nil,
			},
			podNamespace:     "ns1",
			podName:          "pod-a",
			expectedSubGroup: nil,
		},
		{
			name: "subgroup with empty pod references",
			metadata: Metadata{
				Namespace: "ns1",
				SubGroups: []*SubGroupMetadata{
					{
						Name:           "subgroup-1",
						PodsReferences: []string{},
					},
				},
			},
			podNamespace:     "ns1",
			podName:          "pod-a",
			expectedSubGroup: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.metadata.FindSubGroupForPod(tt.podNamespace, tt.podName)
			if tt.expectedSubGroup == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedSubGroup.Name, result.Name)
			}
		})
	}
}

func TestMetadataDeepCopyNoAliasing(t *testing.T) {
	src := &Metadata{}
	populateReferenceFields(reflect.ValueOf(src).Elem())

	dst := src.DeepCopy()
	require.NotSame(t, src, dst)
	require.Equal(t, src, dst, "DeepCopy must preserve values")

	assertNoAliasing(t, "Metadata", reflect.ValueOf(src).Elem(), reflect.ValueOf(dst).Elem())
}

func TestMetadataDeepCopyMutationIndependence(t *testing.T) {
	parent := "parent-group"
	src := &Metadata{
		Annotations: map[string]string{"a": "1"},
		Labels:      map[string]string{"l": "1"},
		Owner: metav1.OwnerReference{
			Name:       "owner",
			Controller: ptr.To(true),
		},
		SubGroups: []*SubGroupMetadata{{
			Name:                "sg",
			Parent:              &parent,
			PodsReferences:      []string{"p1"},
			TopologyConstraints: &TopologyConstraintMetadata{Topology: "rack"},
		}},
	}

	dst := src.DeepCopy()

	src.Annotations["a"] = "mutated"
	src.Labels["l"] = "mutated"
	*src.Owner.Controller = false
	src.SubGroups[0].Name = "mutated"
	*src.SubGroups[0].Parent = "mutated"
	src.SubGroups[0].PodsReferences[0] = "mutated"
	src.SubGroups[0].TopologyConstraints.Topology = "mutated"

	assert.Equal(t, "1", dst.Annotations["a"])
	assert.Equal(t, "1", dst.Labels["l"])
	assert.True(t, *dst.Owner.Controller)
	assert.Equal(t, "sg", dst.SubGroups[0].Name)
	assert.Equal(t, "parent-group", *dst.SubGroups[0].Parent)
	assert.Equal(t, "p1", dst.SubGroups[0].PodsReferences[0])
	assert.Equal(t, "rack", dst.SubGroups[0].TopologyConstraints.Topology)
}

func TestMetadataDeepCopyNil(t *testing.T) {
	var m *Metadata
	assert.Nil(t, m.DeepCopy())

	var sg *SubGroupMetadata
	assert.Nil(t, sg.DeepCopy())

	var tc *TopologyConstraintMetadata
	assert.Nil(t, tc.DeepCopy())
}

func populateReferenceFields(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if !f.CanSet() {
				continue
			}
			populateReferenceFields(f)
		}
	case reflect.Ptr:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		populateReferenceFields(v.Elem())
	case reflect.Map:
		m := reflect.MakeMapWithSize(v.Type(), 1)
		key := reflect.New(v.Type().Key()).Elem()
		val := reflect.New(v.Type().Elem()).Elem()
		populateReferenceFields(key)
		populateReferenceFields(val)
		m.SetMapIndex(key, val)
		v.Set(m)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		populateReferenceFields(s.Index(0))
		v.Set(s)
	case reflect.String:
		if v.String() == "" {
			v.SetString("x")
		}
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() == 0 {
			v.SetInt(1)
		}
	}
}

// Skips nil/empty references — Go does not guarantee distinct backing memory for those.
func assertNoAliasing(t *testing.T, path string, src, dst reflect.Value) {
	t.Helper()
	switch src.Kind() {
	case reflect.Struct:
		for i := 0; i < src.NumField(); i++ {
			assertNoAliasing(t,
				path+"."+src.Type().Field(i).Name,
				src.Field(i), dst.Field(i))
		}
	case reflect.Ptr:
		if src.IsNil() {
			return
		}
		require.NotEqual(t, src.Pointer(), dst.Pointer(),
			"%s: pointer aliased between src and dst", path)
		assertNoAliasing(t, path+".*", src.Elem(), dst.Elem())
	case reflect.Map:
		if src.IsNil() {
			return
		}
		require.NotEqual(t, src.Pointer(), dst.Pointer(),
			"%s: map aliased between src and dst", path)
	case reflect.Slice:
		if src.IsNil() || src.Len() == 0 {
			return
		}
		require.NotEqual(t, src.Pointer(), dst.Pointer(),
			"%s: slice aliased between src and dst", path)
		for i := 0; i < src.Len(); i++ {
			assertNoAliasing(t, fmt.Sprintf("%s[%d]", path, i),
				src.Index(i), dst.Index(i))
		}
	}
}

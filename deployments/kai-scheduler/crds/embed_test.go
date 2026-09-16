// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package crds

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEmbeddedCRDsUnstructured(t *testing.T) {
	typed, err := LoadEmbeddedCRDs()
	require.NoError(t, err)
	require.NotEmpty(t, typed)

	objects, err := LoadEmbeddedCRDsUnstructured()
	require.NoError(t, err)
	require.Len(t, objects, len(typed))

	for i, obj := range objects {
		assert.Equal(t, "CustomResourceDefinition", obj.GetKind())
		assert.Equal(t, typed[i].Name, obj.GetName())
		group, found, err := unstructuredString(obj.Object, "spec", "group")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, typed[i].Spec.Group, group)
	}
}

func unstructuredString(obj map[string]interface{}, fields ...string) (string, bool, error) {
	current := interface{}(obj)
	for _, field := range fields {
		m, ok := current.(map[string]interface{})
		if !ok {
			return "", false, nil
		}
		current, ok = m[field]
		if !ok {
			return "", false, nil
		}
	}
	s, ok := current.(string)
	return s, ok, nil
}

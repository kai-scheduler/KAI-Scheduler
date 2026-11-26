// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package statefulset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/api/podgrouper/constants"
)

func TestGetPodGroupMetadata_MinMember(t *testing.T) {
	tests := []struct {
		name          string
		minMember     string
		policy        string
		specReplicas  int64
		statusReplica int64
		expected      int32
		expectedError bool
	}{
		{name: "no annotation", policy: "Parallel", specReplicas: 4, expected: 1},
		{name: "parallel uses annotation", minMember: "3", policy: "Parallel", specReplicas: 4, expected: 3},
		{name: "parallel does not clamp to spec replicas", minMember: "5", policy: "Parallel", specReplicas: 4, statusReplica: 2, expected: 5},
		{name: "ordered ready clamps to status replicas", minMember: "3", policy: "OrderedReady", specReplicas: 4, statusReplica: 2, expected: 2},
		{name: "default policy is ordered ready", minMember: "3", specReplicas: 4, statusReplica: 1, expected: 1},
		{name: "ordered ready with no pods yet", minMember: "3", policy: "OrderedReady", specReplicas: 4, expected: 1},
		{name: "invalid annotation", minMember: "0", policy: "Parallel", specReplicas: 4, expectedError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			annotations := map[string]any{}
			if test.minMember != "" {
				annotations[constants.MinMemberOverrideKey] = test.minMember
			}
			spec := map[string]any{"replicas": test.specReplicas}
			if test.policy != "" {
				spec["podManagementPolicy"] = test.policy
			}
			sts := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "StatefulSet",
				"metadata": map[string]any{
					"name": "sts", "namespace": "ns", "uid": "1", "annotations": annotations,
				},
				"spec":   spec,
				"status": map[string]any{"replicas": test.statusReplica},
			}}
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sts-0", Namespace: "ns"}}

			grouper := NewStatefulSetGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
			metadata, err := grouper.GetPodGroupMetadata(sts, pod)
			if test.expectedError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, test.expected, metadata.MinAvailable)
		})
	}
}

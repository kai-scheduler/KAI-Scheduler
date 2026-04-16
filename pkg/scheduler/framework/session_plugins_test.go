/*
Copyright 2023 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

func TestMutateBindRequestAnnotations(t *testing.T) {
	tests := []struct {
		name                string
		mutateFns           []api.BindRequestMutateFn
		expectedAnnotations map[string]string
	}{
		{
			name:                "no mutate functions",
			mutateFns:           []api.BindRequestMutateFn{},
			expectedAnnotations: map[string]string{},
		},
		{
			name: "single mutate function",
			mutateFns: []api.BindRequestMutateFn{
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key1": "value1"}
				},
			},
			expectedAnnotations: map[string]string{"key1": "value1"},
		},
		{
			name: "multiple mutate functions with different keys",
			mutateFns: []api.BindRequestMutateFn{
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key1": "value1"}
				},
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key2": "value2"}
				},
			},
			expectedAnnotations: map[string]string{"key1": "value1", "key2": "value2"},
		},
		{
			name: "multiple mutate functions with overlapping keys - later should override",
			mutateFns: []api.BindRequestMutateFn{
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key1": "value1", "common": "first"}
				},
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key2": "value2", "common": "second"}
				},
			},
			expectedAnnotations: map[string]string{"key1": "value1", "key2": "value2", "common": "second"},
		},
		{
			name: "mutate function returns nil map",
			mutateFns: []api.BindRequestMutateFn{
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return map[string]string{"key1": "value1"}
				},
				func(pod *pod_info.PodInfo, nodeName string) map[string]string {
					return nil
				},
			},
			expectedAnnotations: map[string]string{"key1": "value1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := &Session{
				BindRequestMutateFns: tt.mutateFns,
			}
			pod := &pod_info.PodInfo{
				Name: "test-pod",
			}
			nodeName := "test-node"
			annotations := ssn.MutateBindRequestAnnotations(pod, nodeName)
			assert.Equal(t, tt.expectedAnnotations, annotations)
		})
	}
}

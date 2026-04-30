/*
Copyright 2023 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package framework

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
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

func TestVictimInvariantPrePredicateFailure(t *testing.T) {
	task := &pod_info.PodInfo{Name: "task-1"}
	expectedErr := errors.New("missing pvc")

	t.Run("returns nil when no functions are registered", func(t *testing.T) {
		ssn := &Session{}
		assert.Nil(t, ssn.VictimInvariantPrePredicateFailure(task))
	})

	t.Run("returns the first non-nil failure", func(t *testing.T) {
		ssn := &Session{}
		secondCalled := false
		ssn.AddVictimInvariantPrePredicateFn(func(_ *pod_info.PodInfo) *api.VictimInvariantPrePredicateFailure {
			return nil
		})
		ssn.AddVictimInvariantPrePredicateFn(func(gotTask *pod_info.PodInfo) *api.VictimInvariantPrePredicateFailure {
			assert.Same(t, task, gotTask)
			return &api.VictimInvariantPrePredicateFailure{
				Err: expectedErr,
			}
		})
		ssn.AddVictimInvariantPrePredicateFn(func(_ *pod_info.PodInfo) *api.VictimInvariantPrePredicateFailure {
			secondCalled = true
			return &api.VictimInvariantPrePredicateFailure{
				Err: errors.New("should not be returned"),
			}
		})

		failure := ssn.VictimInvariantPrePredicateFailure(task)
		if assert.NotNil(t, failure) {
			assert.Same(t, expectedErr, failure.Err)
		}
		assert.False(t, secondCalled)
	})
}

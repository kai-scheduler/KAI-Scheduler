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

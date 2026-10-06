// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestGeneratorsForJobOrdersBuiltInsByJobType(t *testing.T) {
	tests := []struct {
		name         string
		pods         int
		oneAllocated bool
		registration []string
		want         []string
	}{
		{
			name:         "multi-pod job prefers multi-node",
			pods:         2,
			registration: []string{"custom-first", constants.GeneratorNodeLocalGreedy, "custom-middle", constants.GeneratorMultiNodeGang, "custom-last"},
			want:         []string{"custom-first", constants.GeneratorMultiNodeGang, "custom-middle", constants.GeneratorNodeLocalGreedy, "custom-last"},
		},
		{
			name:         "multi-pod job with one pending pod",
			pods:         2,
			oneAllocated: true,
			registration: []string{constants.GeneratorNodeLocalGreedy, constants.GeneratorMultiNodeGang},
			want:         []string{constants.GeneratorMultiNodeGang, constants.GeneratorNodeLocalGreedy},
		},
		{
			name:         "single-pod job prefers node-local",
			pods:         1,
			registration: []string{constants.GeneratorMultiNodeGang, constants.GeneratorNodeLocalGreedy},
			want:         []string{constants.GeneratorNodeLocalGreedy, constants.GeneratorMultiNodeGang},
		},
		{
			name:         "already preferred order",
			pods:         2,
			registration: []string{constants.GeneratorMultiNodeGang, constants.GeneratorNodeLocalGreedy},
			want:         []string{constants.GeneratorMultiNodeGang, constants.GeneratorNodeLocalGreedy},
		},
		{
			name:         "missing built-in",
			pods:         2,
			registration: []string{"custom", constants.GeneratorNodeLocalGreedy},
			want:         []string{"custom", constants.GeneratorNodeLocalGreedy},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job, tasks := createJobWithTasks(test.pods, 10, "team-pending", v1.PodPending, []v1.ResourceRequirements{requireOneGPU()})
			if test.oneAllocated {
				require.NoError(t, job.UpdateTaskStatus(tasks[0], pod_status.Running))
				require.Equal(t, 1, job.GetNumPendingTasks())
			}
			registrations := make([]framework.ScenarioGeneratorRegistration, len(test.registration))
			for index, name := range test.registration {
				registrations[index].Name = name
			}

			ordered := generatorsForJob(registrations, job)

			require.Equal(t, test.want, generatorNames(ordered))
			require.Equal(t, test.registration, generatorNames(registrations))
		})
	}
}

func generatorNames(registrations []framework.ScenarioGeneratorRegistration) []string {
	names := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		names = append(names, registration.Name)
	}
	return names
}

func TestSearchMaxSolvableKSkipsFullProbe(t *testing.T) {
	probes := []int{}

	maxSolvedK, result := searchMaxSolvableK(4, func(k int) *SearchResult {
		probes = append(probes, k)
		return solvedSearchResult(&solutionResult{solved: true}, false)
	})

	require.Equal(t, 2, maxSolvedK)
	require.Nil(t, result)
	require.Equal(t, []int{1, 2}, probes)
}

func TestSearchMaxSolvableKSkipsSingleTaskFullProbe(t *testing.T) {
	probes := []int{}

	maxSolvedK, result := searchMaxSolvableK(1, func(k int) *SearchResult {
		probes = append(probes, k)
		return solvedSearchResult(&solutionResult{solved: true}, false)
	})

	require.Equal(t, 0, maxSolvedK)
	require.Nil(t, result)
	require.Empty(t, probes)
}

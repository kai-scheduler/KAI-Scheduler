// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/api/constants"
)

func TestGeneratorsForJobUsesJobShapeForDefaultPortfolio(t *testing.T) {
	tests := []struct {
		name          string
		pods          int
		oneAllocated  bool
		registrations []string
		want          []string
	}{
		{
			name:          "multi-pod job prefers multi-node",
			pods:          2,
			registrations: []string{constants.GeneratorNodeLocalGreedy, constants.GeneratorMultiNodeGang},
			want:          []string{constants.GeneratorMultiNodeGang, constants.GeneratorNodeLocalGreedy},
		},
		{
			name:          "multi-pod job with one pending pod",
			pods:          2,
			oneAllocated:  true,
			registrations: []string{constants.GeneratorNodeLocalGreedy, constants.GeneratorMultiNodeGang},
			want:          []string{constants.GeneratorMultiNodeGang, constants.GeneratorNodeLocalGreedy},
		},
		{
			name:          "single-pod job prefers node-local",
			pods:          1,
			registrations: []string{constants.GeneratorNodeLocalGreedy, constants.GeneratorMultiNodeGang},
			want:          []string{constants.GeneratorNodeLocalGreedy, constants.GeneratorMultiNodeGang},
		},
		{
			name:          "admin-reversed portfolio for multi-pod job",
			pods:          2,
			registrations: []string{constants.GeneratorMultiNodeGang, constants.GeneratorNodeLocalGreedy},
			want:          []string{constants.GeneratorMultiNodeGang, constants.GeneratorNodeLocalGreedy},
		},
		{
			name:          "admin-reversed portfolio for single-pod job",
			pods:          1,
			registrations: []string{constants.GeneratorMultiNodeGang, constants.GeneratorNodeLocalGreedy},
			want:          []string{constants.GeneratorMultiNodeGang, constants.GeneratorNodeLocalGreedy},
		},
		{
			name:          "custom portfolio",
			pods:          2,
			registrations: []string{"custom-first", constants.GeneratorNodeLocalGreedy, "custom-middle", constants.GeneratorMultiNodeGang},
			want:          []string{"custom-first", constants.GeneratorNodeLocalGreedy, "custom-middle", constants.GeneratorMultiNodeGang},
		},
		{
			name:          "missing built-in",
			pods:          2,
			registrations: []string{"custom", constants.GeneratorNodeLocalGreedy},
			want:          []string{"custom", constants.GeneratorNodeLocalGreedy},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job, tasks := createJobWithTasks(test.pods, 10, "team-pending", v1.PodPending, []v1.ResourceRequirements{requireOneGPU()})
			if test.oneAllocated {
				require.NoError(t, job.UpdateTaskStatus(tasks[0], pod_status.Running))
				require.Equal(t, 1, job.GetNumPendingTasks())
			}
			registrations := make([]framework.ScenarioGeneratorRegistration, len(test.registrations))
			factoryCalls := []string{}
			for index, name := range test.registrations {
				registrations[index] = framework.ScenarioGeneratorRegistration{
					Name: name,
					Factory: func(framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
						factoryCalls = append(factoryCalls, name)
						return nil
					},
				}
			}

			ordered := generatorsForJob(registrations, job)

			require.Equal(t, test.want, generatorNames(ordered))
			require.Equal(t, test.registrations, generatorNames(registrations))
			for _, registration := range ordered {
				registration.Factory(nil)
			}
			require.Equal(t, test.want, factoryCalls)
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

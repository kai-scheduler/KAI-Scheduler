// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFittingNodeMustPipeline(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		isSimulation         bool
		expectedMustPipeline bool
	}{
		{name: "simulation", isSimulation: true, expectedMustPipeline: true},
		{name: "real allocation that fits now", isSimulation: false, expectedMustPipeline: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ssn, _, task := buildDetailedFitErrorSession(t)
			fits, mustPipeline := ssn.FittingNode(task, ssn.ClusterInfo.Nodes["node-a"], tt.isSimulation)
			assert.True(t, fits)
			assert.Equal(t, tt.expectedMustPipeline, mustPipeline)
		})
	}
}

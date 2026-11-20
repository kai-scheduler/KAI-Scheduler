// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package timeaware

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/api/constants"
)

func TestTimeAwareSimulationSetDefaults(t *testing.T) {
	simulation := TimeAwareSimulation{
		Nodes: []TestNodes{{GPUs: 8}, {GPUs: 4, Count: 2}},
	}

	simulation.SetDefaults()

	assert.Equal(t, ptr.To(0), simulation.HalfLifePeriod)
	assert.Equal(t, []TestNodes{{GPUs: 8, Count: 1}, {GPUs: 4, Count: 2}}, simulation.Nodes)
}

func TestCreateNodes(t *testing.T) {
	ctx := context.Background()
	ctrlClient := fake.NewClientBuilder().Build()

	err := createNodes(ctx, ctrlClient, []TestNodes{{GPUs: 4, Count: 2}, {GPUs: 4, Count: 1}}, "simulation-a")
	require.NoError(t, err)

	var nodes corev1.NodeList
	require.NoError(t, ctrlClient.List(ctx, &nodes, client.MatchingLabels{"simulation": "simulation-a"}))
	require.Len(t, nodes.Items, 3)
	for _, node := range nodes.Items {
		gpus := node.Status.Allocatable[constants.NvidiaGpuResource]
		assert.Equal(t, int64(4), gpus.Value(), "node %s", node.Name)
	}
}

// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactTaintsTolerationsAndNumericSelectors(t *testing.T) {
	hasher := NewHasher([]byte("synthetic-test-key"))
	body := redactJSON(t, hasher, map[string]any{
		"nodes": []any{map[string]any{
			"metadata": map[string]any{"labels": map[string]any{
				"example.com/generation":      "5",
				"example.com/pool":            "blue",
				"topology.kubernetes.io/zone": "zone-a",
			}},
			"spec": map[string]any{"taints": []any{
				map[string]any{"key": "team", "value": "blue", "effect": "NoSchedule"},
				map[string]any{"key": "score", "value": "9", "effect": "NoSchedule"},
			}},
		}},
		"pods": []any{map[string]any{"spec": map[string]any{
			"tolerations": []any{
				map[string]any{"key": "team", "operator": "Equal", "value": "blue", "effect": "NoSchedule"},
				map[string]any{"key": "score", "operator": "Gt", "value": "5", "effect": "NoSchedule"},
			},
			"affinity": map[string]any{
				"nodeAffinity": map[string]any{
					"requiredDuringSchedulingIgnoredDuringExecution": map[string]any{
						"nodeSelectorTerms": []any{map[string]any{"matchExpressions": []any{
							map[string]any{"key": "example.com/pool", "operator": "In", "values": []any{"blue"}},
							map[string]any{"key": "example.com/generation", "operator": "Gt", "values": []any{"2"}},
						}}},
					},
				},
				"podAffinity": map[string]any{
					"requiredDuringSchedulingIgnoredDuringExecution": []any{map[string]any{
						"topologyKey": "example.com/segment",
						"namespaces":  []any{"team-a"},
					}},
				},
			},
		}}},
		"topologies": []any{map[string]any{"spec": map[string]any{"levels": []any{
			map[string]any{"nodeLabel": "topology.kubernetes.io/zone"},
			map[string]any{"nodeLabel": "example.com/rack", "alias": "rack"},
		}}}},
		"podGroups": []any{map[string]any{"spec": map[string]any{"topologyConstraint": map[string]any{
			"requiredTopologyLevel": "rack",
			"topology":              "dc",
		}}}},
		"pending": []any{map[string]any{
			"reasonCodes": []any{"NonPreemptibleOverQuota", "node(s) didn't have enough resources: GPUs", "team-a/pod"},
		}},
	})

	require.NotContains(t, body, "blue")
	require.NotContains(t, body, "team-a")
	require.NotContains(t, body, "example.com/rack")
	require.NotContains(t, body, "example.com/segment")
	require.NotContains(t, body, `"alias":"rack"`)
	require.Contains(t, body, `"value":"5"`)
	require.Contains(t, body, `"values":["2"]`)
	require.Contains(t, body, "topology.kubernetes.io/zone")
	require.Contains(t, body, "zone-a")
	require.Contains(t, body, hasher.LabelValue("team", "blue"))
	require.Contains(t, body, hasher.LabelKey("example.com/rack"))
	require.Contains(t, body, hasher.LabelKey("rack"))
	require.Contains(t, body, hasher.LabelKey("example.com/segment"))
	require.Contains(t, body, hasher.Hash("team-a"))
	require.Contains(t, body, hasher.Hash("dc"))
	require.Contains(t, body, "NonPreemptibleOverQuota")
	require.Contains(t, body, "node(s) didn't have enough resources: GPUs")
	require.NotContains(t, body, "team-a/pod")
	require.Contains(t, body, hasher.LabelValue("example.com/pool", "blue"))
	require.Contains(t, body, `"5"`)
}

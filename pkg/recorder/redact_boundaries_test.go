// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactEnvelopeRejectsBadInput(t *testing.T) {
	require.NoError(t, RedactEnvelope(nil, NewHasher([]byte("key"))))
	require.NoError(t, RedactEnvelope(&Envelope{}, NewHasher([]byte("key"))))
	err := RedactEnvelope(&Envelope{Record: []byte("{")}, NewHasher([]byte("key")))
	require.Error(t, err)
}

func TestEmptyKeyFailsClosed(t *testing.T) {
	body := redactJSON(t, NewHasher(nil), map[string]any{
		"metadata": map[string]any{"name": "team-a", "namespace": "prod"},
	})
	require.NotContains(t, body, "team-a")
	require.NotContains(t, body, "prod")
	require.Contains(t, body, `"name":""`)
}

func TestUsageQueueKeysAndTimestamps(t *testing.T) {
	hasher := NewHasher([]byte("key"))
	const started = "2024-06-01T11:50:00Z"
	body := redactJSON(t, hasher, map[string]any{
		"usage": map[string]any{"queues": map[string]any{
			"team-a": map[string]any{"cpu": 1.5, "nvidia.com/gpu": 0},
		}},
		"metadata": map[string]any{"annotations": map[string]any{
			"kai.scheduler/last-start-timestamp":     started,
			"kai.scheduler/stale-podgroup-timestamp": started,
			"kai.scheduler/last-eviction-timestamp":  started,
			"example.com/note":                       "team-a",
			"pod-group-name":                         "job-a",
		}},
	})
	require.NotContains(t, body, "team-a")
	require.NotContains(t, body, "job-a")
	require.Contains(t, body, hasher.Hash("team-a"))
	require.Contains(t, body, started)
	require.Contains(t, body, `"cpu":1.5`)
	require.Contains(t, body, hasher.Hash("job-a"))
}

func TestSecretsReasonsAndPluginArgs(t *testing.T) {
	hasher := NewHasher([]byte("key"))
	body := redactJSON(t, hasher, map[string]any{
		"env": []any{
			map[string]any{"name": "TOKEN", "value": "literal-secret"},
			map[string]any{"name": "COUNT", "value": 2},
			map[string]any{"name": "CFG", "valueFrom": map[string]any{
				"secretKeyRef":    map[string]any{"name": "team-a", "key": "token"},
				"secretRef":       map[string]any{"name": "team-a"},
				"configMapKeyRef": map[string]any{"name": "app-config", "key": "data"},
			}},
		},
		"status": map[string]any{
			"reason":  "pod team-a did not fit",
			"reason2": "InsufficientResources",
		},
		"plugins": []any{map[string]any{"arguments": map[string]any{
			"clusterName": "prod",
			"address":     "127.0.0.1:9095",
			"password":    "literal-secret",
		}}},
		"schedulerParams": map[string]any{
			"NodePoolLabelKey":   "kai.scheduler/node-pool",
			"NodePoolLabelValue": "pool-a",
		},
	})
	require.NotContains(t, body, "literal-secret")
	require.NotContains(t, body, "secretKeyRef")
	require.NotContains(t, body, "did not fit")
	require.NotContains(t, body, "app-config")
	require.NotContains(t, body, "pool-a")
	require.Contains(t, body, "InsufficientResources")
	require.Contains(t, body, `"value":2`)
	require.Contains(t, body, "127.0.0.1:9095")
	require.Contains(t, body, hasher.Hash("prod"))
	require.Contains(t, body, hasher.Hash("app-config"))
	require.Contains(t, body, hasher.LabelValue("kai.scheduler/node-pool", "pool-a"))
}

func TestNestedSelectorIsNotFlattened(t *testing.T) {
	hasher := NewHasher([]byte("key"))
	body := redactJSON(t, hasher, map[string]any{
		"nodeSelector": map[string]any{
			"nodeSelectorTerms": []any{map[string]any{
				"matchExpressions": []any{map[string]any{
					"key": "kubernetes.io/hostname", "operator": "In", "values": []any{"node-a"},
				}},
			}},
		},
		"queues":     []any{map[string]any{"metadata": map[string]any{"name": "team-a"}}},
		"name":       7,
		"attributes": map[string]any{"product": map[string]any{"string": "Tesla-K80"}},
	})
	require.Contains(t, body, `"operator":"In"`)
	require.Contains(t, body, "Tesla-K80")
	require.Contains(t, body, `"name":7`)
	require.NotContains(t, body, "node-a")
	require.NotContains(t, body, "team-a")
	require.Contains(t, body, hasher.Hash("node-a"))
	require.Contains(t, body, hasher.Hash("team-a"))
}

func TestEvictionPreemptorAndPluginNames(t *testing.T) {
	hasher := NewHasher([]byte("key"))
	const namespace = "kai-kai-rec-leaf"
	body := redactJSON(t, hasher, map[string]any{
		"decisions": []any{map[string]any{"ops": []any{map[string]any{
			"namespace": namespace,
			"name":      "preemptor",
			"eviction": map[string]any{"Preemptor": map[string]any{
				"Namespace": namespace,
				"Name":      "preemptor",
			}},
		}}}},
		"config": map[string]any{"tiers": []any{map[string]any{"plugins": []any{
			map[string]any{"name": "predicates"},
			map[string]any{"name": "proportion"},
		}}}},
	})
	require.NotContains(t, body, namespace)
	require.NotContains(t, body, "preemptor")
	require.Contains(t, body, `"name":"predicates"`)
	require.Contains(t, body, `"name":"proportion"`)
	require.Contains(t, body, hasher.Hash(namespace))
}

func TestHasherEdges(t *testing.T) {
	hasher := NewHasher([]byte("key"))
	require.Empty(t, hasher.Hash(""))
	require.Nil(t, hasher.HashEach(nil))
	require.Equal(t, []string{hasher.Hash("a"), hasher.Hash("b")}, hasher.HashEach([]string{"a", "b"}))
	require.True(t, FreeTextIsName("ns/name"))
	require.True(t, FreeTextIsName(strings.Repeat("word ", 20)))
	require.False(t, FreeTextIsName("InsufficientResources"))
	require.False(t, FreeTextIsName(""))
}

func redactJSON(t *testing.T, hasher Hasher, record map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	env := &Envelope{Kind: KindCycle, Record: raw}
	require.NoError(t, RedactEnvelope(env, hasher))
	return string(env.Record)
}

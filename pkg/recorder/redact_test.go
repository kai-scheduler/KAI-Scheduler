// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRedactionCanary puts a customer-style name into every field kind the
// scheduler records and fails if that name survives.
func TestRedactionCanary(t *testing.T) {
	const (
		customer = "acme-customer-name"
		zone     = "us-east-1a"
		gpuMem   = "11441"
		fraction = "0.5"
		reason   = "InsufficientResources"
		secret   = "literal-secret-value"
	)
	hasher := NewHasher([]byte("customer-key"))
	record := map[string]any{
		"clusterName": customer,
		"shardName":   customer,
		"config": map[string]any{
			"tiers": []any{map[string]any{"plugins": []any{map[string]any{
				"name": "recorder",
				"arguments": map[string]any{
					"clusterName": customer,
					"shardName":   customer,
					"address":     "127.0.0.1:9095",
				},
			}}}},
			"usageDBConfig": map[string]any{"connectionString": "https://user:" + secret + "@prometheus.example"},
		},
		"schedulerParams": map[string]any{
			"partitionParams": map[string]any{
				"NodePoolLabelKey":   "kai.scheduler/node-pool",
				"NodePoolLabelValue": customer,
			},
			"queueLabelKey": "kai.scheduler/queue",
		},
		"rawObjects": map[string]any{
			"pods": []any{map[string]any{
				"metadata": map[string]any{
					"name": customer, "namespace": customer, "uid": customer,
					"creationTimestamp": "2026-01-02T03:04:05Z",
					"deletionTimestamp": "2026-01-02T04:04:05Z",
					"labels": map[string]any{
						"kubernetes.io/hostname":           customer,
						"kai.scheduler/node-pool":          customer,
						"kai.scheduler/queue":              customer,
						"batch.kubernetes.io/job-name":     customer,
						"app.kubernetes.io/instance":       customer,
						"topology.kubernetes.io/zone":      zone,
						"node.kubernetes.io/instance-type": "g4dn.xlarge",
						"nvidia.com/gpu.memory":            gpuMem,
						"example.com/user":                 customer,
					},
					"annotations": map[string]any{
						"pod-group-name": customer,
						"gpu-fraction":   fraction,
						"kubectl.kubernetes.io/last-applied-configuration": `{"metadata":{"name":"` + customer + `"}}`,
						"example.com/note": customer,
					},
					"managedFields": []any{map[string]any{"manager": customer}},
				},
				"spec": map[string]any{
					"nodeName":          customer,
					"schedulerName":     "kai-scheduler",
					"priorityClassName": customer,
					"schedulingGates":   []any{map[string]any{"name": customer}},
					"nodeSelector":      map[string]any{"kubernetes.io/hostname": customer, "kai.scheduler/node-pool": customer},
					"affinity": map[string]any{"nodeAffinity": map[string]any{
						"requiredDuringSchedulingIgnoredDuringExecution": map[string]any{
							"nodeSelectorTerms": []any{map[string]any{
								"matchExpressions": []any{map[string]any{"key": "kubernetes.io/hostname", "operator": "In", "values": []any{customer}}},
								"matchFields":      []any{map[string]any{"key": "metadata.name", "operator": "In", "values": []any{customer}}},
							}},
						},
					}},
					"resourceClaims": []any{map[string]any{"name": customer, "resourceClaimName": customer}},
					"containers": []any{map[string]any{
						"name":    "worker",
						"image":   "registry.example/" + customer + ":latest",
						"command": []any{customer},
						"ports":   []any{map[string]any{"containerPort": 8080, "name": "http"}},
						"env": []any{
							map[string]any{"name": "TOKEN", "value": secret},
							map[string]any{"name": "CFG", "valueFrom": map[string]any{"configMapKeyRef": map[string]any{"name": customer, "key": "data"}}},
						},
						"resources": map[string]any{"requests": map[string]any{"cpu": "1", "nvidia.com/gpu": "1"}},
					}},
				},
			}},
			"nodes": []any{map[string]any{
				"metadata": map[string]any{"name": customer, "labels": map[string]any{"kubernetes.io/hostname": customer, "kai.scheduler/node-pool": customer}},
			}},
			"queues":    []any{map[string]any{"metadata": map[string]any{"name": customer, "creationTimestamp": "2026-01-02T03:04:05Z"}, "spec": map[string]any{"parentQueue": customer}}},
			"podGroups": []any{map[string]any{"metadata": map[string]any{"name": customer, "namespace": customer}, "spec": map[string]any{"queue": customer, "priorityClassName": customer, "topologyConstraint": map[string]any{"topology": customer, "requiredTopologyLevel": customer, "preferredTopologyLevel": customer}}, "status": map[string]any{"schedulingConditions": []any{map[string]any{"nodePool": customer, "reason": reason, "message": "pod " + customer + " did not fit"}}}}},
			"resourceClaims": []any{map[string]any{
				"metadata": map[string]any{"name": customer, "namespace": customer},
				"spec":     map[string]any{"devices": map[string]any{"requests": []any{map[string]any{"exactly": map[string]any{"deviceClassName": customer}}}}},
				"status":   map[string]any{"reservedFor": []any{map[string]any{"name": customer, "uid": customer}}, "allocation": map[string]any{"devices": map[string]any{"results": []any{map[string]any{"pool": customer, "device": "gpu-0"}}}}},
			}},
			"resourceSlices": []any{map[string]any{"spec": map[string]any{
				"pool":         map[string]any{"name": customer},
				"nodeSelector": map[string]any{"nodeSelectorTerms": []any{map[string]any{"matchExpressions": []any{map[string]any{"key": "kubernetes.io/hostname", "operator": "In", "values": []any{customer}}}}}},
				"devices":      []any{map[string]any{"name": "gpu-0", "attributes": map[string]any{"product": map[string]any{"string": "Tesla-K80"}}}},
			}}},
			"deviceClasses": []any{map[string]any{"metadata": map[string]any{"name": customer}}},
		},
		"decisions": []any{map[string]any{"action": "preempt", "ops": []any{map[string]any{
			"kind": "evict", "namespace": customer, "name": customer, "node": customer, "uid": customer,
			"message": "evict " + customer, "error": `bindrequests.scheduling.run.ai "` + customer + `" already exists`,
			"eviction": map[string]any{"action": "preempt", "preemptor": map[string]any{"namespace": customer, "name": customer}},
		}}}},
	}
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	env := &Envelope{Kind: KindCycle, Record: raw}
	require.NoError(t, RedactEnvelope(env, hasher))
	body := string(env.Record)
	require.NotContains(t, body, customer)
	require.NotContains(t, body, secret)
	require.NotContains(t, body, "last-applied-configuration")
	require.NotContains(t, body, "usageDBConfig")
	require.Contains(t, body, zone)
	require.Contains(t, body, "g4dn.xlarge")
	require.Contains(t, body, gpuMem)
	require.Contains(t, body, fraction)
	require.Contains(t, body, reason)
	require.Contains(t, body, "Tesla-K80")
	require.Contains(t, body, "2026-01-02T03:04:05Z")
	require.Contains(t, body, "metadata.name")
	require.Contains(t, body, hasher.Hash(customer))
	require.False(t, strings.Contains(body, customer))
}

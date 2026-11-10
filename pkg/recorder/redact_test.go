// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	usagedbapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	commonconstants "github.com/kai-scheduler/api/constants"
)

func TestRedactSwapsNamesAndDropsUnlistedPodFields(t *testing.T) {
	hasher := NewHasher([]byte("customer-key"))
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "training-job",
			Namespace: "team-a",
			Labels: map[string]string{
				"kubernetes.io/hostname":         "node-a",
				"example.com/user":               "alice",
				commonconstants.SubGroupLabelKey: "workers",
			},
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: "job-1",
				commonconstants.GpuFraction:              "0.5",
				"example.com/note":                       "secret-name",
			},
		},
		Spec: v1.PodSpec{
			NodeName:          "node-a",
			SchedulerName:     "kai-scheduler",
			PriorityClassName: "train",
			Containers: []v1.Container{{
				Name:    "worker",
				Image:   "registry.example/train:latest",
				Command: []string{"python", "train.py"},
				Env:     []v1.EnvVar{{Name: "TOKEN", Value: "s3cret"}},
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				},
			}},
			NodeSelector: map[string]string{"example.com/user": "alice"},
		},
		Status: v1.PodStatus{Phase: v1.PodPending},
	}
	state := StateRecord{
		ClusterName: "prod",
		ShardName:   "gpu",
		Config: &conf.SchedulerConfiguration{
			Actions: "allocate",
			UsageDBConfig: &usagedbapi.UsageDBConfig{
				ConnectionString: "https://user:password@prometheus.example",
			},
		},
		RawObjects: &snapshot.RawKubernetesObjects{Pods: []*v1.Pod{pod}},
	}
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	env := &Envelope{Kind: KindState, Record: raw}
	require.NoError(t, RedactEnvelope(env, hasher))

	var redacted StateRecord
	require.NoError(t, json.Unmarshal(env.Record, &redacted))
	require.Equal(t, hasher.Hash("prod"), redacted.ClusterName)
	require.Equal(t, hasher.Hash("gpu"), redacted.ShardName)
	require.Nil(t, redacted.Config.UsageDBConfig)
	require.Equal(t, "allocate", redacted.Config.Actions)

	got := redacted.RawObjects.Pods[0]
	require.Equal(t, hasher.Hash("training-job"), got.Name)
	require.Equal(t, hasher.Hash("team-a"), got.Namespace)
	require.Equal(t, hasher.Hash("node-a"), got.Spec.NodeName)
	require.Equal(t, "kai-scheduler", got.Spec.SchedulerName)
	require.Equal(t, "train", got.Spec.PriorityClassName)
	require.Equal(t, "node-a", got.Labels["kubernetes.io/hostname"])
	require.Equal(t, hasher.Hash("alice"), got.Labels[hasher.Hash("example.com/user")])
	require.Equal(t, hasher.Hash("workers"), got.Labels[commonconstants.SubGroupLabelKey])
	require.Equal(t, hasher.Hash("job-1"), got.Annotations[commonconstants.PodGroupAnnotationForPod])
	require.Equal(t, "0.5", got.Annotations[commonconstants.GpuFraction])
	require.NotContains(t, got.Annotations, "example.com/note")
	require.Empty(t, got.Spec.Containers[0].Name)
	require.Empty(t, got.Spec.Containers[0].Image)
	require.Empty(t, got.Spec.Containers[0].Command)
	require.Empty(t, got.Spec.Containers[0].Env)
	require.Equal(t, resource.MustParse("1"), got.Spec.Containers[0].Resources.Requests[v1.ResourceCPU])
	require.Equal(t, hasher.Hash("alice"), got.Spec.NodeSelector[hasher.Hash("example.com/user")])

	again := hasher.Hash("training-job")
	require.Equal(t, got.Name, again)
	require.NotContains(t, string(env.Record), "training-job")
	require.NotContains(t, string(env.Record), "registry.example")
	require.NotContains(t, string(env.Record), "password@prometheus")
}

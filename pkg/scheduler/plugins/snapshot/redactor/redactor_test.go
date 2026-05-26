// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRedactSnapshot(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-secret-pod",
						Namespace: "production-namespace",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "app", Image: "internal-registry/my-app:v1"},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "another-pod",
						Namespace: "production-namespace", // Same namespace
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	assert.Equal(t, "pod-1", snap.RawObjects.Pods[0].Name)
	assert.Equal(t, "pod-2", snap.RawObjects.Pods[1].Name)

	assert.Equal(t, "namespace-1", snap.RawObjects.Pods[0].Namespace)
	assert.Equal(t, "namespace-1", snap.RawObjects.Pods[1].Namespace)

	assert.Equal(t, "container-1", snap.RawObjects.Pods[0].Spec.Containers[0].Name)
	assert.Equal(t, "image-1", snap.RawObjects.Pods[0].Spec.Containers[0].Image)

	table := r.GetTranslationTable()
	assert.Equal(t, "namespace-1", table["production-namespace"])
	assert.Equal(t, "pod-1", table["my-secret-pod"])
}

func TestRedactLabelsAndAnnotations(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Labels: map[string]string{
							"app":         "my-app",
							"team":        "platform-team",
							"environment": "production",
						},
						Annotations: map[string]string{
							"description":     "critical-service",
							"internal-policy": "restricted-access",
						},
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	for _, value := range pod.ObjectMeta.Labels {
		assert.NotEqual(t, "my-app", value)
		assert.NotEqual(t, "platform-team", value)
		assert.NotEqual(t, "production", value)
	}

	for _, value := range pod.ObjectMeta.Annotations {
		assert.NotEqual(t, "critical-service", value)
		assert.NotEqual(t, "restricted-access", value)
	}

	assert.Equal(t, 3, r.GetStats().LabelsRedacted, "Should have redacted 3 label values")
	assert.Equal(t, 2, r.GetStats().AnnotationsRedacted, "Should have redacted 2 annotation values")
}

func TestRedactEnvVars(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "env-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "app",
								Image: "app:latest",
								Env: []corev1.EnvVar{
									{
										Name:  "API_KEY",
										Value: "sk-super-secret-key-12345",
									},
									{
										Name:  "DATABASE_URL",
										Value: "postgres://user:password@internal-db:5432/prod",
									},
									{
										Name: "SECRET_FROM_K8S",
										ValueFrom: &corev1.EnvVarSource{
											SecretKeyRef: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "app-secrets",
												},
												Key: "api-token",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	container := pod.Spec.Containers[0]

	assert.NotEqual(t, "sk-super-secret-key-12345", container.Env[0].Value)
	assert.NotEqual(t, "postgres://user:password@internal-db:5432/prod", container.Env[1].Value)
	assert.Equal(t, "API_KEY", container.Env[0].Name) // Key names shouldn't be redacted

	assert.NotEqual(t, "app-secrets", container.Env[2].ValueFrom.SecretKeyRef.Name)
	assert.NotEqual(t, "api-token", container.Env[2].ValueFrom.SecretKeyRef.Key)

	assert.Equal(t, 2, r.GetStats().EnvVarsRedacted, "Should have redacted 2 env var values")
}

func TestRedactConfigMaps(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			ConfigMaps: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-config",
						Namespace: "default",
						Labels: map[string]string{
							"app": "myapp",
						},
					},
					Data: map[string]string{
						"database.yml": "host: prod-db.internal\nport: 5432",
						"api.conf":     "endpoint: https://internal-api:8443\ntoken: secret123",
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	cm := snap.RawObjects.ConfigMaps[0]
	assert.Equal(t, "configmap-1", cm.Name)
	assert.Equal(t, "namespace-1", cm.Namespace)

	for key, value := range cm.Data {
		assert.NotEqual(t, "database.yml", key)
		assert.NotEqual(t, "host: prod-db.internal\nport: 5432", value)
		assert.NotEqual(t, "api.conf", key)
		assert.NotEqual(t, "endpoint: https://internal-api:8443\ntoken: secret123", value)
	}
}

func TestRedactSecretsInVolumes(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-with-secrets",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "app",
								Image: "app:v1",
								EnvFrom: []corev1.EnvFromSource{
									{
										SecretRef: &corev1.SecretEnvSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "db-credentials",
											},
										},
									},
									{
										ConfigMapRef: &corev1.ConfigMapEnvSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "app-config",
											},
										},
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "secret-vol",
								VolumeSource: corev1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										SecretName: "tls-certs",
									},
								},
							},
							{
								Name: "config-vol",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "app-config",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]

	// Verify secret references in envFrom are redacted
	assert.NotEqual(t, "db-credentials", pod.Spec.Containers[0].EnvFrom[0].SecretRef.Name)

	// Verify configmap references in envFrom are redacted
	assert.NotEqual(t, "app-config", pod.Spec.Containers[0].EnvFrom[1].ConfigMapRef.Name)

	// Verify volume secret refs are redacted
	assert.NotEqual(t, "tls-certs", pod.Spec.Volumes[0].Secret.SecretName)

	// Verify volume configmap refs are redacted
	assert.NotEqual(t, "app-config", pod.Spec.Volumes[1].ConfigMap.Name)
}

func TestRedactAffinity(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-with-affinity",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
						},
						Affinity: &corev1.Affinity{
							NodeAffinity: &corev1.NodeAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "zone",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{"zone-a", "zone-b"},
												},
											},
										},
									},
								},
							},
							PodAffinity: &corev1.PodAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"app": "cache-server",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]

	// Verify node affinity values are redacted
	nodeAffinity := pod.Spec.Affinity.NodeAffinity
	assert.NotNil(t, nodeAffinity)
	for _, value := range nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values {
		assert.NotEqual(t, "zone-a", value)
		assert.NotEqual(t, "zone-b", value)
	}

	// Verify pod affinity labels are redacted
	podAffinity := pod.Spec.Affinity.PodAffinity
	assert.NotNil(t, podAffinity)
	for _, value := range podAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].LabelSelector.MatchLabels {
		assert.NotEqual(t, "cache-server", value)
	}
}

func TestRedactCommandAndArgs(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-with-cmd",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "app",
								Image: "app:v1",
								Command: []string{
									"/app/entrypoint.sh",
									"--api-key=sk-secret123",
									"--db-password=mysecretpass",
								},
								Args: []string{
									"--config=/etc/app/config.yml",
									"--token=internal-token-123",
								},
							},
						},
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	container := snap.RawObjects.Pods[0].Spec.Containers[0]

	// Verify command args are redacted
	assert.NotEqual(t, "--api-key=sk-secret123", container.Command[1])
	assert.NotEqual(t, "--db-password=mysecretpass", container.Command[2])

	// Verify args are redacted
	assert.NotEqual(t, "--token=internal-token-123", container.Args[1])
}

func TestRedactNodeStatus(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "worker-node-1",
					},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "10.0.0.5",
							},
							{
								Type:    corev1.NodeExternalIP,
								Address: "203.0.113.42",
							},
							{
								Type:    corev1.NodeHostName,
								Address: "prod-worker-1.example.com",
							},
						},
						NodeInfo: corev1.NodeSystemInfo{
							MachineID:  "ec2-i-0123456789abcdef0",
							SystemUUID: "12345678-1234-1234-1234-123456789012",
						},
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	node := snap.RawObjects.Nodes[0]
	assert.Equal(t, "node-1", node.Name)

	// Verify node addresses are redacted
	for _, addr := range node.Status.Addresses {
		assert.NotEqual(t, "10.0.0.5", addr.Address)
		assert.NotEqual(t, "203.0.113.42", addr.Address)
		assert.NotEqual(t, "prod-worker-1.example.com", addr.Address)
	}

	// Verify node identifiers are redacted
	assert.NotEqual(t, "ec2-i-0123456789abcdef0", node.Status.NodeInfo.MachineID)
	assert.NotEqual(t, "12345678-1234-1234-1234-123456789012", node.Status.NodeInfo.SystemUUID)
}

func TestRedactNilFields(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minimal-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
						},
						// All optional fields are nil
					},
				},
			},
		},
	}

	r := NewRedactor()
	// Should not panic with nil fields
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	assert.NotNil(t, pod)
	assert.Equal(t, "pod-1", pod.Name)
}

func TestRedactNilSnapshot(t *testing.T) {
	r := NewRedactor()
	err := r.RedactSnapshot(nil)
	assert.NoError(t, err)
}

func TestRedactConsistency(t *testing.T) {
	// Test that the same value always maps to the same obfuscated value
	r := NewRedactor()

	val1 := r.Obfuscate("secret-api-key", "secret")
	val2 := r.Obfuscate("secret-api-key", "secret")
	val3 := r.Obfuscate("another-secret", "secret")

	assert.Equal(t, val1, val2, "Same value should obfuscate consistently")
	assert.NotEqual(t, val1, val3, "Different values should have different obfuscations")
}

func TestRedactionStats(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Labels: map[string]string{
							"app": "myapp",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "app",
								Image: "app:v1",
								Env: []corev1.EnvVar{
									{Name: "VAR1", Value: "value1"},
									{Name: "VAR2", Value: "value2"},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "vol1",
								VolumeSource: corev1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										SecretName: "secret1",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	stats := r.GetStats()
	assert.Equal(t, 1, stats.PodsRedacted, "Should have redacted 1 pod")
	assert.Greater(t, stats.LabelsRedacted, 0, "Should have redacted labels")
	assert.Equal(t, 2, stats.EnvVarsRedacted, "Should have redacted 2 env vars")
	assert.Greater(t, stats.SecretsRedacted, 0, "Should have redacted secrets")
}

func TestGetTranslationTableDefensiveCopy(t *testing.T) {
	r := NewRedactor()

	val1 := r.Obfuscate("original-1", "test")
	val2 := r.Obfuscate("original-2", "test")

	table := r.GetTranslationTable()
	assert.NotNil(t, table)
	assert.Equal(t, val1, table["original-1"])
	assert.Equal(t, val2, table["original-2"])

	table["original-1"] = "mutated-value"
	table["new-key"] = "new-value"

	originalTable := r.GetTranslationTable()
	assert.Equal(t, val1, originalTable["original-1"], "Internal map should not be mutated")
	assert.NotContains(t, originalTable, "new-key", "New keys should not affect internal state")
}

func TestRedactWeightedPodAffinityTerms(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-with-weighted-affinity",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
						},
						Affinity: &corev1.Affinity{
							PodAffinity: &corev1.PodAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
									{
										Weight: 50,
										PodAffinityTerm: corev1.PodAffinityTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"app": "preferred-app",
												},
											},
											TopologyKey: "kubernetes.io/hostname",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	affinity := pod.Spec.Affinity.PodAffinity
	assert.NotNil(t, affinity)
	assert.NotNil(t, affinity.PreferredDuringSchedulingIgnoredDuringExecution)
	assert.Len(t, affinity.PreferredDuringSchedulingIgnoredDuringExecution, 1)

	term := affinity.PreferredDuringSchedulingIgnoredDuringExecution[0]
	for _, value := range term.PodAffinityTerm.LabelSelector.MatchLabels {
		assert.NotEqual(t, "preferred-app", value, "Label value should be redacted in weighted affinity term")
	}
	assert.NotEqual(t, "kubernetes.io/hostname", term.PodAffinityTerm.TopologyKey, "TopologyKey should be redacted")
}

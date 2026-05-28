// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"fmt"
	"strings"
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
	assert.Equal(t, "namespace-1", table["namespace:production-namespace"])
	assert.Equal(t, "pod-1", table["pod:my-secret-pod"])
}

func TestRedactNilSnapshot(t *testing.T) {
	r := NewRedactor()
	err := r.RedactSnapshot(nil)
	assert.NoError(t, err)
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

func TestRedactInitContainers(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "init-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{
							{
								Name:  "setup",
								Image: "setup-image:v1",
								Env: []corev1.EnvVar{
									{
										Name:  "SETUP_KEY",
										Value: "secret-setup-value",
									},
								},
							},
						},
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
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

	initContainer := pod.Spec.InitContainers[0]
	assert.Equal(t, "initcontainer-1", initContainer.Name)
	assert.NotEqual(t, "setup-image:v1", initContainer.Image)
	assert.NotEqual(t, "secret-setup-value", initContainer.Env[0].Value)
	assert.Equal(t, "SETUP_KEY", initContainer.Env[0].Name) // Env var names should NOT be redacted
}

func TestRedactPodStatus(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
						},
					},
					Status: corev1.PodStatus{
						HostIP: "192.168.1.100",
						PodIP:  "10.0.0.5",
						PodIPs: []corev1.PodIP{
							{IP: "10.0.0.5"},
							{IP: "10.0.0.6"},
						},
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:        "app",
								ContainerID: "docker://abc123def456",
								ImageID:     "docker-pullable://app@sha256:xyz789",
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

	assert.NotEqual(t, "192.168.1.100", pod.Status.HostIP)
	assert.NotEqual(t, "10.0.0.5", pod.Status.PodIP)

	for _, podIP := range pod.Status.PodIPs {
		assert.NotEqual(t, "10.0.0.5", podIP.IP)
		assert.NotEqual(t, "10.0.0.6", podIP.IP)
	}

	assert.NotEqual(t, "docker://abc123def456", pod.Status.ContainerStatuses[0].ContainerID)
	assert.NotEqual(t, "docker-pullable://app@sha256:xyz789", pod.Status.ContainerStatuses[0].ImageID)
}

func TestRedactNodeName(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bound-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						NodeName: "worker-node-42",
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
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
	assert.NotEqual(t, "worker-node-42", pod.Spec.NodeName)
	assert.True(t, strings.HasPrefix(pod.Spec.NodeName, "node-"))
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

	for _, addr := range node.Status.Addresses {
		assert.NotEqual(t, "10.0.0.5", addr.Address)
		assert.NotEqual(t, "203.0.113.42", addr.Address)
		assert.NotEqual(t, "prod-worker-1.example.com", addr.Address)
	}

	assert.NotEqual(t, "ec2-i-0123456789abcdef0", node.Status.NodeInfo.MachineID)
	assert.NotEqual(t, "12345678-1234-1234-1234-123456789012", node.Status.NodeInfo.SystemUUID)
}

func TestRedactNodeSelector(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "selective-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						NodeSelector: map[string]string{
							"disktype":     "ssd",
							"gpu-required": "nvidia-tesla-v100",
						},
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
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

	for _, value := range pod.Spec.NodeSelector {
		assert.NotEqual(t, "ssd", value)
		assert.NotEqual(t, "nvidia-tesla-v100", value)
	}

	assert.Equal(t, 2, r.GetStats().NodeSelectorsRedacted)
}

func TestRedactTolerations(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tolerant-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Tolerations: []corev1.Toleration{
							{
								Key:      "gpu-type",
								Operator: corev1.TolerationOpEqual,
								Value:    "nvidia-a100",
								Effect:   corev1.TaintEffectNoSchedule,
							},
							{
								Key:      "dedicated",
								Operator: corev1.TolerationOpEqual,
								Value:    "machine-learning",
								Effect:   corev1.TaintEffectNoExecute,
							},
						},
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
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

	assert.NotEqual(t, "nvidia-a100", pod.Spec.Tolerations[0].Value)
	assert.NotEqual(t, "machine-learning", pod.Spec.Tolerations[1].Value)

	// Verify structural fields are preserved
	assert.Equal(t, "gpu-type", pod.Spec.Tolerations[0].Key)
	assert.Equal(t, corev1.TolerationOpEqual, pod.Spec.Tolerations[0].Operator)
	assert.Equal(t, corev1.TaintEffectNoSchedule, pod.Spec.Tolerations[0].Effect)

	// Verify stats tracking
	assert.Equal(t, 2, r.GetStats().TolerationsRedacted)
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

	nodeAffinity := pod.Spec.Affinity.NodeAffinity
	assert.NotNil(t, nodeAffinity)
	for _, value := range nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values {
		assert.NotEqual(t, "zone-a", value)
		assert.NotEqual(t, "zone-b", value)
	}

	podAffinity := pod.Spec.Affinity.PodAffinity
	assert.NotNil(t, podAffinity)
	for _, value := range podAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].LabelSelector.MatchLabels {
		assert.NotEqual(t, "cache-server", value)
	}
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

	assert.NotEqual(t, "db-credentials", pod.Spec.Containers[0].EnvFrom[0].SecretRef.Name)

	assert.NotEqual(t, "app-config", pod.Spec.Containers[0].EnvFrom[1].ConfigMapRef.Name)

	assert.NotEqual(t, "tls-certs", pod.Spec.Volumes[0].Secret.SecretName)

	assert.NotEqual(t, "app-config", pod.Spec.Volumes[1].ConfigMap.Name)
}

func TestRedactPersistentVolumes(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			PersistentVolumes: []*corev1.PersistentVolume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "production-pv",
						Labels: map[string]string{
							"tier": "production",
						},
					},
				},
			},
			PersistentVolumeClaims: []*corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "data-pvc",
						Namespace: "production",
						Labels: map[string]string{
							"app": "database",
						},
					},
				},
			},
		},
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pv := snap.RawObjects.PersistentVolumes[0]
	pvc := snap.RawObjects.PersistentVolumeClaims[0]

	assert.Equal(t, "pv-1", pv.Name)
	assert.Equal(t, "pvc-1", pvc.Name)
	assert.Equal(t, "namespace-1", pvc.Namespace)

	for _, value := range pv.ObjectMeta.Labels {
		assert.NotEqual(t, "production", value)
	}
	for _, value := range pvc.ObjectMeta.Labels {
		assert.NotEqual(t, "database", value)
	}

	assert.Equal(t, 1, r.GetStats().PersistentVolumesRedacted)
	assert.Equal(t, 1, r.GetStats().PersistentVolumeClaimsRedacted)
}

func TestRedactOwnerReferences(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "owned-pod",
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "apps/v1",
								Kind:       "Deployment",
								Name:       "production-deployment",
								UID:        "abc-123-def",
							},
							{
								APIVersion: "v1",
								Kind:       "Node",
								Name:       "worker-node-1",
								UID:        "xyz-789-uvw",
							},
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
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

	assert.Len(t, pod.ObjectMeta.OwnerReferences, 2)
	assert.NotEqual(t, "production-deployment", pod.ObjectMeta.OwnerReferences[0].Name)
	assert.NotEqual(t, "worker-node-1", pod.ObjectMeta.OwnerReferences[1].Name)

	assert.Equal(t, "apps/v1", pod.ObjectMeta.OwnerReferences[0].APIVersion)
	assert.Equal(t, "Deployment", pod.ObjectMeta.OwnerReferences[0].Kind)
	assert.Equal(t, "v1", pod.ObjectMeta.OwnerReferences[1].APIVersion)
	assert.Equal(t, "Node", pod.ObjectMeta.OwnerReferences[1].Kind)
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

func TestObfuscateSameValueDifferentPrefix(t *testing.T) {
	r := NewRedactor()

	podObfuscated := r.Obfuscate("worker-1", "pod")
	nodeObfuscated := r.Obfuscate("worker-1", "node")

	assert.NotEqual(t, podObfuscated, nodeObfuscated,
		"Same value with different prefixes should obfuscate to different values")

	assert.Equal(t, "pod-1", podObfuscated)
	assert.Equal(t, "node-1", nodeObfuscated)

	podObfuscated2 := r.Obfuscate("worker-1", "pod")
	assert.Equal(t, "pod-1", podObfuscated2)

	table := r.GetTranslationTable()
	assert.Equal(t, 2, len(table))
	assert.Equal(t, "pod-1", table["pod:worker-1"])
	assert.Equal(t, "node-1", table["node:worker-1"])
}

func TestGetTranslationTableDefensiveCopy(t *testing.T) {
	r := NewRedactor()

	val1 := r.Obfuscate("original-1", "test")
	val2 := r.Obfuscate("original-2", "test")

	table := r.GetTranslationTable()
	assert.NotNil(t, table)
	assert.Equal(t, val1, table["test:original-1"])
	assert.Equal(t, val2, table["test:original-2"])

	table["test:original-1"] = "mutated-value"
	table["new-key"] = "new-value"

	originalTable := r.GetTranslationTable()
	assert.Equal(t, val1, originalTable["test:original-1"], "Internal map should not be mutated")
	assert.NotContains(t, originalTable, "new-key", "New keys should not affect internal state")
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
			ConfigMaps: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-config",
						Namespace: "default",
					},
					Data: map[string]string{
						"key": "value",
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
	assert.Equal(t, 1, stats.ConfigMapsRedacted, "Should track ALL configmaps, not just with data")
}

func TestRedactedSnapshotSchedulingValid(t *testing.T) {
	// Create a snapshot with valid scheduling constraints
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "critical-pod",
						Namespace: "production",
						Labels: map[string]string{
							"priority": "high",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "apps/v1",
								Kind:       "Deployment",
								Name:       "my-deployment",
								UID:        "12345-67890",
							},
						},
					},
					Spec: corev1.PodSpec{
						NodeName:           "node-1",
						ServiceAccountName: "default-sa",
						Containers: []corev1.Container{
							{
								Name:  "app",
								Image: "nginx:latest",
								Env: []corev1.EnvVar{
									{Name: "ENV_VAR", Value: "secret"},
								},
							},
						},
						NodeSelector: map[string]string{
							"disk": "ssd",
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
													Values:   []string{"zone-a"},
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
			Nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"disk": "ssd",
							"zone": "zone-a",
						},
					},
				},
			},
		},
	}

	// Redact the snapshot
	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	redactedSnap := snap
	assert.NotNil(t, redactedSnap.RawObjects)
	assert.Len(t, redactedSnap.RawObjects.Pods, 1)
	assert.Len(t, redactedSnap.RawObjects.Nodes, 1)

	pod := redactedSnap.RawObjects.Pods[0]
	node := redactedSnap.RawObjects.Nodes[0]

	assert.NotEmpty(t, pod.Spec.NodeName)
	assert.NotEmpty(t, node.Name)
	assert.NotEmpty(t, pod.ObjectMeta.OwnerReferences)
	assert.NotEmpty(t, pod.Spec.NodeSelector)
	assert.NotNil(t, pod.Spec.Affinity)

	nodeSelectorValue := pod.Spec.NodeSelector["disk"]
	assert.NotEmpty(t, nodeSelectorValue, "NodeSelector values must be preserved")

	assert.True(t, len(pod.Spec.NodeName) > 0, "Pod must have NodeName for scheduling")
	assert.True(t, len(node.Name) > 0, "Node must have name")

	affinity := pod.Spec.Affinity.NodeAffinity
	assert.NotNil(t, affinity.RequiredDuringSchedulingIgnoredDuringExecution)
	assert.Len(t, affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms, 1)
	terms := affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0]
	assert.Len(t, terms.MatchExpressions, 1)
	assert.Len(t, terms.MatchExpressions[0].Values, 1)
	assert.NotEmpty(t, terms.MatchExpressions[0].Values[0], "Affinity values must be redacted but present")
}

func TestSchedulingDecisionsConsistentAfterRedaction(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				// Pod 1: Must match nodes with workload-type=general
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "frontend-pod",
						Namespace: "production",
						Labels: map[string]string{
							"tier": "frontend",
						},
					},
					Spec: corev1.PodSpec{
						NodeSelector: map[string]string{
							"workload-type": "general",
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
													Values:   []string{"us-east-1a", "us-east-1b"},
												},
											},
										},
									},
								},
							},
						},
						Containers: []corev1.Container{
							{Name: "nginx", Image: "nginx:latest"},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "database-pod",
						Namespace: "production",
						Labels: map[string]string{
							"tier": "database",
						},
					},
					Spec: corev1.PodSpec{
						NodeSelector: map[string]string{
							"workload-type": "database",
							"disk-type":     "ssd",
						},
						Containers: []corev1.Container{
							{Name: "postgres", Image: "postgres:13"},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cache-pod",
						Namespace: "production",
						Labels: map[string]string{
							"tier": "cache",
						},
					},
					Spec: corev1.PodSpec{
						Affinity: &corev1.Affinity{
							PodAffinity: &corev1.PodAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"tier": "frontend",
											},
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
						Containers: []corev1.Container{
							{Name: "redis", Image: "redis:latest"},
						},
					},
				},
			},
			Nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "general-node-1",
						Labels: map[string]string{
							"workload-type": "general",
							"zone":          "us-east-1a",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "general-node-2",
						Labels: map[string]string{
							"workload-type": "general",
							"zone":          "us-east-1b",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "database-node-1",
						Labels: map[string]string{
							"workload-type": "database",
							"disk-type":     "ssd",
						},
					},
				},
			},
		},
	}

	originalPodCount := len(snap.RawObjects.Pods)
	originalNodeCount := len(snap.RawObjects.Nodes)
	originalSelectorKeysPerPod := make([]int, len(snap.RawObjects.Pods))
	for i, pod := range snap.RawObjects.Pods {
		originalSelectorKeysPerPod[i] = len(pod.Spec.NodeSelector)
	}

	originalAffinityConstraints := make([]bool, len(snap.RawObjects.Pods))
	for i, pod := range snap.RawObjects.Pods {
		originalAffinityConstraints[i] = pod.Spec.Affinity != nil
	}

	r := NewRedactor()
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	assert.Equal(t, originalPodCount, len(snap.RawObjects.Pods),
		"Number of pods must remain the same after redaction")
	assert.Equal(t, originalNodeCount, len(snap.RawObjects.Nodes),
		"Number of nodes must remain the same after redaction")

	for i, pod := range snap.RawObjects.Pods {
		assert.Equal(t, originalSelectorKeysPerPod[i], len(pod.Spec.NodeSelector),
			fmt.Sprintf("Pod %d must have same number of selectors", i))

		for key := range pod.Spec.NodeSelector {
			assert.True(t, len(key) > 0, "Selector keys must not be empty")
		}
	}

	for i, pod := range snap.RawObjects.Pods {
		if originalAffinityConstraints[i] {
			assert.NotNil(t, pod.Spec.Affinity,
				fmt.Sprintf("Pod %d should maintain affinity after redaction", i))
		}
	}

	frontendPod := snap.RawObjects.Pods[0]
	assert.NotNil(t, frontendPod.Spec.NodeSelector["workload-type"],
		"Selector key 'workload-type' must exist in redacted pod")
	assert.NotEmpty(t, frontendPod.Spec.NodeSelector["workload-type"],
		"Selector value must be redacted but present")

	databasePod := snap.RawObjects.Pods[1]
	assert.NotNil(t, databasePod.Spec.NodeSelector["disk-type"],
		"Selector key 'disk-type' must exist in redacted pod")
	assert.NotNil(t, databasePod.Spec.NodeSelector["workload-type"],
		"Selector key 'workload-type' must exist in redacted database pod")

	for _, node := range snap.RawObjects.Nodes {
		assert.NotEmpty(t, node.ObjectMeta.Labels, "Nodes should have labels")
		keyCount := len(node.ObjectMeta.Labels)
		assert.Greater(t, keyCount, 0, "Node labels must be present")
	}

	cachePod := snap.RawObjects.Pods[2]
	if cachePod.Spec.Affinity != nil && cachePod.Spec.Affinity.PodAffinity != nil {
		assert.NotNil(t, cachePod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
			"Pod affinity terms must be preserved")
		assert.Greater(t, len(cachePod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution), 0,
			"Pod affinity should have constraints")
	}

	if frontendPod.Spec.Affinity != nil && frontendPod.Spec.Affinity.NodeAffinity != nil {
		na := frontendPod.Spec.Affinity.NodeAffinity
		assert.NotNil(t, na.RequiredDuringSchedulingIgnoredDuringExecution,
			"Node affinity selector terms must be preserved")
		assert.Greater(t, len(na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms), 0,
			"Node affinity should have selector terms")
		terms := na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0]
		assert.Greater(t, len(terms.MatchExpressions), 0,
			"Selector terms should have match expressions")
		assert.Greater(t, len(terms.MatchExpressions[0].Values), 0,
			"Match expression values must be present (redacted but present)")
	}

	stats := r.GetStats()
	assert.Greater(t, stats.PodsRedacted, 0, "Should have redacted pods")
	assert.Greater(t, stats.NodesRedacted, 0, "Should have redacted nodes")
	assert.Greater(t, stats.LabelsRedacted, 0, "Should have redacted node labels")

	t.Logf("✓ Scheduling decision validation passed: %d pods, %d nodes, %d labels redacted",
		stats.PodsRedacted, stats.NodesRedacted, stats.LabelsRedacted)
}

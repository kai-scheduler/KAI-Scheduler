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
						Namespace: "production-namespace",
					},
				},
			},
		},
	}

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	assert.NotEqual(t, "my-secret-pod", snap.RawObjects.Pods[0].Name, "Pod name should be redacted")
	assert.NotEqual(t, "another-pod", snap.RawObjects.Pods[1].Name, "Pod name should be redacted")
	assert.True(t, strings.HasPrefix(snap.RawObjects.Pods[0].Name, "pod-"), "Pod name should have 'pod' prefix")
	assert.True(t, strings.HasPrefix(snap.RawObjects.Pods[1].Name, "pod-"), "Pod name should have 'pod' prefix")

	assert.Equal(t, snap.RawObjects.Pods[0].Namespace, snap.RawObjects.Pods[1].Namespace,
		"Same namespace should map to same obfuscated value")
	assert.True(t, strings.HasPrefix(snap.RawObjects.Pods[0].Namespace, "namespace-"),
		"Namespace should have 'namespace' prefix")

	assert.True(t, strings.HasPrefix(snap.RawObjects.Pods[0].Spec.Containers[0].Name, "container-"),
		"Container name should have 'container' prefix")
	assert.True(t, strings.HasPrefix(snap.RawObjects.Pods[0].Spec.Containers[0].Image, "image-"),
		"Image should have 'image' prefix")

	table := r.GetTranslationTable()
	assert.True(t, len(table) > 0, "Translation table should contain entries")
	assert.Greater(t, r.GetStats().PodsRedacted, 0, "Stats should track redacted pods")
}

func TestRedactNilSnapshot(t *testing.T) {
	r := NewRedactor("")
	err := r.RedactSnapshot(nil)
	assert.NoError(t, err, "Should handle nil snapshot gracefully")
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
					},
				},
			},
		},
	}

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err, "Should not panic with nil fields")

	pod := snap.RawObjects.Pods[0]
	assert.NotNil(t, pod)
	assert.True(t, strings.HasPrefix(pod.Name, "pod-"))
}

// NEW TEST: Object Metadata Redaction
func TestRedactObjectMetadata(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-pod",
						Namespace:         "default",
						UID:               "12345-67890-abcdef",
						ResourceVersion:   "999999",
						CreationTimestamp: metav1.Now(),
						DeletionTimestamp: nil,
						Finalizers:        []string{"finalizer.example.com/cleanup"},
						ManagedFields:     []metav1.ManagedFieldsEntry{},
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

	// Store original values
	originalUID := snap.RawObjects.Pods[0].ObjectMeta.UID
	originalResVer := snap.RawObjects.Pods[0].ObjectMeta.ResourceVersion
	originalTimestamp := snap.RawObjects.Pods[0].ObjectMeta.CreationTimestamp
	originalFinalizer := snap.RawObjects.Pods[0].ObjectMeta.Finalizers[0]

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]

	// Verify UID is redacted
	assert.NotEqual(t, originalUID, pod.ObjectMeta.UID, "UID should be redacted")
	assert.True(t, strings.HasPrefix(string(pod.ObjectMeta.UID), "uid-"), "UID should have prefix")

	// Verify ResourceVersion is redacted
	assert.NotEqual(t, originalResVer, pod.ObjectMeta.ResourceVersion, "ResourceVersion should be redacted")
	assert.True(t, strings.HasPrefix(pod.ObjectMeta.ResourceVersion, "resver-"), "ResourceVersion should have prefix")

	// Verify timestamps are cleared
	assert.True(t, pod.ObjectMeta.CreationTimestamp.IsZero(), "CreationTimestamp should be cleared")
	assert.NotEqual(t, originalTimestamp, pod.ObjectMeta.CreationTimestamp, "Timestamp should be cleared")

	// Verify finalizers are redacted
	assert.NotEqual(t, originalFinalizer, pod.ObjectMeta.Finalizers[0], "Finalizers should be redacted")
	assert.True(t, strings.HasPrefix(pod.ObjectMeta.Finalizers[0], "finalizer-"), "Finalizer should have prefix")

	// Verify ManagedFields are cleared
	assert.Equal(t, 0, len(pod.ObjectMeta.ManagedFields), "ManagedFields should be cleared")
}

// NEW TEST: HostPath Volume Redaction
func TestRedactHostPathVolume(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "host-path-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "host-data",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/var/sensitive/data",
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	hostPathVol := pod.Spec.Volumes[0].VolumeSource.HostPath

	assert.NotNil(t, hostPathVol, "HostPath should exist")
	assert.NotEqual(t, "/var/sensitive/data", hostPathVol.Path, "HostPath should be redacted")
	assert.True(t, strings.HasPrefix(hostPathVol.Path, "hostpath-"), "HostPath should have prefix")
}

// NEW TEST: NFS Volume Redaction
func TestRedactNFSVolume(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nfs-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "nfs-vol",
								VolumeSource: corev1.VolumeSource{
									NFS: &corev1.NFSVolumeSource{
										Server: "nfs-server.internal.company.com",
										Path:   "/export/prod/data",
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	nfsVol := pod.Spec.Volumes[0].VolumeSource.NFS

	assert.NotNil(t, nfsVol, "NFS should exist")
	assert.NotEqual(t, "nfs-server.internal.company.com", nfsVol.Server, "NFS server should be redacted")
	assert.NotEqual(t, "/export/prod/data", nfsVol.Path, "NFS path should be redacted")
	assert.True(t, strings.HasPrefix(nfsVol.Server, "nfsserver-"), "NFS server should have prefix")
	assert.True(t, strings.HasPrefix(nfsVol.Path, "nfspath-"), "NFS path should have prefix")
}

// NEW TEST: AWS EBS Volume Redaction
func TestRedactAWSEBSVolume(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ebs-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "ebs-vol",
								VolumeSource: corev1.VolumeSource{
									AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
										VolumeID: "vol-0a1b2c3d4e5f6g7h8",
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	ebsVol := pod.Spec.Volumes[0].VolumeSource.AWSElasticBlockStore

	assert.NotNil(t, ebsVol, "EBS should exist")
	assert.NotEqual(t, "vol-0a1b2c3d4e5f6g7h8", ebsVol.VolumeID, "EBS VolumeID should be redacted")
	assert.True(t, strings.HasPrefix(ebsVol.VolumeID, "ebs-volume-"), "EBS VolumeID should have prefix")
}

// NEW TEST: Container Probes Redaction
func TestRedactContainerProbes(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "probe-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "app",
								Image: "app:v1",
								LivenessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path: "/health/live",
											Host: "internal-service.local",
										},
									},
								},
								ReadinessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										TCPSocket: &corev1.TCPSocketAction{
											Host: "service-internal.example.com",
										},
									},
								},
								StartupProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										Exec: &corev1.ExecAction{
											Command: []string{"/bin/check-startup.sh", "--secret-key=xyz"},
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	container := pod.Spec.Containers[0]

	// Check Liveness Probe
	assert.NotNil(t, container.LivenessProbe, "Liveness probe should exist")
	assert.NotEqual(t, "/health/live", container.LivenessProbe.HTTPGet.Path, "Probe path should be redacted")
	assert.NotEqual(t, "internal-service.local", container.LivenessProbe.HTTPGet.Host, "Probe host should be redacted")
	assert.True(t, strings.HasPrefix(container.LivenessProbe.HTTPGet.Path, "probepath-"), "Probe path should have prefix")

	// Check Readiness Probe
	assert.NotNil(t, container.ReadinessProbe, "Readiness probe should exist")
	assert.NotEqual(t, "service-internal.example.com", container.ReadinessProbe.TCPSocket.Host, "TCP socket host should be redacted")
	assert.True(t, strings.HasPrefix(container.ReadinessProbe.TCPSocket.Host, "tcphost-"), "TCP socket host should have prefix")

	// Check Startup Probe
	assert.NotNil(t, container.StartupProbe, "Startup probe should exist")
	assert.NotEqual(t, "/bin/check-startup.sh", container.StartupProbe.Exec.Command[0], "Probe command should be redacted")
	assert.NotEqual(t, "--secret-key=xyz", container.StartupProbe.Exec.Command[1], "Probe command args should be redacted")

	stats := r.GetStats()
	assert.Greater(t, stats.ProbesRedacted, 0, "Should track redacted probes")
}

// NEW TEST: Standard Topology Keys Preservation
func TestPreserveStandardTopologyKeys(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "affinity-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Affinity: &corev1.Affinity{
							PodAffinity: &corev1.PodAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"app": "frontend",
											},
										},
										TopologyKey: "kubernetes.io/hostname", // Standard key
									},
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"app": "cache",
											},
										},
										TopologyKey: "custom.topology/datacenter", // Custom key
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	terms := pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution

	// Standard key should be preserved
	assert.Equal(t, "kubernetes.io/hostname", terms[0].TopologyKey,
		"Standard topology key should NOT be redacted")

	// Custom key should be redacted
	assert.NotEqual(t, "custom.topology/datacenter", terms[1].TopologyKey,
		"Custom topology key should be redacted")
	assert.True(t, strings.HasPrefix(terms[1].TopologyKey, "topokey-"),
		"Custom topology key should have prefix")
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

	r := NewRedactor("")
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

	stats := r.GetStats()
	assert.Equal(t, 3, stats.LabelsRedacted, "Should have redacted 3 label values")
	assert.Equal(t, 2, stats.AnnotationsRedacted, "Should have redacted 2 annotation values")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	container := pod.Spec.Containers[0]

	assert.NotEqual(t, "sk-super-secret-key-12345", container.Env[0].Value, "API_KEY value should be redacted")
	assert.NotEqual(t, "postgres://user:password@internal-db:5432/prod", container.Env[1].Value, "DATABASE_URL value should be redacted")
	assert.Equal(t, "API_KEY", container.Env[0].Name, "Env var names should NOT be redacted")

	assert.NotEqual(t, "app-secrets", container.Env[2].ValueFrom.SecretKeyRef.Name, "Secret name should be redacted")
	assert.NotEqual(t, "api-token", container.Env[2].ValueFrom.SecretKeyRef.Key, "Secret key should be redacted")

	stats := r.GetStats()
	assert.Equal(t, 2, stats.EnvVarsRedacted, "Should have redacted 2 env var values")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	container := snap.RawObjects.Pods[0].Spec.Containers[0]

	assert.NotEqual(t, "--api-key=sk-secret123", container.Command[1], "Command args should be redacted")
	assert.NotEqual(t, "--db-password=mysecretpass", container.Command[2], "Command args should be redacted")
	assert.NotEqual(t, "--token=internal-token-123", container.Args[1], "Args should be redacted")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]

	initContainer := pod.Spec.InitContainers[0]
	assert.True(t, strings.HasPrefix(initContainer.Name, "initcontainer-"), "Init container name should have correct prefix")
	assert.NotEqual(t, "setup-image:v1", initContainer.Image, "Init container image should be redacted")
	assert.NotEqual(t, "secret-setup-value", initContainer.Env[0].Value, "Init container env value should be redacted")
	assert.Equal(t, "SETUP_KEY", initContainer.Env[0].Name, "Env var names should NOT be redacted")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]

	assert.NotEqual(t, "192.168.1.100", pod.Status.HostIP, "Host IP should be redacted")
	assert.NotEqual(t, "10.0.0.5", pod.Status.PodIP, "Pod IP should be redacted")

	for _, podIP := range pod.Status.PodIPs {
		assert.NotEqual(t, "10.0.0.5", podIP.IP, "Pod IPs should be redacted")
		assert.NotEqual(t, "10.0.0.6", podIP.IP, "Pod IPs should be redacted")
	}

	assert.NotEqual(t, "docker://abc123def456", pod.Status.ContainerStatuses[0].ContainerID, "Container ID should be redacted")
	assert.NotEqual(t, "docker-pullable://app@sha256:xyz789", pod.Status.ContainerStatuses[0].ImageID, "Image ID should be redacted")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]
	assert.NotEqual(t, "worker-node-42", pod.Spec.NodeName, "Node name should be redacted")
	assert.True(t, strings.HasPrefix(pod.Spec.NodeName, "node-"), "Node name should have 'node' prefix")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	node := snap.RawObjects.Nodes[0]
	assert.True(t, strings.HasPrefix(node.Name, "node-"), "Node name should have 'node' prefix")

	for _, addr := range node.Status.Addresses {
		assert.NotEqual(t, "10.0.0.5", addr.Address, "Node address should be redacted")
		assert.NotEqual(t, "203.0.113.42", addr.Address, "Node address should be redacted")
		assert.NotEqual(t, "prod-worker-1.example.com", addr.Address, "Node hostname should be redacted")
	}

	assert.NotEqual(t, "ec2-i-0123456789abcdef0", node.Status.NodeInfo.MachineID, "Machine ID should be redacted")
	assert.NotEqual(t, "12345678-1234-1234-1234-123456789012", node.Status.NodeInfo.SystemUUID, "System UUID should be redacted")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]

	for _, value := range pod.Spec.NodeSelector {
		assert.NotEqual(t, "ssd", value, "Node selector values should be redacted")
		assert.NotEqual(t, "nvidia-tesla-v100", value, "Node selector values should be redacted")
	}

	stats := r.GetStats()
	assert.Equal(t, 2, stats.NodeSelectorsRedacted, "Should track redacted node selectors")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]

	assert.NotEqual(t, "nvidia-a100", pod.Spec.Tolerations[0].Value, "Toleration values should be redacted")
	assert.NotEqual(t, "machine-learning", pod.Spec.Tolerations[1].Value, "Toleration values should be redacted")

	assert.Equal(t, "gpu-type", pod.Spec.Tolerations[0].Key, "Toleration keys should be preserved")
	assert.Equal(t, corev1.TolerationOpEqual, pod.Spec.Tolerations[0].Operator, "Toleration operators should be preserved")
	assert.Equal(t, corev1.TaintEffectNoSchedule, pod.Spec.Tolerations[0].Effect, "Taint effects should be preserved")

	stats := r.GetStats()
	assert.Equal(t, 2, stats.TolerationsRedacted, "Should track redacted tolerations")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]

	nodeAffinity := pod.Spec.Affinity.NodeAffinity
	assert.NotNil(t, nodeAffinity)
	for _, value := range nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values {
		assert.NotEqual(t, "zone-a", value, "Node affinity values should be redacted")
		assert.NotEqual(t, "zone-b", value, "Node affinity values should be redacted")
	}

	podAffinity := pod.Spec.Affinity.PodAffinity
	assert.NotNil(t, podAffinity)
	for _, value := range podAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].LabelSelector.MatchLabels {
		assert.NotEqual(t, "cache-server", value, "Pod affinity label values should be redacted")
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

	r := NewRedactor("")
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
	assert.Equal(t, "kubernetes.io/hostname", term.PodAffinityTerm.TopologyKey, "Standard TopologyKey should NOT be redacted")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	cm := snap.RawObjects.ConfigMaps[0]
	assert.True(t, strings.HasPrefix(cm.Name, "configmap-"), "ConfigMap name should have 'configmap' prefix")
	assert.True(t, strings.HasPrefix(cm.Namespace, "namespace-"), "Namespace should have 'namespace' prefix")

	for key, value := range cm.Data {
		assert.NotEqual(t, "database.yml", key, "ConfigMap keys should be redacted")
		assert.NotEqual(t, "host: prod-db.internal\nport: 5432", value, "ConfigMap values should be redacted")
		assert.NotEqual(t, "api.conf", key, "ConfigMap keys should be redacted")
		assert.NotEqual(t, "endpoint: https://internal-api:8443\ntoken: secret123", value, "ConfigMap values should be redacted")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]

	assert.NotEqual(t, "db-credentials", pod.Spec.Containers[0].EnvFrom[0].SecretRef.Name, "Secret name should be redacted")
	assert.NotEqual(t, "app-config", pod.Spec.Containers[0].EnvFrom[1].ConfigMapRef.Name, "ConfigMap name should be redacted")
	assert.NotEqual(t, "tls-certs", pod.Spec.Volumes[0].Secret.SecretName, "Secret in volume should be redacted")
	assert.NotEqual(t, "app-config", pod.Spec.Volumes[1].ConfigMap.Name, "ConfigMap in volume should be redacted")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pv := snap.RawObjects.PersistentVolumes[0]
	pvc := snap.RawObjects.PersistentVolumeClaims[0]

	assert.True(t, strings.HasPrefix(pv.Name, "pv-"), "PV name should have 'pv' prefix")
	assert.True(t, strings.HasPrefix(pvc.Name, "pvc-"), "PVC name should have 'pvc' prefix")
	assert.True(t, strings.HasPrefix(pvc.Namespace, "namespace-"), "Namespace should have 'namespace' prefix")

	for _, value := range pv.ObjectMeta.Labels {
		assert.NotEqual(t, "production", value, "PV label values should be redacted")
	}
	for _, value := range pvc.ObjectMeta.Labels {
		assert.NotEqual(t, "database", value, "PVC label values should be redacted")
	}

	stats := r.GetStats()
	assert.Equal(t, 1, stats.PersistentVolumesRedacted)
	assert.Equal(t, 1, stats.PersistentVolumeClaimsRedacted)
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	pod := snap.RawObjects.Pods[0]

	assert.Len(t, pod.ObjectMeta.OwnerReferences, 2)
	assert.NotEqual(t, "production-deployment", pod.ObjectMeta.OwnerReferences[0].Name, "Owner reference names should be redacted")
	assert.NotEqual(t, "worker-node-1", pod.ObjectMeta.OwnerReferences[1].Name, "Owner reference names should be redacted")

	assert.Equal(t, "apps/v1", pod.ObjectMeta.OwnerReferences[0].APIVersion, "APIVersion should be preserved")
	assert.Equal(t, "Deployment", pod.ObjectMeta.OwnerReferences[0].Kind, "Kind should be preserved")
	assert.Equal(t, "v1", pod.ObjectMeta.OwnerReferences[1].APIVersion, "APIVersion should be preserved")
	assert.Equal(t, "Node", pod.ObjectMeta.OwnerReferences[1].Kind, "Kind should be preserved")
}

func TestRedactConsistency(t *testing.T) {
	r := NewRedactor("")

	val1 := r.Obfuscate("secret-api-key", "secret")
	val2 := r.Obfuscate("secret-api-key", "secret")
	val3 := r.Obfuscate("another-secret", "secret")

	assert.Equal(t, val1, val2, "Same value should obfuscate consistently")
	assert.NotEqual(t, val1, val3, "Different values should have different obfuscations")
}

func TestObfuscateSameValueDifferentPrefix(t *testing.T) {
	r := NewRedactor("")

	podObfuscated := r.Obfuscate("worker-1", "pod")
	nodeObfuscated := r.Obfuscate("worker-1", "node")

	assert.NotEqual(t, podObfuscated, nodeObfuscated,
		"Same value with different prefixes should obfuscate to different values")
	assert.True(t, strings.HasPrefix(podObfuscated, "pod-"), "Pod value should have pod prefix")
	assert.True(t, strings.HasPrefix(nodeObfuscated, "node-"), "Node value should have node prefix")

	podObfuscated2 := r.Obfuscate("worker-1", "pod")
	assert.Equal(t, podObfuscated, podObfuscated2, "Same obfuscation should be returned for same input")

	table := r.GetTranslationTable()
	assert.Greater(t, len(table), 0, "Translation table should have entries")
}

func TestGetTranslationTableDefensiveCopy(t *testing.T) {
	r := NewRedactor("")

	r.Obfuscate("original-1", "test")
	r.Obfuscate("original-2", "test")

	table := r.GetTranslationTable()
	assert.NotNil(t, table, "Translation table should not be nil")
	assert.Greater(t, len(table), 0, "Translation table should have entries")

	originalLen := len(table)
	table["new-key"] = "new-value"

	newTable := r.GetTranslationTable()
	assert.Equal(t, originalLen, len(newTable), "Internal map should not be mutated by external changes")
	_, exists := newTable["new-key"]
	assert.False(t, exists, "New keys should not affect internal state")
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	stats := r.GetStats()
	assert.Equal(t, 1, stats.PodsRedacted, "Should have redacted 1 pod")
	assert.Greater(t, stats.LabelsRedacted, 0, "Should have redacted labels")
	assert.Equal(t, 2, stats.EnvVarsRedacted, "Should have redacted 2 env vars")
	assert.Greater(t, stats.SecretsRedacted, 0, "Should have redacted secrets")
	assert.Equal(t, 1, stats.ConfigMapsRedacted, "Should track all configmaps")
}

func TestRedactedSnapshotSchedulingValid(t *testing.T) {
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

	r := NewRedactor("")
	err := r.RedactSnapshot(snap)
	assert.NoError(t, err)

	redactedSnap := snap
	assert.NotNil(t, redactedSnap.RawObjects)
	assert.Len(t, redactedSnap.RawObjects.Pods, 1)
	assert.Len(t, redactedSnap.RawObjects.Nodes, 1)

	pod := redactedSnap.RawObjects.Pods[0]
	node := redactedSnap.RawObjects.Nodes[0]

	assert.NotEmpty(t, pod.Spec.NodeName, "Pod must have NodeName for scheduling")
	assert.NotEmpty(t, node.Name, "Node must have name")
	assert.NotEmpty(t, pod.ObjectMeta.OwnerReferences, "Pod should have owner references")
	assert.NotEmpty(t, pod.Spec.NodeSelector, "Pod should have node selectors")
	assert.NotNil(t, pod.Spec.Affinity, "Pod should have affinity")

	assert.True(t, strings.HasPrefix(pod.Spec.NodeName, "node-"), "Node name should have prefix")
	assert.True(t, strings.HasPrefix(node.Name, "node-"), "Node name should have prefix")
}

func TestSchedulingDecisionsConsistentAfterRedaction(t *testing.T) {
	snap := &snapshot.Snapshot{
		RawObjects: &snapshot.RawKubernetesObjects{
			Pods: []*corev1.Pod{
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

	r := NewRedactor("")
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

	t.Logf("Scheduling decision validation passed: %d pods, %d nodes, %d labels redacted",
		stats.PodsRedacted, stats.NodesRedacted, stats.LabelsRedacted)
}

func TestObfuscateWithSalt(t *testing.T) {
	val1 := NewRedactor("salt1").Obfuscate("value", "prefix")
	val2 := NewRedactor("salt2").Obfuscate("value", "prefix")

	assert.NotEqual(t, val1, val2, "Different salts should produce different obfuscations")

	val3 := NewRedactor("salt1").Obfuscate("value", "prefix")
	assert.Equal(t, val1, val3, "Same salt should produce same obfuscation")
}

// NEW TEST: Whitespace String Handling
func TestWhitespaceStringHandling(t *testing.T) {
	r := NewRedactor("")

	// Test empty string
	result1 := r.Obfuscate("", "prefix")
	assert.Equal(t, "", result1, "Empty string should return empty string")

	// Test whitespace-only string
	result2 := r.Obfuscate("   ", "prefix")
	assert.Equal(t, "   ", result2, "Whitespace-only string should return unchanged")

	// Test tab and newline
	result3 := r.Obfuscate("\t\n", "prefix")
	assert.Equal(t, "\t\n", result3, "Tab/newline should return unchanged")

	// Test normal string (should be obfuscated)
	result4 := r.Obfuscate("value", "prefix")
	assert.NotEqual(t, "value", result4, "Normal string should be obfuscated")
}

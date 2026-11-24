// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storageclaim_info"
)

const (
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestPodSchedulingConstraintsSignature(t *testing.T) {
	pod := getRandomPod()
	pod.Namespace = "test-namespace"
	pod.Labels = map[string]string{"key": "val"}
	pod.Spec.NodeSelector = map[string]string{
		"key": "val",
	}
	pod.Spec.InitContainers[0].Ports[0].HostPort = 200
	pod.Spec.Containers[0].Ports[0].HostPort = 300
	pod.Spec.Affinity = &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      nodePoolLabelKey,
								Operator: "in",
								Values:   []string{"node-pool-1"},
							},
						},
					},
				},
			},
		},
		PodAffinity: &v1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{
				LabelSelector:     getLabelSelector("key", "val"),
				Namespaces:        []string{"test-namespace"},
				TopologyKey:       "topology-key",
				NamespaceSelector: getLabelSelector("key", "val"),
			}},
		},
		PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{
				LabelSelector:     getLabelSelector("key2", "val2"),
				Namespaces:        []string{"test-namespace"},
				TopologyKey:       "topology-key2",
				NamespaceSelector: getLabelSelector("key", "val"),
			}},
		},
	}
	pod.Spec.Tolerations = []v1.Toleration{
		{
			Key:               "toleration-key",
			Operator:          "in",
			Value:             "toleration-value",
			Effect:            "noSchedule",
			TolerationSeconds: pointer.Int64(6),
		},
	}
	pod.Spec.Volumes = []v1.Volume{
		{
			Name: "volume-1",
			VolumeSource: v1.VolumeSource{
				PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
					ClaimName: "claim-1",
				},
			},
		},
		{
			Name: "volume-2",
			VolumeSource: v1.VolumeSource{
				AWSElasticBlockStore: &v1.AWSElasticBlockStoreVolumeSource{
					VolumeID:  "10",
					FSType:    "good-type",
					Partition: 6,
					ReadOnly:  false,
				},
			},
		},
	}
	affinityIngore := v1.NodeInclusionPolicyIgnore
	pod.Spec.TopologySpreadConstraints = []v1.TopologySpreadConstraint{
		{
			MaxSkew:            6,
			TopologyKey:        "key",
			WhenUnsatisfiable:  v1.DoNotSchedule,
			LabelSelector:      getLabelSelector("key", "val"),
			MinDomains:         pointer.Int32(6),
			NodeAffinityPolicy: &affinityIngore,
			NodeTaintsPolicy:   &affinityIngore,
			MatchLabelKeys:     []string{"key"},
		},
	}
	pod.Spec.Priority = pointer.Int32(6)
	pod.Spec.PriorityClassName = "priority-class-1"

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	storageClaimID := storageclaim_info.NewKey("test-namespace", "claim-1")
	podInfo.storageClaims = map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{
		storageClaimID: {
			Key:          storageClaimID,
			Name:         "claim-1",
			Namespace:    "test-namespace",
			Size:         resource.NewQuantity(100, resource.DecimalSI),
			Phase:        v1.ClaimPending,
			StorageClass: "storage-class-1",
		},
	}
	key := podInfo.GetSchedulingConstraintsSignature()

	assert.Equal(t, common_info.SchedulingConstraintsSignature("1631890d56291a89b90d0895e154119a3e92ea9e6457aa1d45870945caa579c2"), key)
}

func TestPodSchedulingConstraintsSignature_NodeSelector(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Spec.NodeSelector = nil
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected node selector to affect signature, got same")
}

func TestPodSchedulingConstraintsSignature_Affinity(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Spec.Affinity = nil
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected affinity to affect signature, got same")
}

func TestPodSchedulingConstraintsSignature_Tolerations(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Spec.Tolerations = nil
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected tolerations to affect signature, got same")
}

func TestPodSchedulingConstraintsSignature_Priority(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Spec.Priority = nil
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected priority to affect signature, got same")
}

func TestPodSchedulingConstraintsSignature_TopologySpreadConstraints(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Spec.TopologySpreadConstraints = nil
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected topology spread constraints to affect signature, got same")
}

func TestPodSchedulingConstraintsSignature_TopologySpreadConstraints_MinDomainsNil(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Spec.TopologySpreadConstraints[0].MinDomains = nil
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected topology spread constraints to affect signature, got same")
}

func TestPodSchedulingConstraintsSignature_TopologySpreadConstraints_nodeAffinityPolicyNil(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Spec.TopologySpreadConstraints[0].NodeAffinityPolicy = nil
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected topology spread constraints to affect signature, got same")
}

func TestPodSchedulingConstraintsSignature_TopologySpreadConstraints_nodeTaintsPolicyNil(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Spec.TopologySpreadConstraints[0].NodeTaintsPolicy = nil
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected topology spread constraints to affect signature, got same")
}

func TestPodSchedulingConstraintsSignature_TopologySpreadConstraints_Namespace(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Namespace = pod.Namespace + "-other"
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected namespace to affect signature of a pod with topology spread constraints, got same")
}

func TestPodSchedulingConstraintsSignature_TopologySpreadConstraints_PodLabels(t *testing.T) {
	basePod := getRandomPod()
	spreadPod := func(labels map[string]string, matchLabelKeys ...string) v1.Pod {
		pod := *basePod.DeepCopy()
		pod.Labels = labels
		pod.Spec.TopologySpreadConstraints = []v1.TopologySpreadConstraint{{
			MaxSkew:           1,
			TopologyKey:       "kubernetes.io/hostname",
			WhenUnsatisfiable: v1.DoNotSchedule,
			LabelSelector:     getLabelSelector("app", "spread"),
			MatchLabelKeys:    matchLabelKeys,
		}}
		return pod
	}
	signature := func(pod v1.Pod) common_info.SchedulingConstraintsSignature {
		return NewTaskInfo(&pod, resource_info.NewResourceVectorMap()).GetSchedulingConstraintsSignature()
	}

	tests := []struct {
		name          string
		base          v1.Pod
		other         v1.Pod
		sameSignature bool
	}{
		{
			name:          "labels outside the selector and matchLabelKeys, such as the StatefulSet pod index, are ignored",
			base:          spreadPod(map[string]string{"app": "spread", "apps.kubernetes.io/pod-index": "0"}),
			other:         spreadPod(map[string]string{"app": "spread", "apps.kubernetes.io/pod-index": "1"}),
			sameSignature: true,
		},
		{
			name:          "indexed Job completion index is ignored",
			base:          spreadPod(map[string]string{"app": "spread", "batch.kubernetes.io/job-completion-index": "0"}),
			other:         spreadPod(map[string]string{"app": "spread", "batch.kubernetes.io/job-completion-index": "7"}),
			sameSignature: true,
		},
		{
			name:  "self-match changes",
			base:  spreadPod(map[string]string{"app": "spread"}),
			other: spreadPod(map[string]string{"app": "other"}),
		},
		{
			name:  "self-match lost by removing the labels",
			base:  spreadPod(map[string]string{"app": "spread"}),
			other: spreadPod(nil),
		},
		{
			name:  "matchLabelKeys value changes",
			base:  spreadPod(map[string]string{"app": "spread", "pod-template-hash": "a"}, "pod-template-hash"),
			other: spreadPod(map[string]string{"app": "spread", "pod-template-hash": "b"}, "pod-template-hash"),
		},
		{
			name:  "matchLabelKeys key is missing",
			base:  spreadPod(map[string]string{"app": "spread", "pod-template-hash": ""}, "pod-template-hash"),
			other: spreadPod(map[string]string{"app": "spread"}, "pod-template-hash"),
		},
		{
			name:          "label outside matchLabelKeys is ignored",
			base:          spreadPod(map[string]string{"app": "spread", "pod-template-hash": "a", "index": "0"}, "pod-template-hash"),
			other:         spreadPod(map[string]string{"app": "spread", "pod-template-hash": "a", "index": "1"}, "pod-template-hash"),
			sameSignature: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.sameSignature {
				assert.Equal(t, signature(test.base), signature(test.other))
			} else {
				assert.NotEqual(t, signature(test.base), signature(test.other))
			}
		})
	}
}

func TestPodSchedulingConstraintsSignature_NoTopologySpreadConstraints_NamespaceAndLabelsIgnored(t *testing.T) {
	pod := getRandomPod()
	pod.Spec.TopologySpreadConstraints = nil

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Namespace = pod.Namespace + "-other"
	pod.Labels = map[string]string{"app": randomString(8)}
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.Equalf(t, key, newKey, "Expected namespace and labels not to affect signature without topology spread constraints")
}

func TestPodSchedulingConstraintsSignature_ContainerPorts(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Spec.Containers[0].Ports[0].HostPort = rand.Int31()
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected container ports to affect signature, got same")
}

func TestPodSchedulingConstraintsSignature_InitContainerPorts(t *testing.T) {
	pod := getRandomPod()

	podInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	key := podInfo.GetSchedulingConstraintsSignature()

	pod.Spec.InitContainers[0].Ports[0].HostPort = rand.Int31()
	newPodInfo := NewTaskInfo(&pod, resource_info.NewResourceVectorMap())
	newKey := newPodInfo.GetSchedulingConstraintsSignature()
	assert.NotEqualf(t, key, newKey, "Expected init container ports to affect signature, got same")
}

// GenerateRandomString generates a random string of given length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	value := make([]byte, length)
	for i := range value {
		value[i] = charset[rand.Intn(len(charset))]
	}
	return string(value)
}

func getLabelSelector(key, val string) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{
			key: val,
		},
	}
}

func getRandomPod() v1.Pod {
	return v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      randomString(10),
			Namespace: randomString(10),
			UID:       types.UID(randomString(10)),
		},
		Spec: v1.PodSpec{
			Volumes: nil,
			InitContainers: []v1.Container{
				{
					Name:       randomString(10),
					Image:      randomString(10),
					Command:    []string{randomString(10)},
					Args:       []string{randomString(10)},
					WorkingDir: randomString(10),
					Ports: []v1.ContainerPort{
						{
							Name:          randomString(10),
							HostPort:      rand.Int31(),
							ContainerPort: rand.Int31(),
						},
					},
					EnvFrom: []v1.EnvFromSource{
						{
							ConfigMapRef: &v1.ConfigMapEnvSource{
								LocalObjectReference: v1.LocalObjectReference{
									Name: randomString(10),
								},
							},
						},
					},
					Env: []v1.EnvVar{
						{
							Name:  randomString(10),
							Value: randomString(10),
						},
					},
					Resources: v1.ResourceRequirements{
						Limits: map[v1.ResourceName]resource.Quantity{
							v1.ResourceCPU: *resource.NewQuantity(rand.Int63(), resource.DecimalSI),
						},
						Requests: map[v1.ResourceName]resource.Quantity{
							v1.ResourceCPU: *resource.NewQuantity(rand.Int63(), resource.DecimalSI),
						},
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:        randomString(10),
							ReadOnly:    rand.Int()%2 == 0,
							MountPath:   randomString(10),
							SubPath:     randomString(10),
							SubPathExpr: randomString(10),
						},
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:       randomString(10),
					Image:      randomString(10),
					Command:    []string{randomString(10)},
					Args:       []string{randomString(10)},
					WorkingDir: randomString(10),
					Ports: []v1.ContainerPort{
						{
							Name:          randomString(10),
							HostPort:      rand.Int31(),
							ContainerPort: rand.Int31(),
						},
					},
					EnvFrom: []v1.EnvFromSource{
						{
							ConfigMapRef: &v1.ConfigMapEnvSource{
								LocalObjectReference: v1.LocalObjectReference{
									Name: randomString(10),
								},
							},
						},
					},
					Env: []v1.EnvVar{
						{
							Name:  randomString(10),
							Value: randomString(10),
						},
					},
					Resources: v1.ResourceRequirements{
						Limits: map[v1.ResourceName]resource.Quantity{
							v1.ResourceCPU: *resource.NewQuantity(rand.Int63(), resource.DecimalSI),
						},
						Requests: map[v1.ResourceName]resource.Quantity{
							v1.ResourceCPU: *resource.NewQuantity(rand.Int63(), resource.DecimalSI),
						},
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:        randomString(10),
							ReadOnly:    rand.Int()%2 == 0,
							MountPath:   randomString(10),
							SubPath:     randomString(10),
							SubPathExpr: randomString(10),
						},
					},
				},
			},
			EphemeralContainers:           nil,
			RestartPolicy:                 v1.RestartPolicy(randomString(10)),
			TerminationGracePeriodSeconds: pointer.Int64(rand.Int63()),
			ActiveDeadlineSeconds:         pointer.Int64(rand.Int63()),
			DNSPolicy:                     v1.DNSPolicy(randomString(10)),
			NodeSelector: map[string]string{
				randomString(10): randomString(10),
			},
			ServiceAccountName:           randomString(10),
			AutomountServiceAccountToken: pointer.Bool(rand.Int()%2 == 0),
			HostNetwork:                  rand.Int()%2 == 0,
			HostPID:                      rand.Int()%2 == 0,
			HostIPC:                      rand.Int()%2 == 0,
			ShareProcessNamespace:        pointer.Bool(rand.Int()%2 == 0),
			ImagePullSecrets:             []v1.LocalObjectReference{{Name: randomString(10)}},
			Hostname:                     randomString(10),
			Subdomain:                    randomString(10),
			Affinity: &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      randomString(10),
										Operator: "in",
										Values:   []string{randomString(10)},
									},
								},
							},
						},
					},
				},
				PodAffinity: &v1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{
						LabelSelector:     getLabelSelector(randomString(5), randomString(5)),
						Namespaces:        []string{randomString(5)},
						TopologyKey:       randomString(5),
						NamespaceSelector: getLabelSelector(randomString(5), randomString(5)),
					}},
				},
				PodAntiAffinity: &v1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{
						LabelSelector:     getLabelSelector(randomString(5), randomString(5)),
						Namespaces:        []string{randomString(5)},
						TopologyKey:       randomString(5),
						NamespaceSelector: getLabelSelector(randomString(5), randomString(5)),
					}},
				},
			},
			SchedulerName: randomString(10),
			Tolerations: []v1.Toleration{
				{
					Key:               randomString(10),
					Operator:          "in",
					Value:             randomString(10),
					Effect:            "noSchedule",
					TolerationSeconds: pointer.Int64(rand.Int63()),
				},
			},
			HostAliases: []v1.HostAlias{{
				IP:        randomString(10),
				Hostnames: []string{randomString(10)},
			}},
			PriorityClassName:  randomString(10),
			Priority:           pointer.Int32(rand.Int31()),
			DNSConfig:          nil,
			ReadinessGates:     nil,
			RuntimeClassName:   nil,
			EnableServiceLinks: nil,
			PreemptionPolicy:   nil,
			Overhead:           nil,
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{
				{
					MaxSkew:            rand.Int31(),
					TopologyKey:        randomString(10),
					WhenUnsatisfiable:  v1.UnsatisfiableConstraintAction(randomString(10)),
					LabelSelector:      getLabelSelector(randomString(5), randomString(5)),
					MinDomains:         pointer.Int32(rand.Int31()),
					NodeAffinityPolicy: randomNodeInclusionPolicy(),
					NodeTaintsPolicy:   randomNodeInclusionPolicy(),
					MatchLabelKeys:     []string{randomString(10)},
				},
			},
			SetHostnameAsFQDN: nil,
			OS:                nil,
			HostUsers:         nil,
		},
	}
}

func randomNodeInclusionPolicy() *v1.NodeInclusionPolicy {
	if rand.Int31()%2 == 0 {
		ignore := v1.NodeInclusionPolicyIgnore
		return &ignore
	}
	honor := v1.NodeInclusionPolicyHonor
	return &honor
}

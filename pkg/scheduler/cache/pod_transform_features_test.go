// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"
	ndf "k8s.io/component-helpers/nodedeclaredfeatures"
	"k8s.io/component-helpers/nodedeclaredfeatures/features"
	"k8s.io/component-helpers/nodedeclaredfeatures/features/restartallcontainers"
)

func TestCompactSchedulerPodPreservesFeatureRequirements(t *testing.T) {
	registry, err := ndf.New(features.AllFeatures)
	require.NoError(t, err)
	for _, containerType := range []string{"regular", "init"} {
		t.Run(containerType, func(t *testing.T) {
			restartPolicy := v1.ContainerRestartPolicyNever
			container := v1.Container{
				Name: "worker", RestartPolicy: &restartPolicy,
				RestartPolicyRules: []v1.ContainerRestartRule{{
					Action: v1.ContainerRestartRuleActionRestartAllContainers,
					ExitCodes: &v1.ContainerRestartRuleOnExitCodes{
						Operator: v1.ContainerRestartRuleOnExitCodesOpIn,
						Values:   []int32{1},
					},
				}},
			}
			pod := &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{container}}, Status: v1.PodStatus{Phase: v1.PodPending}}
			if containerType == "init" {
				pod.Spec.InitContainers = []v1.Container{container}
				pod.Spec.Containers = []v1.Container{{Name: "main"}}
			}
			original := pod.DeepCopy()
			transformed, err := compactSchedulerPod(pod)
			require.NoError(t, err)
			compacted := transformed.(*v1.Pod)
			reqs, err := registry.InferForPodScheduling(&ndf.PodInfo{Spec: &compacted.Spec}, version.MustParse("1.35.4"))
			require.NoError(t, err)
			require.True(t, reqs.Has(restartallcontainers.RestartAllContainersOnContainerExits), "compaction must retain scheduling requirements")
			require.Equal(t, original, pod, "compaction must not mutate the informer input")
			if containerType == "init" {
				compacted.Spec.InitContainers[0].RestartPolicyRules[0].ExitCodes.Values[0] = 42
			} else {
				compacted.Spec.Containers[0].RestartPolicyRules[0].ExitCodes.Values[0] = 42
			}
			require.Equal(t, original, pod, "compacted restart rules must not alias the informer input")
		})
	}
}

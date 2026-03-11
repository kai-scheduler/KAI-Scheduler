// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	kwokv1alpha1 "sigs.k8s.io/kwok/pkg/apis/v1alpha1"
)

const (
	stageName      = "nccl-pod-job"
	statusTemplate = `
{{ $now := Now }}
containerStatuses:
{{ range $index, $item := .spec.containers }}
- image: {{ $item.image | Quote }}
  name: {{ $item.name | Quote }}
  ready: false
  restartCount: 0
  started: false
  state:
    terminated:
      exitCode: 0
      finishedAt: {{ $now | Quote }}
      reason: Completed
      startedAt: {{ $now | Quote }}
{{ end }}
phase: Succeeded
`
)

// CreatePodCompletionStage creates a KWOK stage that marks pods as complete after a random time between minDuration and maxDuration
func createPodCompletionStage(
	ctx context.Context,
	client runtimeClient.Client,
	minDuration time.Duration,
	maxDuration time.Duration,
) error {
	stage := &kwokv1alpha1.Stage{
		ObjectMeta: metav1.ObjectMeta{
			Name: stageName,
		},
		Spec: kwokv1alpha1.StageSpec{
			ResourceRef: kwokv1alpha1.StageResourceRef{
				APIGroup: "",
				Kind:     "Pod",
			},
			Selector: &kwokv1alpha1.StageSelector{
				MatchLabels: map[string]string{
					"app":        "engine-e2e",
					"burst-test": "true",
				},
				MatchExpressions: []kwokv1alpha1.SelectorRequirement{
					{
						Key:      ".metadata.deletionTimestamp",
						Operator: kwokv1alpha1.SelectorOpDoesNotExist,
					},
					{
						Key:      ".status.phase",
						Operator: kwokv1alpha1.SelectorOpIn,
						Values:   []string{"Running"},
					},
				},
			},
			Delay: &kwokv1alpha1.StageDelay{
				DurationMilliseconds:       ptr.To(minDuration.Milliseconds()),
				JitterDurationMilliseconds: ptr.To(maxDuration.Milliseconds()),
			},
			Next: kwokv1alpha1.StageNext{
				Patches: []kwokv1alpha1.StagePatch{
					{
						Subresource: "status",
						Root:        "status",
						Template:    statusTemplate,
						Type:        ptr.To(kwokv1alpha1.StagePatchTypeMergePatch),
					},
				},
			},
		},
	}

	// Check if stage already exists
	existingStage := &kwokv1alpha1.Stage{}
	err := client.Get(ctx, runtimeClient.ObjectKeyFromObject(stage), existingStage)
	if err == nil {
		// Stage exists, update it
		existingStage.Spec = stage.Spec
		return client.Update(ctx, existingStage)
	}

	// Stage doesn't exist, create it
	return client.Create(ctx, stage)
}

// deletePodCompletionStage deletes a KWOK stage by name
func deletePodCompletionStage(
	ctx context.Context,
	client runtimeClient.Client,
) error {
	stage := &kwokv1alpha1.Stage{
		ObjectMeta: metav1.ObjectMeta{
			Name: stageName,
		},
	}
	return client.Delete(ctx, stage)
}

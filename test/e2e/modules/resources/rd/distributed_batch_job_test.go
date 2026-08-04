// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package rd

import (
	"reflect"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
)

func TestBuildDistributedBatchJobCopiesPodTemplateFinalizers(t *testing.T) {
	finalizers := []string{"e2e.kai.scheduler/nccl-completion-observed"}
	job := buildDistributedBatchJob(
		queue.CreateQueueObject("test-queue", ""),
		DistributedBatchJobOptions{PodTemplateFinalizers: finalizers},
		1,
		1,
	)

	if !reflect.DeepEqual(job.Spec.Template.Finalizers, finalizers) {
		t.Fatalf("unexpected Pod template finalizers: %#v", job.Spec.Template.Finalizers)
	}
	finalizers[0] = "mutated"
	if job.Spec.Template.Finalizers[0] != "e2e.kai.scheduler/nccl-completion-observed" {
		t.Fatal("Pod template finalizers share the caller's backing array")
	}
}

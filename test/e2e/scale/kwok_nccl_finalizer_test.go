// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	kwokv1alpha1 "sigs.k8s.io/kwok/pkg/apis/v1alpha1"
)

func TestFinalizerClientConfigUsesSeparateRateLimiter(t *testing.T) {
	config := &rest.Config{QPS: 1, Burst: 2, RateLimiter: flowcontrol.NewFakeAlwaysRateLimiter()}
	finalizerConfig := finalizerClientConfig(config)

	if finalizerConfig == config {
		t.Fatal("finalizer client config must not reuse the submission client config")
	}
	if config.QPS != 1 || config.Burst != 2 {
		t.Fatalf("submission client config was mutated: QPS=%v Burst=%d", config.QPS, config.Burst)
	}
	if finalizerConfig.QPS != finalizerControllerQPS || finalizerConfig.Burst != finalizerControllerBurst {
		t.Fatalf("unexpected finalizer client limits: QPS=%v Burst=%d", finalizerConfig.QPS, finalizerConfig.Burst)
	}
	if finalizerConfig.RateLimiter != nil {
		t.Fatal("finalizer client config retained the submission client's rate limiter")
	}
}

func TestNCCLFinalizerControllerReleasesCompletedJobPods(t *testing.T) {
	client := newFinalizerTestClient(t, completedJob(), podWithNCCLFinalizer())
	controller := newNCCLFinalizerControllerWithClient(client, "test", "batch")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller.Start(ctx)
	defer controller.Stop()

	waitForFinalizerRemoval(t, client, "test", "pod")
}

func TestNCCLFinalizerControllerCleanupRestoresOnlyItsStageChange(t *testing.T) {
	stage := &kwokv1alpha1.Stage{
		ObjectMeta: metav1.ObjectMeta{Name: podDeleteStageName},
		Spec: kwokv1alpha1.StageSpec{Selector: &kwokv1alpha1.StageSelector{
			MatchExpressions: []kwokv1alpha1.SelectorRequirement{{
				Key:      ".metadata.deletionTimestamp",
				Operator: kwokv1alpha1.SelectorOpExists,
			}},
		}},
	}
	client := newFinalizerTestClient(t, stage, incompleteJob(), podWithNCCLFinalizer())
	controller := newNCCLFinalizerControllerWithClient(client, "test", "batch")
	if err := controller.Setup(context.Background()); err != nil {
		t.Fatalf("set up finalizer controller: %v", err)
	}

	updatedStage := &kwokv1alpha1.Stage{}
	if err := client.Get(context.Background(), runtimeClient.ObjectKey{Name: podDeleteStageName}, updatedStage); err != nil {
		t.Fatalf("get updated Stage: %v", err)
	}
	if !hasBatchExclusion(updatedStage.Spec.Selector.MatchExpressions, "batch") {
		t.Fatal("NCCL batch exclusion was not added")
	}

	if err := controller.Cleanup(context.Background()); err != nil {
		t.Fatalf("clean up finalizer controller: %v", err)
	}

	pod := &v1.Pod{}
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: "test", Name: "pod"}, pod); err != nil {
		t.Fatalf("get cleaned Pod: %v", err)
	}
	if len(pod.Finalizers) != 1 || pod.Finalizers[0] != "other.example/finalizer" {
		t.Fatalf("cleanup did not preserve other finalizers: %#v", pod.Finalizers)
	}

	if err := client.Get(context.Background(), runtimeClient.ObjectKey{Name: podDeleteStageName}, updatedStage); err != nil {
		t.Fatalf("get restored Stage: %v", err)
	}
	if hasBatchExclusion(updatedStage.Spec.Selector.MatchExpressions, "batch") {
		t.Fatal("NCCL batch exclusion was not removed")
	}
	if len(updatedStage.Spec.Selector.MatchExpressions) != 1 {
		t.Fatalf("unrelated Stage selector changed: %#v", updatedStage.Spec.Selector.MatchExpressions)
	}
}

func newFinalizerTestClient(t *testing.T, objects ...runtimeClient.Object) runtimeClient.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add batch scheme: %v", err)
	}
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := kwokv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add KWOK scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func completedJob() *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "job",
			UID:       types.UID("job-uid"),
			Labels:    map[string]string{distributedJobBatchLabel: "batch"},
		},
		Spec:   batchv1.JobSpec{Completions: ptr.To(int32(1))},
		Status: batchv1.JobStatus{Succeeded: 1},
	}
}

func incompleteJob() *batchv1.Job {
	job := completedJob()
	job.Status.Succeeded = 0
	return job
}

func podWithNCCLFinalizer() *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "test",
			Name:       "pod",
			Finalizers: []string{"other.example/finalizer", ncclPodFinalizer},
			Labels: map[string]string{
				distributedJobBatchLabel: "batch",
				batchv1.JobNameLabel:     "job",
			},
		},
	}
}

func waitForFinalizerRemoval(t *testing.T, client runtimeClient.Client, namespace, name string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pod := &v1.Pod{}
		if err := client.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
			t.Fatalf("get Pod while waiting for finalizer removal: %v", err)
		}
		if _, found := withoutFinalizer(pod.Finalizers, ncclPodFinalizer); !found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for finalizer controller to release Pod")
}

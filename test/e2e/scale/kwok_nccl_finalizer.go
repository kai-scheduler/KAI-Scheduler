// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	kwokv1alpha1 "sigs.k8s.io/kwok/pkg/apis/v1alpha1"

	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
)

const (
	ncclPodFinalizer              = "e2e.kai.scheduler/nccl-completion-observed"
	podDeleteStageName            = "pod-delete"
	finalizerControllerQPS        = 50
	finalizerControllerBurst      = 300
	finalizerControllerWorkers    = 32
	finalizerControllerPollPeriod = time.Second
)

type ncclFinalizerController struct {
	client      runtimeClient.Client
	namespace   string
	batchID     string
	batchLabels map[string]string

	ctx       context.Context
	cancel    context.CancelFunc
	work      chan batchv1.Job
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup

	mu        sync.Mutex
	processed map[string]struct{}
}

func newNCCLFinalizerController(
	testCtx *testcontext.TestContext,
	namespace string,
	batchID string,
) (*ncclFinalizerController, error) {
	config := finalizerClientConfig(testCtx.KubeConfig)
	client, err := runtimeClient.New(config, runtimeClient.Options{Scheme: testCtx.ControllerClient.Scheme()})
	if err != nil {
		return nil, fmt.Errorf("create NCCL finalizer client: %w", err)
	}
	return newNCCLFinalizerControllerWithClient(client, namespace, batchID), nil
}

func finalizerClientConfig(config *rest.Config) *rest.Config {
	finalizerConfig := rest.CopyConfig(config)
	finalizerConfig.QPS = finalizerControllerQPS
	finalizerConfig.Burst = finalizerControllerBurst
	finalizerConfig.RateLimiter = nil
	return finalizerConfig
}

func newNCCLFinalizerControllerWithClient(
	client runtimeClient.Client,
	namespace string,
	batchID string,
) *ncclFinalizerController {
	return &ncclFinalizerController{
		client:      client,
		namespace:   namespace,
		batchID:     batchID,
		batchLabels: map[string]string{distributedJobBatchLabel: batchID},
		processed:   make(map[string]struct{}),
	}
}

func (c *ncclFinalizerController) Setup(ctx context.Context) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		stage := &kwokv1alpha1.Stage{}
		if err := c.client.Get(ctx, runtimeClient.ObjectKey{Name: podDeleteStageName}, stage); err != nil {
			return fmt.Errorf("get KWOK pod-delete Stage: %w", err)
		}
		if stage.Spec.Selector == nil {
			return fmt.Errorf("KWOK pod-delete Stage has no selector")
		}
		if hasBatchExclusion(stage.Spec.Selector.MatchExpressions, c.batchID) {
			return nil
		}
		stage.Spec.Selector.MatchExpressions = append(stage.Spec.Selector.MatchExpressions, batchExclusion(c.batchID))
		if err := c.client.Update(ctx, stage); err != nil {
			return fmt.Errorf("exclude NCCL batch from KWOK pod-delete Stage: %w", err)
		}
		return nil
	})
}

func (c *ncclFinalizerController) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		c.ctx, c.cancel = context.WithCancel(ctx)
		c.work = make(chan batchv1.Job, finalizerControllerWorkers*2)
		c.wg.Add(finalizerControllerWorkers + 1)
		for range finalizerControllerWorkers {
			go c.runWorker()
		}
		go c.run()
	})
}

func (c *ncclFinalizerController) Stop() {
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		c.wg.Wait()
	})
}

func (c *ncclFinalizerController) WaitForCompletion(ctx context.Context, expectedPods int) (int, error) {
	ticker := time.NewTicker(finalizerControllerPollPeriod)
	defer ticker.Stop()

	for {
		succeeded, err := c.succeededPods(ctx)
		if err != nil {
			return 0, err
		}
		if succeeded >= expectedPods {
			return succeeded, nil
		}

		select {
		case <-ctx.Done():
			return succeeded, fmt.Errorf("wait for NCCL Job completion: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *ncclFinalizerController) Cleanup(ctx context.Context) error {
	c.Stop()
	releaseErr := c.releaseAllFinalizers(ctx)
	restoreErr := c.restorePodDeleteStage(ctx)
	return errors.Join(releaseErr, restoreErr)
}

func (c *ncclFinalizerController) run() {
	defer c.wg.Done()
	ticker := time.NewTicker(finalizerControllerPollPeriod)
	defer ticker.Stop()

	for {
		if err := c.enqueueCompletedJobs(); err != nil && c.ctx.Err() == nil {
			GinkgoLogr.Error(err, "Reconcile NCCL Pod finalizers", "batchID", c.batchID)
		}
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *ncclFinalizerController) runWorker() {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case job := <-c.work:
			if err := c.releaseJobFinalizers(c.ctx, job); err != nil && c.ctx.Err() == nil {
				GinkgoLogr.Error(err, "Release NCCL Pod finalizers", "batchID", c.batchID, "job", job.Name)
				c.markForRetry(job)
			}
		}
	}
}

func (c *ncclFinalizerController) enqueueCompletedJobs() error {
	jobs := &batchv1.JobList{}
	if err := c.client.List(c.ctx, jobs, runtimeClient.InNamespace(c.namespace), runtimeClient.MatchingLabels(c.batchLabels)); err != nil {
		return fmt.Errorf("list NCCL Jobs: %w", err)
	}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if !isJobComplete(job) || !c.markProcessed(job) {
			continue
		}
		select {
		case <-c.ctx.Done():
			return nil
		case c.work <- *job.DeepCopy():
		}
	}
	return nil
}

func (c *ncclFinalizerController) succeededPods(ctx context.Context) (int, error) {
	jobs := &batchv1.JobList{}
	if err := c.client.List(ctx, jobs, runtimeClient.InNamespace(c.namespace), runtimeClient.MatchingLabels(c.batchLabels)); err != nil {
		return 0, fmt.Errorf("list NCCL Jobs: %w", err)
	}
	succeeded := 0
	for i := range jobs.Items {
		succeeded += int(jobs.Items[i].Status.Succeeded)
	}
	return succeeded, nil
}

func (c *ncclFinalizerController) releaseAllFinalizers(ctx context.Context) error {
	jobs := &batchv1.JobList{}
	if err := c.client.List(ctx, jobs, runtimeClient.InNamespace(c.namespace), runtimeClient.MatchingLabels(c.batchLabels)); err != nil {
		return fmt.Errorf("list NCCL Jobs for finalizer cleanup: %w", err)
	}
	return c.releaseJobs(ctx, jobs.Items)
}

func (c *ncclFinalizerController) releaseJobs(ctx context.Context, jobs []batchv1.Job) error {
	work := make(chan batchv1.Job)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var releaseErr error

	for range finalizerControllerWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range work {
				if err := c.releaseJobFinalizers(ctx, job); err != nil {
					errMu.Lock()
					releaseErr = errors.Join(releaseErr, err)
					errMu.Unlock()
				}
			}
		}()
	}

	for i := range jobs {
		select {
		case <-ctx.Done():
			close(work)
			wg.Wait()
			return errors.Join(releaseErr, ctx.Err())
		case work <- *jobs[i].DeepCopy():
		}
	}
	close(work)
	wg.Wait()
	return releaseErr
}

func (c *ncclFinalizerController) releaseJobFinalizers(ctx context.Context, job batchv1.Job) error {
	pods := &v1.PodList{}
	if err := c.client.List(ctx, pods,
		runtimeClient.InNamespace(c.namespace),
		runtimeClient.MatchingLabels(map[string]string{batchv1.JobNameLabel: job.Name}),
	); err != nil {
		return fmt.Errorf("list Pods for Job %s: %w", job.Name, err)
	}
	for i := range pods.Items {
		if err := c.releasePodFinalizer(ctx, &pods.Items[i]); err != nil {
			return err
		}
	}
	return nil
}

func (c *ncclFinalizerController) releasePodFinalizer(ctx context.Context, pod *v1.Pod) error {
	finalizers, found := withoutFinalizer(pod.Finalizers, ncclPodFinalizer)
	if !found {
		return nil
	}
	base := pod.DeepCopy()
	pod.Finalizers = finalizers
	if err := c.client.Patch(ctx, pod, runtimeClient.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("release finalizer from Pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	return nil
}

func (c *ncclFinalizerController) restorePodDeleteStage(ctx context.Context) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		stage := &kwokv1alpha1.Stage{}
		if err := c.client.Get(ctx, runtimeClient.ObjectKey{Name: podDeleteStageName}, stage); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("get KWOK pod-delete Stage for cleanup: %w", err)
		}
		if stage.Spec.Selector == nil {
			return nil
		}
		expressions, removed := removeBatchExclusion(stage.Spec.Selector.MatchExpressions, c.batchID)
		if !removed {
			return nil
		}
		stage.Spec.Selector.MatchExpressions = expressions
		if err := c.client.Update(ctx, stage); err != nil {
			return fmt.Errorf("restore KWOK pod-delete Stage: %w", err)
		}
		return nil
	})
}

func (c *ncclFinalizerController) markProcessed(job *batchv1.Job) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := string(job.UID)
	if _, found := c.processed[key]; found {
		return false
	}
	c.processed[key] = struct{}{}
	return true
}

func (c *ncclFinalizerController) markForRetry(job batchv1.Job) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.processed, string(job.UID))
}

func isJobComplete(job *batchv1.Job) bool {
	return job.Spec.Completions != nil && job.Status.Succeeded >= *job.Spec.Completions
}

func batchExclusion(batchID string) kwokv1alpha1.SelectorRequirement {
	return kwokv1alpha1.SelectorRequirement{
		Key:      batchLabelSelectorKey(),
		Operator: kwokv1alpha1.SelectorOpNotIn,
		Values:   []string{batchID},
	}
}

func hasBatchExclusion(expressions []kwokv1alpha1.SelectorRequirement, batchID string) bool {
	for _, expression := range expressions {
		if isBatchExclusion(expression, batchID) {
			return true
		}
	}
	return false
}

func removeBatchExclusion(expressions []kwokv1alpha1.SelectorRequirement, batchID string) ([]kwokv1alpha1.SelectorRequirement, bool) {
	result := make([]kwokv1alpha1.SelectorRequirement, 0, len(expressions))
	removed := false
	for _, expression := range expressions {
		if isBatchExclusion(expression, batchID) {
			removed = true
			continue
		}
		result = append(result, expression)
	}
	return result, removed
}

func isBatchExclusion(expression kwokv1alpha1.SelectorRequirement, batchID string) bool {
	return expression.Key == batchLabelSelectorKey() &&
		expression.Operator == kwokv1alpha1.SelectorOpNotIn &&
		len(expression.Values) == 1 && expression.Values[0] == batchID
}

func batchLabelSelectorKey() string {
	return ".metadata.labels[\"" + distributedJobBatchLabel + "\"]"
}

func withoutFinalizer(finalizers []string, target string) ([]string, bool) {
	result := make([]string, 0, len(finalizers))
	found := false
	for _, finalizer := range finalizers {
		if finalizer == target {
			found = true
			continue
		}
		result = append(result, finalizer)
	}
	return result, found
}

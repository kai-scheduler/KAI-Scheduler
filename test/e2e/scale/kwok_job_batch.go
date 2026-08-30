// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/panjf2000/ants/v2"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/client-go/rest"

	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

const defaultScaleSubmissionWorkers = 32

type jobSubmission struct {
	ExpectedPods int
	Submit       func(context.Context, map[string]string) (*batchv1.Job, error)
}

// JobResult is one scale Job creation result.
type JobResult struct {
	Job *batchv1.Job
	Err error
}

func scaleSubmissionWorkerCount(config *rest.Config, jobs int) int {
	if jobs <= 0 {
		return 0
	}
	workers := defaultScaleSubmissionWorkers
	if config != nil && config.Burst > 0 {
		workers = config.Burst
	} else if config != nil && config.QPS > 0 {
		workers = int(math.Ceil(float64(config.QPS)))
	}
	return min(workers, jobs)
}

func submitJobBatch(
	ctx context.Context,
	testCtx *testcontext.TestContext,
	namespace string,
	submissions []jobSubmission,
) (*jobBatchTracker, error) {
	if len(submissions) == 0 {
		return nil, fmt.Errorf("cannot submit an empty Job batch")
	}
	expectedPods := 0
	for _, submission := range submissions {
		if submission.ExpectedPods <= 0 {
			return nil, fmt.Errorf("Job submission must expect at least one Pod")
		}
		expectedPods += submission.ExpectedPods
	}

	batchID := utils.GenerateRandomK8sName(10)
	batchLabels := map[string]string{distributedJobBatchLabel: batchID}
	tracker, err := newJobBatchTracker(ctx, testCtx.ControllerClient, namespace, batchID, batchLabels, expectedPods, len(submissions))
	if err != nil {
		return nil, err
	}

	pool, err := ants.NewPool(scaleSubmissionWorkerCount(testCtx.KubeConfig, len(submissions)))
	if err != nil {
		tracker.Close()
		return nil, fmt.Errorf("create Job submission pool: %w", err)
	}
	defer pool.Release()

	results := make(chan JobResult, len(submissions))
	var submitted sync.WaitGroup
	for _, submission := range submissions {
		submission := submission
		submitted.Add(1)
		if err := pool.Submit(func() {
			defer submitted.Done()
			job, err := submission.Submit(ctx, cloneStringMap(batchLabels))
			results <- JobResult{Job: job, Err: err}
		}); err != nil {
			submitted.Done()
			results <- JobResult{Err: fmt.Errorf("submit Job creation task: %w", err)}
		}
	}
	submitted.Wait()
	close(results)

	var submissionError error
	for result := range results {
		if result.Err != nil {
			submissionError = errors.Join(submissionError, result.Err)
			continue
		}
		tracker.AddJob(result.Job)
	}
	if submissionError != nil {
		tracker.Close()
		return nil, fmt.Errorf("submit Job batch %s: %w", batchID, submissionError)
	}
	if err := tracker.WaitForReady(ctx); err != nil {
		tracker.Close()
		return nil, err
	}
	return tracker, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

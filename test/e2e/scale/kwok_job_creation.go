// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"maps"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
)

func createJobObjectForKwok(
	ctx context.Context, testCtx *testcontext.TestContext,
	jobQueue *v2.Queue,
	resources v1.ResourceRequirements,
	extraLabels map[string]string,
) (*batchv1.Job, error) {
	job := rd.CreateBatchJobObject(jobQueue, resources)
	addKWOKTaintsAndAffinity(&job.Spec.Template.Spec)
	maps.Copy(job.Spec.Template.ObjectMeta.Labels, extraLabels)

	return job, rd.CreateObjectWithRetries(ctx, testCtx.ControllerClient, job)
}

// createFractionJobForKwok creates a job whose pod requests a GPU fraction. Fractional requests are
// expressed through an annotation rather than the resource list.
func createFractionJobForKwok(
	ctx context.Context, testCtx *testcontext.TestContext,
	jobQueue *v2.Queue, fraction string, extraLabels map[string]string,
) (*batchv1.Job, error) {
	job := rd.CreateBatchJobObject(jobQueue, v1.ResourceRequirements{})
	addKWOKTaintsAndAffinity(&job.Spec.Template.Spec)
	job.Spec.Template.ObjectMeta.Annotations[constants.GpuFraction] = fraction
	maps.Copy(job.Spec.Template.ObjectMeta.Labels, extraLabels)

	return job, rd.CreateObjectWithRetries(ctx, testCtx.ControllerClient, job)
}

// kwokJobOpts pins every distributed job created by the scale suite to the simulated nodes.
func kwokJobOpts(opts rd.DistributedBatchJobOptions) rd.DistributedBatchJobOptions {
	opts.PodSpecMutator = addKWOKTaintsAndAffinity
	return opts
}

func createDistributedJobForKwok(
	ctx context.Context, testCtx *testcontext.TestContext,
	jobQueue *v2.Queue, opts rd.DistributedBatchJobOptions,
) (*rd.JobResult, error) {
	return rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, jobQueue, kwokJobOpts(opts))
}

func submitDistributedJobForKwok(
	ctx context.Context, testCtx *testcontext.TestContext,
	jobQueue *v2.Queue, opts rd.DistributedBatchJobOptions,
) (*batchv1.Job, error) {
	return rd.SubmitDistributedBatchJob(ctx, testCtx.ControllerClient, jobQueue, kwokJobOpts(opts))
}

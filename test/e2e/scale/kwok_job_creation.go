// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"maps"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
	"github.com/kai-scheduler/api/scheduling/v2alpha2"
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

func createDistributedJobForKwok(
	ctx context.Context, testCtx *testcontext.TestContext,
	jobQueue *v2.Queue, resourcesPerPod v1.ResourceRequirements, numberOfTasks int,
	extraLabels map[string]string, topologyConstraint *v2alpha2.TopologyConstraint,
) (*rd.JobResult, error) {
	return rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, jobQueue,
		rd.DistributedBatchJobOptions{
			Parallelism:        ptr.To(int32(numberOfTasks)),
			Resources:          resourcesPerPod,
			ExtraLabels:        extraLabels,
			TopologyConstraint: topologyConstraint,
			PodSpecMutator:     addKWOKTaintsAndAffinity,
		})
}

func submitDistributedJobForKwok(
	ctx context.Context, testCtx *testcontext.TestContext,
	jobQueue *v2.Queue, resourcesPerPod v1.ResourceRequirements, numberOfTasks int,
	extraLabels, jobLabels map[string]string, topologyConstraint *v2alpha2.TopologyConstraint,
) (*batchv1.Job, error) {
	return rd.SubmitDistributedBatchJob(ctx, testCtx.ControllerClient, jobQueue,
		rd.DistributedBatchJobOptions{
			Parallelism:        ptr.To(int32(numberOfTasks)),
			Resources:          resourcesPerPod,
			ExtraLabels:        extraLabels,
			JobLabels:          jobLabels,
			TopologyConstraint: topologyConstraint,
			PodSpecMutator:     addKWOKTaintsAndAffinity,
		})
}

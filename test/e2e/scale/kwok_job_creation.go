// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
)

func createJobObjectForKwok(
	ctx context.Context, testCtx *testcontext.TestContext,
	jobQueue *v2.Queue,
	resources v1.ResourceRequirements,
	extraLabels map[string]string,
) *batchv1.Job {
	job, _, _, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, jobQueue,
		rd.DistributedBatchJobOptions{
			Resources:      resources,
			ExtraLabels:    extraLabels,
			PodSpecMutator: addKWOKTaintsAndAffinity,
		})
	Expect(err).To(Succeed())
	return job
}

func createDistributedJobForKwok(
	ctx context.Context, testCtx *testcontext.TestContext,
	jobQueue *v2.Queue, resourcesPerPod v1.ResourceRequirements, numberOfTasks int,
	extraLabels map[string]string, topologyConstraint *v2alpha2.TopologyConstraint,
) (*v2alpha2.PodGroup, []*v1.Pod, error) {
	_, pg, pods, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, jobQueue,
		rd.DistributedBatchJobOptions{
			Parallelism:        ptr.To(int32(numberOfTasks)),
			Resources:          resourcesPerPod,
			ExtraLabels:        extraLabels,
			TopologyConstraint: topologyConstraint,
			PodSpecMutator:     addKWOKTaintsAndAffinity,
		})
	return pg, pods, err
}

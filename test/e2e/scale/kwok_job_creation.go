// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"fmt"
	"sync"

	. "github.com/onsi/gomega"
	"go.uber.org/multierr"
	"golang.org/x/exp/maps"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

func createJobObjectForKwok(
	ctx context.Context, testCtx *testcontext.TestContext,
	jobQueue *v2.Queue,
	resources v1.ResourceRequirements,
	extraLabels map[string]string,
) *batchv1.Job {
	job := rd.CreateBatchJobObject(jobQueue, resources)
	addKWOKTaintsAndAffinity(&job.Spec.Template.Spec)

	maps.Copy(job.Spec.Template.ObjectMeta.Labels, extraLabels)

	Expect(createObjectWithRetries(ctx, testCtx.ControllerClient, job)).To(Succeed())

	return job
}

// createDistributedJobForKwok creates one distributed job with podsPerDistributedJob batch jobs each with one pod
func createDistributedJobForKwok(
	ctx context.Context, testCtx *testcontext.TestContext,
	jobQueue *v2.Queue, resourcesPerPod v1.ResourceRequirements, numberOfTasks int,
	extraLabels map[string]string, topologyConstraint *v2alpha2.TopologyConstraint,
) (*v2alpha2.PodGroup, []*v1.Pod, error) {
	namespace := queue.GetConnectedNamespaceToQueue(jobQueue)
	podGroup := pod_group.Create(
		namespace, "distributed-job-"+utils.GenerateRandomK8sName(10), jobQueue.Name,
	)
	podGroup.Spec.MinMember = int32(numberOfTasks)
	maps.Copy(podGroup.Labels, extraLabels)
	if topologyConstraint != nil {
		podGroup.Spec.TopologyConstraint = *topologyConstraint
	}

	err := createObjectWithRetries(ctx, testCtx.ControllerClient, podGroup)
	if err != nil {
		return nil, nil, err
	}

	var pods []*v1.Pod
	var creationError error
	podsLock := sync.Mutex{}
	var wg sync.WaitGroup

	for i := range numberOfTasks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			pod := rd.CreatePodObject(jobQueue, resourcesPerPod)
			pod.Name = fmt.Sprintf("distributed-pod-%d-%s", i, utils.GenerateRandomK8sName(10))

			if pod.Annotations == nil {
				pod.Annotations = map[string]string{}
			}
			pod.Annotations[pod_group.PodGroupNameAnnotation] = podGroup.Name

			maps.Copy(pod.Labels, extraLabels)
			addKWOKTaintsAndAffinity(&pod.Spec)

			err := createObjectWithRetries(ctx, testCtx.ControllerClient, pod)

			podsLock.Lock()
			if err != nil {
				creationError = multierr.Append(creationError, err)
			} else {
				pods = append(pods, pod)
			}
			podsLock.Unlock()
		}(i)
	}
	wg.Wait()

	if creationError != nil {
		return nil, nil, fmt.Errorf("failed to create some pods: %w", creationError)
	}

	return podGroup, pods, nil
}

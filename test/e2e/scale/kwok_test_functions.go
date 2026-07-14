// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	schedulerconfig "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	waitutils "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	gpuOperatorNamespace     = "gpu-operator"
	KWOKOperatorNodePoolName = "managed-nodepool"
	podsPollIntervalSeconds  = 10
	testLabelKey             = "scale-test"
	distributedJobBatchLabel = "scale-test-batch"

	defaultNumberOfNodes         = 500
	gpusPerNode                  = 8
	defaultPodsPerDistributedJob = 10
	maxFlowTimeoutMinutes        = 90
	ncclTimeoutMinutes           = (60 * 4)

	statusMeasuringSamples = 10

	pendingBackgroundTasks = 400

	numberOfNCCLJobsPerSize = 90
)

var (
	SingleGPURequirement = v1.ResourceRequirements{
		Limits: map[v1.ResourceName]resource.Quantity{
			constants.NvidiaGpuResource: *resource.NewQuantity(1, resource.DecimalSI),
		},
	}
	FullNodeGPURequirement = v1.ResourceRequirements{
		Limits: map[v1.ResourceName]resource.Quantity{
			constants.NvidiaGpuResource: *resource.NewQuantity(gpusPerNode, resource.DecimalSI),
		},
	}
)

func basicScaleTest(
	ctx context.Context, testCtx *testcontext.TestContext, testName string,
	testQueue *v2.Queue,
	disableSchedulerForPodCreation bool, numberOfNodes int,
) {
	GinkgoLogr.Info("Base test.", "testName", testName)

	startTime, endTime, totalNumberOfJobs := fillClusterWithJobs(ctx, testCtx, testQueue, disableSchedulerForPodCreation, numberOfNodes, SingleGPURequirement)

	GinkgoLogr.Info(
		"Scheduled pods", "Total time", endTime.Sub(startTime),
		"nodes", numberOfNodes, "jobs", totalNumberOfJobs,
	)

	Expect(writeTestResults(testName, true,
		map[string]interface{}{
			"nodes": numberOfNodes,
			"jobs":  totalNumberOfJobs,
			"time":  endTime.Sub(startTime).String(),
		})).To(Succeed())
}

func fillClusterWithJobs(
	ctx context.Context, testCtx *testcontext.TestContext,
	testQueue *v2.Queue, disableSchedulerForPodCreation bool, numberOfNodes int,
	resourceRequirements v1.ResourceRequirements,
) (startTime time.Time, endTime time.Time, totalNumberOfJobs int) {
	if disableSchedulerForPodCreation {
		schedulerconfig.DisableScheduler(ctx, testCtx)
		defer schedulerconfig.EnableScheduler(ctx, testCtx)
	} else {
		startTime = time.Now()
	}

	GinkgoLogr.Info("Creating pods")
	gpuQuantity := resourceRequirements.Limits[constants.NvidiaGpuResource]
	gpusPerJob := int(gpuQuantity.Value())
	totalNumberOfJobs = (numberOfNodes * gpusPerNode) / gpusPerJob

	submissions := make([]jobSubmission, totalNumberOfJobs)
	for i := range submissions {
		submissions[i] = singleJobSubmissionForKwok(testCtx, testQueue, resourceRequirements, nil)
	}
	tracker, err := submitJobBatch(ctx, testCtx, queue.GetConnectedNamespaceToQueue(testQueue), submissions)
	Expect(err).NotTo(HaveOccurred(), "Failed to create Job batch")
	defer tracker.Close()

	if disableSchedulerForPodCreation {
		startTime = time.Now()
		schedulerconfig.EnableScheduler(ctx, testCtx)
	}

	GinkgoLogr.Info("Waiting for pods scheduling")
	status, err := tracker.WaitForScheduled(ctx)
	Expect(err).NotTo(HaveOccurred())
	return startTime, status.LastScheduledAt, totalNumberOfJobs
}

func distributedJobsScaleTest(
	ctx context.Context, testCtx *testcontext.TestContext,
	testQueue *v2.Queue, testName string, numberOfNodes int,
) {
	gpuPerPod := int(math.Floor(math.Min(gpusPerNode, (gpusPerNode/2.0)+1)))
	numberOfDistributedJobs := numberOfNodes / defaultPodsPerDistributedJob
	distributedJobsScaleTestInternal(
		ctx, testCtx, testQueue, numberOfDistributedJobs, defaultPodsPerDistributedJob, gpuPerPod, testName, numberOfNodes,
		nil,
	)
}

func distributedJobsScaleTestInternal(
	ctx context.Context, testCtx *testcontext.TestContext,
	testQueue *v2.Queue, numberOfDistributedJobs, podsPerDistributedJob, gpuPerPod int, testName string, numberOfNodes int,
	topologyConstraint *v2alpha2.TopologyConstraint,
) {
	schedulerconfig.DisableScheduler(ctx, testCtx)
	defer schedulerconfig.EnableScheduler(ctx, testCtx)

	resources := v1.ResourceRequirements{Limits: map[v1.ResourceName]resource.Quantity{
		constants.NvidiaGpuResource: *resource.NewQuantity(int64(gpuPerPod), resource.DecimalSI),
	}}
	submissions := make([]jobSubmission, numberOfDistributedJobs)
	for i := range submissions {
		submissions[i] = distributedJobSubmissionForKwok(
			testCtx, testQueue, resources, podsPerDistributedJob, nil, topologyConstraint,
		)
	}
	tracker, err := submitJobBatch(ctx, testCtx, queue.GetConnectedNamespaceToQueue(testQueue), submissions)
	Expect(err).NotTo(HaveOccurred(), "Failed to create distributed Job batch")
	defer tracker.Close()

	startTime := time.Now()
	schedulerconfig.EnableScheduler(ctx, testCtx)

	status, err := tracker.WaitForScheduled(ctx)
	Expect(err).NotTo(HaveOccurred())
	endTime := status.LastScheduledAt

	GinkgoLogr.Info(
		"Scheduled pods", "Total time", endTime.Sub(startTime),
		"nodes", numberOfNodes, "jobs", numberOfDistributedJobs,
	)

	Expect(writeTestResults(testName, true,
		map[string]interface{}{
			"nodes":            numberOfNodes,
			"pods":             numberOfDistributedJobs * podsPerDistributedJob,
			"distributed jobs": numberOfDistributedJobs,
			"time":             endTime.Sub(startTime).String(),
		})).To(Succeed())
}

func waitForDistributedJobsForKwok(
	ctx context.Context, testCtx *testcontext.TestContext, jobs []*batchv1.Job,
) []*v1.Pod {
	if len(jobs) == 0 {
		return nil
	}

	expectedPods := 0
	for _, job := range jobs {
		if job.Spec.Parallelism == nil {
			expectedPods++
		} else {
			expectedPods += int(*job.Spec.Parallelism)
		}
	}

	batchID := jobs[0].Labels[distributedJobBatchLabel]
	selector := metav1.LabelSelector{MatchLabels: map[string]string{distributedJobBatchLabel: batchID}}
	waitutils.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, selector, expectedPods)

	Eventually(func(g Gomega) {
		podGroups := &v2alpha2.PodGroupList{}
		err := testCtx.ControllerClient.List(ctx, podGroups,
			runtimeClient.InNamespace(jobs[0].Namespace),
			runtimeClient.MatchingLabels{distributedJobBatchLabel: batchID},
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(podGroups.Items)).To(Equal(len(jobs)))
	}, maxFlowTimeoutMinutes*time.Minute, podsPollIntervalSeconds*time.Second).Should(Succeed())

	pods := &v1.PodList{}
	Expect(testCtx.ControllerClient.List(ctx, pods,
		runtimeClient.InNamespace(jobs[0].Namespace),
		runtimeClient.MatchingLabels{distributedJobBatchLabel: batchID},
	)).To(Succeed())

	result := make([]*v1.Pod, 0, len(pods.Items))
	for i := range pods.Items {
		result = append(result, &pods.Items[i])
	}
	return result
}

func consolidateScaleTest(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue, numberOfNodes int,
) {
	gpuPerPod := int(math.Floor(math.Min(gpusPerNode, (gpusPerNode/2.0)+1)))
	numberOfDistributedJobs := numberOfNodes / defaultPodsPerDistributedJob

	freeGpus := gpuPerPod * defaultPodsPerDistributedJob * numberOfDistributedJobs

	newGPUPerPod := gpuPerPod + 1
	newNumberOfDistributedJobs := freeGpus / (newGPUPerPod * defaultPodsPerDistributedJob)

	Expect(feature_flags.SetMaxConsolidationPreemptees(ctx, testCtx, numberOfNodes*gpusPerNode)).To(Succeed())
	GinkgoLogr.Info("Consolidating for jobs.", "number of distributed jobs", newNumberOfDistributedJobs, "gpus per pod", newGPUPerPod, "pods per distributed job", defaultPodsPerDistributedJob)
	distributedJobsScaleTestInternal(
		ctx, testCtx, testQueue, newNumberOfDistributedJobs, defaultPodsPerDistributedJob, newGPUPerPod,
		"Consolidation to run multiple distributed jobs", numberOfNodes,
		nil,
	)
}

func measureReclaimSingleGPUJob(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue, numberOfNodes int,
) {
	totalTime := time.Duration(0)
	for i := 0; i < statusMeasuringSamples; i++ {
		startTime := time.Now()
		_, err := createJobObjectForKwok(
			ctx, testCtx, testQueue, SingleGPURequirement,
			map[string]string{},
		)
		Expect(err).NotTo(HaveOccurred())
		scheduledTime := waitForAllJobsToSchedule(ctx, testCtx, testQueue, i+1)
		totalTime += scheduledTime.Sub(startTime)
	}
	Expect(writeTestResults(
		"Measuring reclaim time for single GPU", true,
		map[string]interface{}{
			"running jobs": numberOfNodes * gpusPerNode,
			"average time to reclaim single GPU (seconds)": totalTime.Seconds() / float64(statusMeasuringSamples),
		},
	)).To(Succeed())
}

func measureUnschedulableDelayInSeconds(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	resources v1.ResourceRequirements, numberOfPods int,
) float64 {
	totalTime := time.Duration(0)
	for range statusMeasuringSamples {
		tracker, err := submitJobBatch(ctx, testCtx, queue.GetConnectedNamespaceToQueue(testQueue), []jobSubmission{
			distributedJobSubmissionForKwok(testCtx, testQueue, resources, numberOfPods, nil, nil),
		})
		Expect(err).NotTo(HaveOccurred())
		timing, err := tracker.WaitForPodGroupCondition(ctx, v2alpha2.UnschedulableOnNodePool)
		Expect(err).NotTo(HaveOccurred())
		totalTime += timing.TransitionAt.Sub(timing.CreatedAt)

		jobs := tracker.Jobs()
		Expect(jobs).To(HaveLen(1))
		Expect(deleteObjectWithRetries(ctx, testCtx.ControllerClient, jobs[0])).To(Succeed())
		tracker.Close()
	}

	return totalTime.Seconds() / float64(statusMeasuringSamples)
}

// reclaimForOneLargeJob creates a distributed job with the specified number of pods, each requesting gpusPerNode GPUs
func reclaimForOneLargeJob(ctx context.Context, testCtx *testcontext.TestContext, reclaimSingleGPUJobsQueue *v2.Queue, numberOfPods int) {
	resources := v1.ResourceRequirements{Limits: map[v1.ResourceName]resource.Quantity{
		constants.NvidiaGpuResource: *resource.NewQuantity(int64(gpusPerNode), resource.DecimalSI),
	}}
	tracker, err := submitJobBatch(ctx, testCtx, queue.GetConnectedNamespaceToQueue(reclaimSingleGPUJobsQueue), []jobSubmission{
		distributedJobSubmissionForKwok(testCtx, reclaimSingleGPUJobsQueue, resources, numberOfPods, nil, nil),
	})
	Expect(err).NotTo(HaveOccurred())
	defer tracker.Close()
	status, err := tracker.WaitForRunning(ctx)
	Expect(err).NotTo(HaveOccurred())
	startTime, err := tracker.WaitForSinglePodGroupCreation(ctx)
	Expect(err).NotTo(HaveOccurred())
	endTime := status.LastScheduledAt

	Expect(writeTestResults(
		"Reclaim time for one very large job", true,
		map[string]interface{}{
			"total requested gpus":      float64((numberOfPods) * gpusPerNode),
			"time to reclaim (seconds)": endTime.Sub(startTime).Seconds(),
			"number of pods":            float64(status.ObservedPods),
		},
	))
}

func runNCCLSimulation(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	numberOfNodes int,
) (testSucceeded bool, totalPods int, completedPods int, pendingPods int, startTime time.Time) {
	jobSizes := []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512}
	startTime = time.Now()
	batchID := utils.GenerateRandomK8sName(10)
	podLabels := map[string]string{
		"burst-test":             "true",
		distributedJobBatchLabel: batchID,
	}
	jobLabels := map[string]string{distributedJobBatchLabel: batchID}
	var jobs []*batchv1.Job
	var creationError error
	for _, jobSize := range jobSizes {
		if jobSize > numberOfNodes {
			break
		}
		for range numberOfNCCLJobsPerSize {
			job, err := submitDistributedJobForKwok(
				ctx, testCtx, testQueue, FullNodeGPURequirement, jobSize,
				podLabels, jobLabels, nil,
			)
			if err != nil {
				creationError = errors.Join(creationError, err)
				continue
			}
			jobs = append(jobs, job)
		}
	}
	Expect(creationError).NotTo(HaveOccurred(), "Failed to create some NCCL jobs")
	testPods := waitForDistributedJobsForKwok(ctx, testCtx, jobs)

	totalPods = len(testPods)
	completedPods = 0
	pendingPods = 0

	Eventually(func(g Gomega) bool {
		queuePods := &v1.PodList{}
		g.Expect(testCtx.ControllerClient.List(ctx, queuePods,
			runtimeClient.InNamespace(queue.GetConnectedNamespaceToQueue(testQueue)),
		)).To(Succeed())

		currentCompletedPods := 0
		currentPendingPods := 0

		queuePodsByName := map[string]*v1.Pod{}
		for i := range queuePods.Items {
			pod := &queuePods.Items[i]
			queuePodsByName[pod.Name] = pod
			if pod.Status.Phase == v1.PodPending {
				currentPendingPods++
			}
		}

		for _, pod := range testPods {
			queuePod, exists := queuePodsByName[pod.Name]
			if exists && queuePod.Status.Phase == v1.PodSucceeded {
				currentCompletedPods++
			}
		}
		completedPods = currentCompletedPods
		pendingPods = currentPendingPods

		return len(testPods) == completedPods || currentPendingPods == 0
	}, time.Duration(ncclTimeoutMinutes)*time.Minute, podsPollIntervalSeconds*time.Second).Should(BeTrue())

	GinkgoLogr.Info("Finished NCCL test", "completedPods", completedPods, "len(testPods)", len(testPods), "pendingPods", pendingPods)

	testSucceeded = true

	return testSucceeded, totalPods, completedPods, pendingPods, startTime
}

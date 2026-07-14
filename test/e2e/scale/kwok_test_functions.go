// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"math"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	schedulerconfig "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
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
	podLabels := map[string]string{"burst-test": "true"}
	var submissions []jobSubmission
	for _, jobSize := range jobSizes {
		if jobSize > numberOfNodes {
			break
		}
		for range numberOfNCCLJobsPerSize {
			submissions = append(submissions, distributedJobSubmissionForKwok(
				testCtx, testQueue, FullNodeGPURequirement, jobSize, podLabels, nil,
			))
		}
	}
	tracker, err := submitJobBatch(ctx, testCtx, queue.GetConnectedNamespaceToQueue(testQueue), submissions)
	Expect(err).NotTo(HaveOccurred(), "Failed to create NCCL Job batch")
	defer tracker.Close()
	completionCtx, cancelCompletion := context.WithTimeout(ctx, time.Duration(ncclTimeoutMinutes)*time.Minute)
	defer cancelCompletion()
	status, err := waitForNCCLCompletion(completionCtx, tracker)
	Expect(err).NotTo(HaveOccurred())

	totalPods = status.ExpectedPods
	completedPods = status.SucceededPods
	pendingPods = status.PendingPods
	GinkgoLogr.Info("Finished NCCL test", "completedPods", completedPods, "totalPods", totalPods, "pendingPods", pendingPods)

	testSucceeded = true

	return testSucceeded, totalPods, completedPods, pendingPods, startTime
}

func waitForNCCLCompletion(ctx context.Context, tracker *jobBatchTracker) (BatchStatus, error) {
	return tracker.WaitForStatus(ctx, "NCCL batch completion", func(status BatchStatus) bool {
		return status.SucceededPods == status.ExpectedPods ||
			(status.ObservedPods == status.ExpectedPods && status.PendingPods == 0)
	})
}

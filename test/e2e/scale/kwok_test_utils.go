// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"fmt"
	"math"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
	"github.com/kai-scheduler/api/scheduling/v2alpha2"
)

const (
	kwokNodeAnnotationKey = "kwok.x-k8s.io/node"
	kwokNodeAnnotationVal = "fake"

	operationAttemptsRetries = 10
	retryInterval            = 100 * time.Microsecond
)

var (
	kwokTaint = v1.Taint{
		Key:    kwokNodeAnnotationKey,
		Value:  kwokNodeAnnotationVal,
		Effect: v1.TaintEffectNoSchedule,
	}
)

func deleteObjectWithRetries(
	ctx context.Context, kubeClient runtimeClient.Client,
	obj runtimeClient.Object, opts ...runtimeClient.DeleteOption) error {
	var err error
	for i := 0; i < operationAttemptsRetries; i++ {
		err = kubeClient.Delete(ctx, obj, opts...)
		if err == nil || errors.IsNotFound(err) {
			return nil
		}
		time.Sleep(retryInterval)
	}
	return err
}

func deleteAllOfWithRetries(
	ctx context.Context, kubeClient runtimeClient.Client,
	obj runtimeClient.Object, opts ...runtimeClient.DeleteAllOfOption,
) error {
	var lastErr error
	backoff := utilwait.Backoff{
		Duration: 200 * time.Millisecond,
		Factor:   2,
		Jitter:   0.2,
		Steps:    operationAttemptsRetries,
		Cap:      5 * time.Second,
	}
	err := utilwait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		lastErr = kubeClient.DeleteAllOf(ctx, obj, opts...)
		if lastErr == nil {
			return true, nil
		}
		if errors.IsTooManyRequests(lastErr) || errors.IsTimeout(lastErr) ||
			errors.IsServerTimeout(lastErr) || errors.IsServiceUnavailable(lastErr) ||
			errors.IsInternalError(lastErr) || errors.IsConflict(lastErr) {
			return false, nil
		}
		return false, lastErr
	})
	if ctx.Err() == nil && utilwait.Interrupted(err) && lastErr != nil {
		return lastErr
	}
	return err
}

func getPodScheduledTime(pod *v1.Pod) (time.Time, error) {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodScheduled && condition.Status == v1.ConditionTrue {
			return condition.LastTransitionTime.Time, nil
		}
	}

	return time.Time{}, fmt.Errorf("pod %s is not scheduled", pod.Name)
}

func addKWOKTaintsAndAffinity(podSpec *v1.PodSpec) {
	podSpec.Tolerations = []v1.Toleration{
		{
			Key:    kwokTaint.Key,
			Value:  kwokTaint.Value,
			Effect: v1.TaintEffectNoSchedule,
		},
	}
	podSpec.Affinity = &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      "type",
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{"kwok"},
							},
						},
					},
				},
			},
		},
	}
}

func cleanupTestQueue(ctx context.Context, testCtx *testcontext.TestContext, queueToClean *v2.Queue) {
	Expect(deleteTestQueueObjects(ctx, testCtx.ControllerClient, queueToClean)).To(Succeed())
}

func deleteTestQueueObjects(ctx context.Context, client runtimeClient.Client, queueToClean *v2.Queue) error {
	namespace := queue.GetConnectedNamespaceToQueue(queueToClean)
	options := []runtimeClient.DeleteAllOfOption{
		runtimeClient.InNamespace(namespace),
		runtimeClient.MatchingLabels{"app": "engine-e2e"},
	}
	jobOptions := append([]runtimeClient.DeleteAllOfOption{runtimeClient.PropagationPolicy(metav1.DeletePropagationBackground)}, options...)
	if err := deleteAllOfWithRetries(ctx, client, &batchv1.Job{}, jobOptions...); err != nil {
		return fmt.Errorf("delete jobs in %s: %w", namespace, err)
	}
	for _, object := range []runtimeClient.Object{&v1.Pod{}, &v2alpha2.PodGroup{}, &v1.Pod{}} {
		if err := deleteAllOfWithRetries(ctx, client, object, options...); err != nil {
			return fmt.Errorf("delete %T in %s: %w", object, namespace, err)
		}
	}
	return nil
}

func waitForAllJobsToSchedule(
	ctx context.Context, testCtx *testcontext.TestContext,
	testQueue *v2.Queue, expectedNumberOfPods int,
) time.Time {
	queueLabelKey := testconfig.GetConfig().QueueLabelKey
	selector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			queueLabelKey: testQueue.Name,
		},
	}
	wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, selector, expectedNumberOfPods)

	namespace := queue.GetConnectedNamespaceToQueue(testQueue)
	podsList := &v1.PodList{}

	Eventually(func(g Gomega) {
		err := testCtx.ControllerClient.List(ctx, podsList,
			runtimeClient.InNamespace(namespace),
			runtimeClient.MatchingLabels(map[string]string{
				queueLabelKey: testQueue.Name,
			}))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(podsList.Items)).To(Equal(expectedNumberOfPods))

		for _, pod := range podsList.Items {
			if !rd.IsPodScheduled(&pod) {
				GinkgoLogr.Info("Pod is not scheduled", "pod", pod.Name)
			}
			g.Expect(rd.IsPodScheduled(&pod)).To(BeTrue())
		}
	}, maxFlowTimeoutMinutes*time.Minute, podsPollIntervalSeconds*time.Second).Should(Succeed())

	lastScheduledTime, err := getPodScheduledTime(&podsList.Items[0])
	if err != nil {
		Fail(fmt.Sprintf("Expected all pods to be scheduled but pod %s is not scheduled", podsList.Items[0].Name))
	}

	for _, pod := range podsList.Items[1:] {
		scheduledTime, err := getPodScheduledTime(&pod)
		if err != nil {
			Fail(fmt.Sprintf("Expected all pods to be scheduled but pod %s is not scheduled", pod.Name))
		}
		if lastScheduledTime.Before(scheduledTime) {
			lastScheduledTime = scheduledTime
		}
	}

	return lastScheduledTime
}

func deleteJobsFromAllNodes(ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue) {
	pods := &v1.PodList{}
	err := testCtx.ControllerClient.List(ctx, pods,
		runtimeClient.InNamespace(queue.GetConnectedNamespaceToQueue(testQueue)),
		runtimeClient.MatchingLabels(map[string]string{
			testconfig.GetConfig().QueueLabelKey: testQueue.Name,
		}))
	Expect(err).NotTo(HaveOccurred())

	podsToDeletePerNode := int(math.Floor(math.Min(gpusPerNode, (gpusPerNode/2.0)+1)))
	deletedPerNode := make(map[string]int)
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			deletedOnNode, found := deletedPerNode[pod.Spec.NodeName]
			if found && deletedOnNode == podsToDeletePerNode {
				continue
			}
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pod.OwnerReferences[0].Name,
					Namespace: pod.Namespace,
				},
			}
			Expect(deleteObjectWithRetries(
				ctx, testCtx.ControllerClient, job,
				runtimeClient.PropagationPolicy(metav1.DeletePropagationBackground),
			)).To(Succeed())
			if !found {
				deletedOnNode = 0
			}
			deletedPerNode[pod.Spec.NodeName] = deletedOnNode + 1
		}
	}
}

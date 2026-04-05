/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package fillers

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"

	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

var (
	maxFillerPodsPerNode = 1000
)

func FillAllNodesWithJobs(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue, resources v1.ResourceRequirements,
	annotations map[string]string, labels map[string]string, priorityClass string, targetNodes ...string) ([]*batchv1.Job, []*v1.Pod, error) {
	var jobs []*batchv1.Job
	var jobPods []*v1.Pod

	podFillerSize, err := calcNumOfFillerPods(testCtx, resources, annotations, targetNodes...)
	if err != nil {
		return nil, nil, err
	}

	for range podFillerSize {
		job, jobPod := createFillerJob(ctx, testCtx, testQueue, resources, annotations, labels, priorityClass, targetNodes...)
		jobs = append(jobs, job)
		jobPods = append(jobPods, jobPod)
	}

	namespace := queue.GetConnectedNamespaceToQueue(testQueue)
	wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, jobPods)

	// Validate that we filled the relevant nodes to capacity
	extraJob, extraJobPod := createFillerJob(ctx, testCtx, testQueue, resources, annotations, labels, priorityClass,
		targetNodes...)
	defer rd.DeleteJob(ctx, testCtx.KubeClientset, extraJob)
	wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, extraJobPod)

	return jobs, jobPods, nil
}

func createFillerJob(ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	resources v1.ResourceRequirements, annotations, labels map[string]string, priorityClass string, targetNodes ...string) (
	*batchv1.Job, *v1.Pod) {
	namespace := queue.GetConnectedNamespaceToQueue(testQueue)

	job := rd.CreateBatchJobObject(testQueue, resources)
	if len(annotations) > 0 {
		maps.Copy(job.Spec.Template.Annotations, annotations)
	}
	if len(labels) > 0 {
		maps.Copy(job.Spec.Template.Labels, labels)
	}
	if len(targetNodes) > 0 {
		setTargetNodeAffinity(targetNodes, job)
	}
	job.Spec.Template.Spec.PriorityClassName = priorityClass
	job, err := testCtx.KubeClientset.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	Expect(err).To(Succeed())

	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			rd.BatchJobAppLabel: job.Labels[rd.BatchJobAppLabel],
		},
	}
	wait.ForAtLeastOnePodCreation(ctx, testCtx.ControllerClient, labelSelector)

	pods, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", rd.BatchJobAppLabel, job.Labels[rd.BatchJobAppLabel]),
	})
	Expect(err).To(Succeed())
	Expect(len(pods.Items)).To(BeNumerically(">", 0))

	job, err = testCtx.KubeClientset.BatchV1().Jobs(namespace).Get(ctx, job.Name, metav1.GetOptions{})
	Expect(err).To(Succeed())
	return job, &pods.Items[0]
}

func setTargetNodeAffinity(targetNodes []string, job *batchv1.Job) {
	var nodeSelectorRequirements []v1.NodeSelectorRequirement
	for _, nodeName := range targetNodes {
		nodeSelectorRequirements = append(nodeSelectorRequirements, v1.NodeSelectorRequirement{
			Key:      constant.NodeNamePodLabelName,
			Operator: v1.NodeSelectorOpIn,
			Values:   []string{nodeName},
		})
	}
	job.Spec.Template.Spec.Affinity = &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: nodeSelectorRequirements,
					},
				},
			},
		},
	}
}

func calcNumOfFillerPods(testCtx *testcontext.TestContext, resources v1.ResourceRequirements,
	annotations map[string]string, targetNodes ...string) (int, error) {
	nodesIdleResources, err := capacity.GetNodesIdleResources(testCtx.KubeClientset)
	if err != nil {
		return -1, err
	}

	podFillerSize := 0
	for nodeName, nodeIdleResources := range nodesIdleResources {
		if len(targetNodes) > 0 && !slices.Contains(targetNodes, nodeName) {
			continue
		}
		perNodeFillerPods, err := calcNumOfFillerPodsForNode(nodeIdleResources, resources, annotations)
		if err != nil {
			return -1, err
		}
		podFillerSize += perNodeFillerPods
	}
	return podFillerSize, nil
}

func calcNumOfFillerPodsForNode(
	nodeIdleResources *capacity.ResourceList, resources v1.ResourceRequirements,
	annotations map[string]string) (int, error) {
	fillerPodResources := resources.Requests
	if fillerPodResources == nil {
		fillerPodResources = resources.Limits
	}
	singleFillerPodResources := capacity.ResourcesRequestToList(fillerPodResources, annotations)
	nodeFillerResources := &capacity.ResourceList{}
	nodeFillerResources.Add(singleFillerPodResources)

	perNodeFillerPods := 0
	for nodeFillerResources.LessOrEqual(nodeIdleResources) {
		perNodeFillerPods += 1
		nodeFillerResources.Add(singleFillerPodResources)

		if perNodeFillerPods > maxFillerPodsPerNode {
			return -1, fmt.Errorf(
				"the number of filler pods (according to resource calculation) is bigger then "+
					"the max filler pods per node\n. free resources: %v, filler pod resources: %v, max filler pods: %d",
				nodeFillerResources, fillerPodResources, maxFillerPodsPerNode)
		}
	}
	return perNodeFillerPods, nil
}

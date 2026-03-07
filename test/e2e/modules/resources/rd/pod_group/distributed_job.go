/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package pod_group

import (
	"context"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
)

func CreateDistributedJob(
	ctx context.Context, clientset *kubernetes.Clientset, k8sClient runtimeClient.WithWatch,
	ownerQueue *v2.Queue, count int, resources v1.ResourceRequirements,
	priorityClassName string,
) (*v2alpha2.PodGroup, []*v1.Pod) {
	return CreatePrefixedDistributedJob(ctx, clientset, k8sClient, ownerQueue, "", count, resources, priorityClassName)
}

func CreatePrefixedDistributedJob(
	ctx context.Context, clientset *kubernetes.Clientset, k8sClient runtimeClient.WithWatch,
	ownerQueue *v2.Queue, prefix string, count int, resources v1.ResourceRequirements,
	priorityClassName string,
) (*v2alpha2.PodGroup, []*v1.Pod) {
	podGroup := Create(
		queue.GetConnectedNamespaceToQueue(ownerQueue), "distributed-pod-group"+utils.GenerateRandomK8sName(10), ownerQueue.Name)
	podGroup.Spec.PriorityClassName = priorityClassName
	podGroup.Spec.MinMember = int32(count)

	pods := []*v1.Pod{}

	Expect(k8sClient.Create(ctx, podGroup)).To(Succeed())
	for i := 0; i < count; i++ {
		pod := rd.CreatePodObject(ownerQueue, resources)
		pod.Name = prefix + "distributed-pod-" + utils.GenerateRandomK8sName(10)
		pod.Annotations[PodGroupNameAnnotation] = podGroup.Name
		pod.Labels[PodGroupNameAnnotation] = podGroup.Name
		pod.Spec.PriorityClassName = priorityClassName
		_, err := rd.CreatePod(ctx, clientset, pod)
		Expect(err).To(Succeed())
		pods = append(pods, pod)
	}

	return podGroup, pods
}

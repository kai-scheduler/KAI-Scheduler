/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package pod_group

import (
	"context"
	"regexp"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	kaiClient "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned"
	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/testconfig"
)

const (
	PodGroupNameAnnotation = "pod-group-name"
)

func Create(namespace, name, queue string) *v2alpha2.PodGroup {
	podGroup := &v2alpha2.PodGroup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "scheduling.run.ai/v2alpha2",
			Kind:       "PodGroup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
			Labels: map[string]string{
				constants.AppLabelName:               "engine-e2e",
				testconfig.GetConfig().QueueLabelKey: queue,
			},
		},
		Spec: v2alpha2.PodGroupSpec{
			MinMember: 1,
			Queue:     queue,
		},
	}
	return &*podGroup
}

func DeleteAllInNamespace(
	ctx context.Context, client runtimeClient.Client, namespace string,
) error {
	err := client.DeleteAllOf(
		ctx, &v2alpha2.PodGroup{},
		runtimeClient.InNamespace(namespace),
		runtimeClient.GracePeriodSeconds(0),
	)
	return runtimeClient.IgnoreNotFound(err)
}

func CreateWithPods(ctx context.Context, client *kubernetes.Clientset, kaiClient *kaiClient.Clientset,
	podGroupName string, q *v2.Queue, numPods int, priorityClassName *string,
	requirements v1.ResourceRequirements) (*v2alpha2.PodGroup, []*v1.Pod) {
	namespace := queue.GetConnectedNamespaceToQueue(q)
	podGroup := Create(namespace, podGroupName, q.Name)
	if priorityClassName != nil {
		podGroup.Spec.PriorityClassName = *priorityClassName
	}
	podGroup, err := kaiClient.SchedulingV2alpha2().PodGroups(namespace).Create(ctx, podGroup, metav1.CreateOptions{})
	Expect(err).To(Succeed())

	var pods []*v1.Pod
	for i := 0; i < numPods; i++ {
		pod := createPod(ctx, client, q, podGroupName, requirements)
		pods = append(pods, pod)
	}
	return podGroup, pods
}

func IsNotReadyForScheduling(event *v1.Event) bool {
	if event.Type != v1.EventTypeNormal || event.Reason != "NotReady" {
		return false
	}
	match, err := regexp.MatchString(
		"Job is not ready for scheduling. Waiting for \\d pods, currently \\d exist, \\d are gated",
		event.Message)
	Expect(err).To(Succeed())
	return match
}

func createPod(ctx context.Context, client *kubernetes.Clientset, queue *v2.Queue,
	podGroupName string, requirements v1.ResourceRequirements) *v1.Pod {
	pod := rd.CreatePodObject(queue, requirements)
	pod.Annotations[PodGroupNameAnnotation] = podGroupName
	pod.Labels[PodGroupNameAnnotation] = podGroupName
	pod, err := rd.CreatePod(ctx, client, pod)
	Expect(err).To(Succeed())
	return pod
}

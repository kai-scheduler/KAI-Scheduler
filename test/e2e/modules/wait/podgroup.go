/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package wait

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	kaiClient "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func WaitForPodGroupsToBeReady(
	ctx context.Context,
	kubeClientset *kubernetes.Clientset,
	kaiClientset *kaiClient.Clientset,
	controllerClient runtimeClient.WithWatch,
	namespace string,
	numPodGroups int) {
	Eventually(func(g Gomega) {
		podGroups, err := kaiClientset.SchedulingV2alpha2().PodGroups(namespace).List(
			ctx, metav1.ListOptions{},
		)
		g.Expect(err).To(Succeed())
		g.Expect(len(podGroups.Items)).To(Equal(numPodGroups),
			"Couldn't find podgroups for the pods")
	}).WithPolling(time.Second).WithTimeout(time.Minute)

	// wait for pods to be connected to the pod groups
	ForPodsWithCondition(ctx, controllerClient, func(watch.Event) bool {
		pods, err := kubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		Expect(err).To(Succeed())
		connectedPodsCounter := 0
		for _, pod := range pods.Items {
			if _, found := pod.Annotations[constants.PodGroupAnnotationForPod]; found {
				connectedPodsCounter += 1
			}
		}
		return connectedPodsCounter == numPodGroups
	})
}

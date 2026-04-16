/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package wait

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func WaitForPodGroupToExist(
	ctx context.Context,
	client runtimeClient.WithWatch,
	namespace, name string,
) {
	var lastErr error
	Eventually(func(g Gomega) bool {
		var podGroup v2alpha2.PodGroup
		lastErr = client.Get(ctx, runtimeClient.ObjectKey{Name: name, Namespace: namespace}, &podGroup)
		return lastErr == nil
	}).WithPolling(time.Second).WithTimeout(time.Minute).Should(BeTrue(), "Failed to find PodGroup", namespace, name, lastErr)
}

func WaitForPodGroupsToBeReady(
	ctx context.Context,
	controllerClient runtimeClient.WithWatch,
	namespace string,
	numPodGroups int) {
	Eventually(func(g Gomega) {
		var podGroups v2alpha2.PodGroupList
		err := controllerClient.List(ctx, &podGroups, runtimeClient.InNamespace(namespace))
		g.Expect(err).To(Succeed())
		g.Expect(len(podGroups.Items)).To(Equal(numPodGroups),
			"Couldn't find podgroups for the pods")
	}).WithPolling(time.Second).WithTimeout(time.Minute)

	// wait for pods to be connected to the pod groups
	ForPodsWithCondition(ctx, controllerClient, func(watch.Event) bool {
		var pods v1.PodList
		err := controllerClient.List(ctx, &pods, runtimeClient.InNamespace(namespace))
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

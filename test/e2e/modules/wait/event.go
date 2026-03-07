/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package wait

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait/watcher"
)

type checkEventCondition func(event *v1.Event) bool

func WaitForEventInNamespaceAndPod(ctx context.Context, client runtimeClient.WithWatch,
	namespace string, podName string, checkCondition checkEventCondition) bool {

	condition := func(event watch.Event) bool {
		events, ok := event.Object.(*v1.EventList)
		if !ok {
			return false
		}

		for _, e := range events.Items {
			// Check if the event is associated with the specific pod name
			if (podName == "" || e.InvolvedObject.Name == podName) && checkCondition(&e) {
				return true
			}
		}
		return false
	}

	w := watcher.NewGenericWatcher[v1.EventList](client, condition,
		runtimeClient.InNamespace(namespace))
	if !watcher.ForEvent(ctx, client, w) {
		if podName != "" {
			Fail(fmt.Sprintf("Failed to watch events for pod %s in namespace %s", podName, namespace))
		} else {
			Fail(fmt.Sprintf("Failed to watch events in namespace %s", namespace))
		}
	}
	return true
}

func WaitForEventInNamespace(ctx context.Context, client runtimeClient.WithWatch,
	namespace string, checkCondition checkEventCondition) bool {

	return WaitForEventInNamespaceAndPod(ctx, client, namespace, "", checkCondition)
}

func ForPodGroupNotReadyEvent(ctx context.Context, client runtimeClient.WithWatch, namespace, name string) {
	if !WaitForEventInNamespace(ctx, client, namespace, pod_group.IsNotReadyForScheduling) {
		Fail(fmt.Sprintf("Failed to wait for podGroup %s/%s to be marked as NotReady for scheduling",
			namespace, name))
	}
}

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait/watcher"
)

const (
	resourceReservationNamespace = "kai-resource-reservation"
)

type checkCondition func(watch.Event) bool
type checkPodCondition func(*v1.Pod) bool

func podEventCheck(condition checkPodCondition) func(event watch.Event) bool {
	return func(event watch.Event) bool {
		pod, ok := event.Object.(*v1.Pod)
		if !ok {
			return false
		}
		return condition(pod)
	}
}

func ForAtLeastNPodsScheduled(ctx context.Context, client runtimeClient.WithWatch, namespace string,
	pods []*v1.Pod, minRequired int) {
	pw := watcher.NewPodsWatcher(client, podEventCheck(rd.IsPodScheduled), namespace, pods, minRequired)
	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("Failed to wait for at least %d pods to be scheduled in namespace %s",
			minRequired, namespace))
	}
}

func ForAtLeastNPodsUnschedulable(ctx context.Context, client runtimeClient.WithWatch, namespace string,
	pods []*v1.Pod, minRequired int) {
	pw := watcher.NewPodsWatcher(client, podEventCheck(rd.IsPodUnschedulable), namespace, pods, minRequired)
	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("Failed to wait for at least %d pods to be unschedulable in namespace %s",
			minRequired, namespace))
	}
}

func ForPodsScheduled(ctx context.Context, client runtimeClient.WithWatch, namespace string, pods []*v1.Pod) {
	pw := watcher.NewPodsWatcher(client, podEventCheck(rd.IsPodScheduled), namespace, pods, len(pods))
	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("Failed to wait for pods to be scheduled in namespace %s", namespace))
	}
}

func ForPodsReady(ctx context.Context, client runtimeClient.WithWatch, namespace string, pods []*v1.Pod) {
	pw := watcher.NewPodsWatcher(client, podEventCheck(rd.IsPodReady), namespace, pods, len(pods))
	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("Failed to wait for pods to be ready in namespace %s", namespace))
	}
}

func ForPodScheduled(ctx context.Context, client runtimeClient.WithWatch, pod *v1.Pod) {
	pw := watcher.NewPodsWatcher(client, podEventCheck(rd.IsPodScheduled), pod.Namespace, []*v1.Pod{pod}, 1)
	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("Failed to wait for pod %s/%s to be scheduled", pod.Namespace, pod.Name))
	}
}

func ForPodReady(ctx context.Context, client runtimeClient.WithWatch, pod *v1.Pod) {
	pw := watcher.NewPodsWatcher(client, podEventCheck(rd.IsPodReady), pod.Namespace, []*v1.Pod{pod}, 1)
	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("Failed to wait for pod %s/%s to be ready", pod.Namespace, pod.Name))
	}
}

func ForPodUnschedulable(ctx context.Context, client runtimeClient.WithWatch, pod *v1.Pod) {
	pw := watcher.NewPodsWatcher(client, podEventCheck(rd.IsPodUnschedulable), pod.Namespace, []*v1.Pod{pod}, 1)
	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("Failed to wait for pod %s/%s to be unschedulable", pod.Namespace, pod.Name))
	}
}

func ForPodSucceededOrError(ctx context.Context, client runtimeClient.WithWatch, pod *v1.Pod) {
	pw := watcher.NewPodsWatcher(client, podEventCheck(rd.IsPodEnded), pod.Namespace, []*v1.Pod{pod}, 1)
	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("Failed to wait for pod %s/%s to finish (success or error)", pod.Namespace, pod.Name))
	}
}

func ForAtLeastOnePodCreation(ctx context.Context, client runtimeClient.WithWatch, selector metav1.LabelSelector) {
	ForAtLeastNPodCreation(ctx, client, selector, 1)
}

func ForAtLeastNPodCreation(ctx context.Context, client runtimeClient.WithWatch, selector metav1.LabelSelector, n int) {
	labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		Fail(fmt.Sprintf("Failed to convert label selector: %v", err))
	}
	condition := func(event watch.Event) bool {
		podsListObj, ok := event.Object.(*v1.PodList)
		if !ok {
			return false
		}
		return len(podsListObj.Items) >= n
	}
	pw := watcher.NewGenericWatcher[v1.PodList](client, condition, runtimeClient.MatchingLabelsSelector{Selector: labelSelector})
	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("Failed to watch for %d pods creation with selector <%v>", n, selector))
	}
}

func ForPodsWithCondition(
	ctx context.Context, client runtimeClient.WithWatch, checkCondition checkCondition,
	listOptions ...runtimeClient.ListOption) {
	condition := func(event watch.Event) bool {
		_, ok := event.Object.(*v1.PodList)
		if !ok {
			return false
		}
		return checkCondition(event)
	}
	pw := watcher.NewGenericWatcher[v1.PodList](client, condition, listOptions...)
	if !watcher.ForEvent(ctx, client, pw) {
		Fail("Failed to watch pods for condition")
	}
}

func ForPodsToBeDeleted(ctx context.Context, client runtimeClient.WithWatch, listOptions ...runtimeClient.ListOption) {
	condition := func(event watch.Event) bool {
		podsListObj, ok := event.Object.(*v1.PodList)
		if !ok {
			return false
		}
		return len(podsListObj.Items) == 0
	}
	pw := watcher.NewGenericWatcher[v1.PodList](client, condition, listOptions...)
	if !watcher.ForEvent(ctx, client, pw) {
		Fail("Failed to wait for pods to be deleted")
	}
}

func ForNoE2EPods(ctx context.Context, client runtimeClient.WithWatch) {
	ForPodsToBeDeleted(ctx, client, runtimeClient.MatchingLabels{constants.AppLabelName: "engine-e2e"})
}

func ForNoReservationPods(ctx context.Context, client runtimeClient.WithWatch) {
	ForPodsToBeDeleted(ctx, client, runtimeClient.InNamespace(resourceReservationNamespace))
}

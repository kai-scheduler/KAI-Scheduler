// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"slices"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	kubeaischedulerschema "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned/scheme"
	enginev2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

func TestEvictEventNotStarvedByRepeatedEvents(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	broadcaster := newEventBroadcaster()
	defer broadcaster.Shutdown()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	recorder := broadcaster.NewRecorder(kubeaischedulerschema.Scheme, v1.EventSource{Component: "kai-scheduler"})

	podGroup := &enginev2alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "ns", UID: "uid"}}
	for range 30 {
		recorder.Event(podGroup, v1.EventTypeNormal, "Unschedulable", "no nodes with enough resources")
	}
	recorder.Event(podGroup, v1.EventTypeNormal, "Evict", "Pod ns/p was preempted")

	NewWithT(t).Eventually(func() bool {
		events, _ := kubeClient.CoreV1().Events("ns").List(context.Background(), metav1.ListOptions{})
		return slices.ContainsFunc(events.Items, func(e v1.Event) bool { return e.Reason == "Evict" })
	}, 5*time.Second, 10*time.Millisecond).Should(BeTrue(), "Evict event was dropped")
}

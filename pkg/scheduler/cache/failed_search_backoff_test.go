// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	kubeaischedulerfake "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	enginev2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
)

// A failed search recorded through the cache puts the job in a backoff for that action in the
// following snapshots, until it is cleared.
func TestSnapshotMarksJobsInFailedSearchBackoff(t *testing.T) {
	g := NewWithT(t)
	kubeClient := fake.NewSimpleClientset(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pending-pod", Namespace: "ns", UID: "pending-pod",
			Annotations: map[string]string{commonconstants.PodGroupAnnotationForPod: "job"}},
		Status: v1.PodStatus{Phase: v1.PodPending},
	})
	kaiClient := kubeaischedulerfake.NewSimpleClientset(
		&enginev2.Queue{ObjectMeta: metav1.ObjectMeta{Name: "department"}, Spec: enginev2.QueueSpec{Resources: &enginev2.QueueResources{}}},
		&enginev2.Queue{ObjectMeta: metav1.ObjectMeta{Name: "queue"}, Spec: enginev2.QueueSpec{ParentQueue: "department"}},
		&enginev2alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{Name: "job", Namespace: "ns", UID: types.UID("job")},
			Spec: enginev2alpha2.PodGroupSpec{Queue: "queue"}},
	)
	schedulerCache, err := New(&SchedulerCacheParams{
		KubeClient:            kubeClient,
		KAISchedulerClient:    kaiClient,
		NodePoolParams:        &conf.SchedulingNodePoolParams{},
		FullHierarchyFairness: true,
		DiscoveryClient:       kubeClient.Discovery(),
	})
	g.Expect(err).NotTo(HaveOccurred())
	stopCh := make(chan struct{})
	defer close(stopCh)
	schedulerCache.Run(stopCh)
	schedulerCache.WaitForCacheSync(stopCh)

	jobID := common_info.NewPodGroupID("ns", "job")
	backoffUntil := func() map[string]time.Time {
		snapshot, err := schedulerCache.Snapshot()
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(snapshot.PodGroupInfos).To(HaveKey(jobID))
		return snapshot.PodGroupInfos[jobID].SearchBackoffUntil
	}

	g.Expect(backoffUntil()).To(BeEmpty())

	snapshot, err := schedulerCache.Snapshot()
	g.Expect(err).NotTo(HaveOccurred())
	schedulerCache.RecordFailedSearch(reclaim, snapshot.PodGroupInfos[jobID], time.Minute, time.Minute)
	g.Expect(backoffUntil()).To(HaveKey(reclaim))
	g.Expect(backoffUntil()).NotTo(HaveKey(preempt))

	schedulerCache.ClearFailedSearch(reclaim, snapshot.PodGroupInfos[jobID])
	g.Expect(backoffUntil()).To(BeEmpty())
}

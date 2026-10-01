// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	faketesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	kubeaischedulerfake "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	enginev2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
)

// A pod evicted for a job marks that job, in the following snapshots, until the pod has terminated;
// an eviction that was not for a job (e.g. a stale gang) marks none.
func TestEvictedPodsMarkTheJobTheyWereEvictedFor(t *testing.T) {
	g := NewWithT(t)
	pod := func(name, podGroup, node string) *v1.Pod {
		p := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", UID: types.UID(name),
				Annotations: map[string]string{commonconstants.PodGroupAnnotationForPod: podGroup}},
			Spec:   v1.PodSpec{NodeName: node},
			Status: v1.PodStatus{Phase: v1.PodPending},
		}
		if node != "" {
			p.Status.Phase = v1.PodRunning
		}
		return p
	}
	podGroup := func(name string) *enginev2alpha2.PodGroup {
		return &enginev2alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", UID: types.UID(name)},
			Spec: enginev2alpha2.PodGroupSpec{Queue: "queue"}}
	}

	kubeClient := fake.NewSimpleClientset(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		pod("victim", "victim-job", "node-1"), pod("stale", "stale-job", "node-1"), pod("preemptor-pod", "preemptor", ""))
	// A deleted pod stays terminating, as it does during its grace period.
	podsResource := v1.SchemeGroupVersion.WithResource("pods")
	kubeClient.PrependReactor("delete", "pods", func(action faketesting.Action) (bool, runtime.Object, error) {
		obj, err := kubeClient.Tracker().Get(podsResource, action.GetNamespace(), action.(faketesting.DeleteAction).GetName())
		if err != nil {
			return true, nil, err
		}
		terminating := obj.(*v1.Pod).DeepCopy()
		terminating.DeletionTimestamp = ptr.To(metav1.Now())
		return true, nil, kubeClient.Tracker().Update(podsResource, terminating, action.GetNamespace())
	})
	kaiClient := kubeaischedulerfake.NewSimpleClientset(
		&enginev2.Queue{ObjectMeta: metav1.ObjectMeta{Name: "department"}, Spec: enginev2.QueueSpec{Resources: &enginev2.QueueResources{}}},
		&enginev2.Queue{ObjectMeta: metav1.ObjectMeta{Name: "queue"}, Spec: enginev2.QueueSpec{ParentQueue: "department"}},
		podGroup("victim-job"), podGroup("stale-job"), podGroup("preemptor"))

	cache, err := New(&SchedulerCacheParams{
		KubeClient:            kubeClient,
		KAISchedulerClient:    kaiClient,
		NodePoolParams:        &conf.SchedulingNodePoolParams{},
		FullHierarchyFairness: true,
		DiscoveryClient:       kubeClient.Discovery(),
	})
	g.Expect(err).NotTo(HaveOccurred())
	stopCh := make(chan struct{})
	defer close(stopCh)
	cache.Run(stopCh)
	cache.WaitForCacheSync(stopCh)

	snapshot, err := cache.Snapshot()
	g.Expect(err).NotTo(HaveOccurred())
	evict := func(podName, jobName string, preemptor *types.NamespacedName) {
		job := snapshot.PodGroupInfos[common_info.NewPodGroupID("ns", jobName)]
		g.Expect(job).NotTo(BeNil())
		task := job.GetAllPodsMap()[common_info.PodID(podName)]
		g.Expect(task).NotTo(BeNil())
		g.Expect(cache.Evict(task.Pod, job, eviction_info.EvictionMetadata{Preemptor: preemptor}, "")).To(Succeed())
		cache.WaitForWorkers(stopCh)
	}
	terminatingSnapshot := func(podName string) *api.ClusterInfo {
		var terminating *api.ClusterInfo
		g.Eventually(func() bool {
			s, err := cache.Snapshot()
			g.Expect(err).NotTo(HaveOccurred())
			terminating = s
			return s.Pods != nil && hasDeletionTimestamp(s, podName)
		}).WithTimeout(5 * time.Second).Should(BeTrue())
		return terminating
	}

	evict("stale", "stale-job", nil)
	for _, job := range terminatingSnapshot("stale").PodGroupInfos {
		g.Expect(job.HasTerminatingVictims).To(BeFalse(), "job %s", job.Name)
	}

	evict("victim", "victim-job", &types.NamespacedName{Namespace: "ns", Name: "preemptor"})
	marked := terminatingSnapshot("victim")
	g.Expect(marked.PodGroupInfos[common_info.NewPodGroupID("ns", "preemptor")].HasTerminatingVictims).To(BeTrue())
	g.Expect(marked.PodGroupInfos[common_info.NewPodGroupID("ns", "victim-job")].HasTerminatingVictims).To(BeFalse())
}

func hasDeletionTimestamp(snapshot *api.ClusterInfo, podName string) bool {
	for _, pod := range snapshot.Pods {
		if pod.Name == podName {
			return pod.DeletionTimestamp != nil
		}
	}
	return false
}

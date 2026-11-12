// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package env_tests

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/xyproto/randomstring"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"

	kaiclient "github.com/kai-scheduler/api/client/clientset/versioned"
	schedulingv2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/status_updater"
)

type statusPutCounter struct {
	next http.RoundTripper
	puts atomic.Int32
}

func (c *statusPutCounter) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPut && strings.HasSuffix(req.URL.Path, "/podgroups/"+podGroupStatusPatchName+"/status") {
		c.puts.Add(1)
	}
	return c.next.RoundTrip(req)
}

const (
	podGroupStatusPatchName = "status-patch-pg"
	statusPatchNodePoolKey  = "kai.scheduler/node-pool"
)

var _ = Describe("PodGroup status patch", func() {
	var (
		namespace   string
		kaiClient   kaiclient.Interface
		counter     *statusPutCounter
		stopUpdater chan struct{}
		updater     interface {
			RecordJobStatusEvent(*podgroup_info.PodGroupInfo, func(*podgroup_info.PodGroupInfo, *pod_info.PodInfo) ([]*common_info.TasksFitError, error)) error
		}
		pod *corev1.Pod
	)

	BeforeEach(func(ctx context.Context) {
		namespace = "test-" + randomstring.HumanFriendlyEnglishString(10)
		Expect(ctrlClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})).To(Succeed())

		counter = &statusPutCounter{}
		countingCfg := rest.CopyConfig(cfg)
		countingCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			counter.next = rt
			return counter
		})
		var err error
		kaiClient, err = kaiclient.NewForConfig(countingCfg)
		Expect(err).To(Succeed())
		kubeClient, err := kubernetes.NewForConfig(cfg)
		Expect(err).To(Succeed())

		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "pending-pod",
				Namespace:   namespace,
				Annotations: map[string]string{"pod-group-name": podGroupStatusPatchName},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "busybox"}}},
		}
		Expect(ctrlClient.Create(ctx, pod)).To(Succeed())

		su := status_updater.New(kubeClient, kaiClient, record.NewFakeRecorder(100), 2, false, statusPatchNodePoolKey)
		updater = su
		stopUpdater = make(chan struct{})
		go su.Run(stopUpdater)
	})

	AfterEach(func() {
		close(stopUpdater)
	})

	newPodGroup := func(ctx context.Context) *schedulingv2alpha2.PodGroup {
		pg := &schedulingv2alpha2.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podGroupStatusPatchName,
				Namespace: namespace,
				Labels:    map[string]string{statusPatchNodePoolKey: "pool-a"},
			},
			Spec: schedulingv2alpha2.PodGroupSpec{Queue: "test-queue", MinMember: ptr.To(int32(1))},
		}
		Expect(ctrlClient.Create(ctx, pg)).To(Succeed())
		return pg
	}

	recordVerdict := func(snapshot *schedulingv2alpha2.PodGroup) {
		vectorMap := resource_info.NewResourceVectorMap()
		job := podgroup_info.NewPodGroupInfoWithVectorMap(common_info.PodGroupID(snapshot.Name), vectorMap)
		job.SetPodGroup(snapshot)
		job.AddTaskInfo(pod_info.NewTaskInfo(pod, vectorMap))
		Expect(updater.RecordJobStatusEvent(job, nil)).To(Succeed())
	}

	It("writes the first verdict when the controller wrote status first", func(ctx context.Context) {
		pg := newPodGroup(ctx)
		snapshot := pg.DeepCopy()

		live := pg.DeepCopy()
		live.Status.ResourcesStatus.Allocated = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}
		Expect(ctrlClient.Status().Update(ctx, live)).To(Succeed())

		recordVerdict(snapshot)

		Eventually(func(g Gomega) {
			got := &schedulingv2alpha2.PodGroup{}
			g.Expect(ctrlClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podGroupStatusPatchName}, got)).To(Succeed())
			g.Expect(got.Status.SchedulingConditions).To(HaveLen(1))
			g.Expect(got.Status.ResourcesStatus.Allocated).To(HaveKey(corev1.ResourceCPU))
		}, 10*time.Second, 50*time.Millisecond).Should(Succeed())
	})

	It("keeps patching across consecutive cycles", func(ctx context.Context) {
		pg := newPodGroup(ctx)
		live := pg.DeepCopy()
		live.Status.ResourcesStatus.Allocated = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}
		Expect(ctrlClient.Status().Update(ctx, live)).To(Succeed())

		for cycle := 1; cycle <= 4; cycle++ {
			snapshot := &schedulingv2alpha2.PodGroup{}
			Expect(ctrlClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podGroupStatusPatchName}, snapshot)).To(Succeed())
			snapshot.Status.SchedulingConditions = nil
			Expect(ctrlClient.Status().Update(ctx, snapshot)).To(Succeed())
			recordVerdict(snapshot.DeepCopy())

			Eventually(func(g Gomega) {
				got := &schedulingv2alpha2.PodGroup{}
				g.Expect(ctrlClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podGroupStatusPatchName}, got)).To(Succeed())
				g.Expect(got.ResourceVersion).NotTo(Equal(snapshot.ResourceVersion))
			}, 10*time.Second, 50*time.Millisecond).Should(Succeed())
		}

		Expect(counter.puts.Load()).To(BeZero())
	})
})

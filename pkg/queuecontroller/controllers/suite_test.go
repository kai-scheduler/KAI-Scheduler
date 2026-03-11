/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/queuecontroller/metrics"
)

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
)

var (
	cfg         *rest.Config
	k8sClient   client.Client
	testEnv     *envtest.Environment
	ctx         context.Context
	cancel      context.CancelFunc
	managerDone chan struct{}
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "deployments", "kai-scheduler", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = v2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = v2alpha2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("QueueController", Ordered, func() {
	var (
		mgr        ctrl.Manager
		controller *QueueReconciler
	)

	BeforeAll(func() {
		ctx, cancel = context.WithCancel(context.Background())

		metrics.InitMetrics("testns",
			map[string]string{"priority": "queue_priority", "some-other-label": "some_other_label"},
			map[string]string{"priority": "normal"},
		)

		var err error
		mgr, err = ctrl.NewManager(cfg, ctrl.Options{
			Scheme: scheme.Scheme,
			Metrics: metricsserver.Options{
				BindAddress: "0",
			},
		})
		Expect(err).ToNot(HaveOccurred())

		controller = &QueueReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}

		err = controller.SetupWithManager(mgr, false)
		Expect(err).ToNot(HaveOccurred())

		managerDone = make(chan struct{})
		go func() {
			defer close(managerDone)
			defer GinkgoRecover()
			err := mgr.Start(ctx)
			Expect(err).ToNot(HaveOccurred())
		}()
	})

	AfterAll(func() {
		cancel()
		<-managerDone
	})

	Context("When managing child queues", func() {
		AfterAll(func() {
			deleteQueue(ctx, k8sClient, "child-queue-1")
			deleteQueue(ctx, k8sClient, "child-queue-2")
			deleteQueue(ctx, k8sClient, "parent-queue")
		})

		It("Should update parent queue's childQueues field", func() {
			parentQueue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "parent-queue",
				},
				Spec: v2.QueueSpec{},
			}
			Expect(k8sClient.Create(ctx, parentQueue)).Should(Succeed())

			childQueue1 := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "child-queue-1",
				},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
				},
			}
			Expect(k8sClient.Create(ctx, childQueue1)).Should(Succeed())

			childQueue2 := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "child-queue-2",
				},
				Spec: v2.QueueSpec{
					ParentQueue: "parent-queue",
				},
			}
			Expect(k8sClient.Create(ctx, childQueue2)).Should(Succeed())

			Eventually(func() []string {
				var updatedParentQueue v2.Queue
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "parent-queue"}, &updatedParentQueue)
				if err != nil {
					return nil
				}
				return updatedParentQueue.Status.ChildQueues
			}, timeout, interval).Should(ContainElements("child-queue-1", "child-queue-2"))
		})
	})

	Context("When managing pod groups", func() {
		AfterAll(func() {
			deleteQueue(ctx, k8sClient, "resource-queue")
		})

		It("Should update queue status with pod group resources", func() {
			queue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resource-queue",
				},
				Spec: v2.QueueSpec{},
			}
			Expect(k8sClient.Create(ctx, queue)).Should(Succeed())

			podGroup1 := &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-group-1",
					Namespace: "default",
					Labels: map[string]string{
						"kai.scheduler/queue": "resource-queue",
					},
				},
				Spec: v2alpha2.PodGroupSpec{
					Queue:     "resource-queue",
					MinMember: 1,
				},
			}
			Expect(k8sClient.Create(ctx, podGroup1)).Should(Succeed())

			podGroup2 := &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-group-2",
					Namespace: "default",
					Labels: map[string]string{
						"kai.scheduler/queue": "resource-queue",
					},
				},
				Spec: v2alpha2.PodGroupSpec{
					Queue:     "resource-queue",
					MinMember: 1,
				},
			}
			Expect(k8sClient.Create(ctx, podGroup2)).Should(Succeed())

			createdPodGroup1 := &v2alpha2.PodGroup{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "pod-group-1", Namespace: "default"}, createdPodGroup1)).Should(Succeed())

			createdPodGroup2 := &v2alpha2.PodGroup{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "pod-group-2", Namespace: "default"}, createdPodGroup2)).Should(Succeed())

			createdPodGroup1.Status = v2alpha2.PodGroupStatus{
				Running: 1,
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: v1.ResourceList{
						"cpu":    resource.MustParse("2"),
						"memory": resource.MustParse("4Gi"),
					},
					AllocatedNonPreemptible: v1.ResourceList{
						"cpu":    resource.MustParse("1"),
						"memory": resource.MustParse("2Gi"),
					},
					Requested: v1.ResourceList{
						"cpu":    resource.MustParse("2"),
						"memory": resource.MustParse("4Gi"),
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, createdPodGroup1)).Should(Succeed())

			createdPodGroup2.Status = v2alpha2.PodGroupStatus{
				Running: 1,
				ResourcesStatus: v2alpha2.PodGroupResourcesStatus{
					Allocated: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("2"),
						"cpu":            resource.MustParse("3"),
						"memory":         resource.MustParse("6Gi"),
					},
					AllocatedNonPreemptible: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
						"cpu":            resource.MustParse("2"),
						"memory":         resource.MustParse("4Gi"),
					},
					Requested: v1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("2"),
						"cpu":            resource.MustParse("3"),
						"memory":         resource.MustParse("6Gi"),
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, createdPodGroup2)).Should(Succeed())

			Eventually(func(q gomega.Gomega) bool {
				var updatedQueue v2.Queue
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "resource-queue"}, &updatedQueue)
				if err != nil {
					return false
				}
				// GinkgoLogr.Info("EREZ TEST", "allocated", updatedQueue.Status.Allocated)
				q.Expect(updatedQueue.Status.Allocated["cpu"]).To(Equal(resource.MustParse("5")))
				q.Expect(updatedQueue.Status.Allocated["memory"]).To(Equal(resource.MustParse("10Gi")))
				q.Expect(updatedQueue.Status.Allocated["nvidia.com/gpu"]).To(Equal(resource.MustParse("2")))

				q.Expect(updatedQueue.Status.AllocatedNonPreemptible["cpu"]).To(Equal(resource.MustParse("3")))
				q.Expect(updatedQueue.Status.AllocatedNonPreemptible["memory"]).To(Equal(resource.MustParse("6Gi")))
				q.Expect(updatedQueue.Status.AllocatedNonPreemptible["nvidia.com/gpu"]).To(Equal(resource.MustParse("1")))

				q.Expect(updatedQueue.Status.Requested["cpu"]).To(Equal(resource.MustParse("5")))
				q.Expect(updatedQueue.Status.Requested["memory"]).To(Equal(resource.MustParse("10Gi")))
				q.Expect(updatedQueue.Status.Requested["nvidia.com/gpu"]).To(Equal(resource.MustParse("2")))

				labels := []string{"resource-queue", "normal", ""}
				expectMetricValue(q, metrics.GetQueueAllocatedGPUsMetric(), labels, 2)
				expectMetricValue(q, metrics.GetQueueAllocatedCPUMetric(), labels, 5)
				expectMetricValue(q, metrics.GetQueueAllocatedMemoryMetric(), labels, 10737418240)
				return true
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When setting queue metrics", func() {
		AfterAll(func() {
			deleteQueue(ctx, k8sClient, "test-queue")
		})

		It("should set metrics for a queue with resources", func() {
			queue := &v2.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-queue",
					Labels: map[string]string{"priority": "high"},
				},
				Spec: v2.QueueSpec{
					Resources: &v2.QueueResources{
						GPU:    v2.QueueResource{Quota: 2},
						CPU:    v2.QueueResource{Quota: 2000},
						Memory: v2.QueueResource{Quota: 4},
					},
				},
			}
			Expect(k8sClient.Create(ctx, queue)).Should(Succeed())

			labels := []string{"test-queue", "high", ""}

			Eventually(func(q gomega.Gomega) {
				expectMetricValue(q, metrics.GetQueueInfoMetric(), labels, 1)
				expectMetricValue(q, metrics.GetQueueDeservedGPUsMetric(), labels, 2)
				expectMetricValue(q, metrics.GetQueueQuotaCPUMetric(), labels, 2)
				expectMetricValue(q, metrics.GetQueueQuotaMemoryMetric(), labels, 4000000)
			}, timeout, interval).Should(Succeed())

			// delete the queue and expect metrics to be deleted
			Expect(k8sClient.Delete(ctx, queue)).Should(Succeed())

			Eventually(func(q gomega.Gomega) {
				gathered := testutil.CollectAndCount(metrics.GetQueueInfoMetric())
				q.Expect(gathered).To(Equal(0))
				gathered = testutil.CollectAndCount(metrics.GetQueueDeservedGPUsMetric())
				q.Expect(gathered).To(Equal(0))
				gathered = testutil.CollectAndCount(metrics.GetQueueQuotaCPUMetric())
				q.Expect(gathered).To(Equal(0))
				gathered = testutil.CollectAndCount(metrics.GetQueueQuotaMemoryMetric())
				q.Expect(gathered).To(Equal(0))
				gathered = testutil.CollectAndCount(metrics.GetQueueAllocatedGPUsMetric())
				q.Expect(gathered).To(Equal(0))
				gathered = testutil.CollectAndCount(metrics.GetQueueAllocatedCPUMetric())
				q.Expect(gathered).To(Equal(0))
				gathered = testutil.CollectAndCount(metrics.GetQueueAllocatedMemoryMetric())
				q.Expect(gathered).To(Equal(0))
			}, timeout, interval).Should(Succeed())
		})
	})
})

func expectMetricValue(q gomega.Gomega, gauge *prometheus.GaugeVec, labels []string, expected float64) {
	metricGauge, err := gauge.GetMetricWithLabelValues(labels...)
	q.ExpectWithOffset(1, err).To(BeNil())
	q.ExpectWithOffset(1, metricGauge).ToNot(BeNil())
	q.ExpectWithOffset(1, testutil.ToFloat64(metricGauge)).To(BeEquivalentTo(expected))
}

func deleteQueue(ctx context.Context, k8sClient client.Client, queueName string) {
	queueObj := &v2.Queue{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: queueName}, queueObj)
	if err != nil {
		return
	}

	_ = k8sClient.Delete(ctx, queueObj)
}

/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package workload

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

const (
	pgWaitTimeout = 30 * time.Second
	pgPollTick    = 500 * time.Millisecond
)

// DescribeWorkloadSpecs returns the Ginkgo suite that exercises KAI's
// translation of the upstream scheduling.k8s.io/v1alpha1 Workload API
// (KEP-4671) into KAI PodGroups. The suite skips itself on clusters that do
// not expose the API, so it is safe to include in a default e2e run.
func DescribeWorkloadSpecs() bool {
	return Describe("Workload API", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
			testQ   *v2.Queue
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			skipIfWorkloadAPIUnavailable(ctx, testCtx)

			testQ = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			testCtx.InitQueues([]*v2.Queue{testQ})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		It("creates a KAI PodGroup with MinMember=gang.minCount for a Gang-policy Workload", func(ctx context.Context) {
			wlName := "gang-" + rand.String(6)
			ns := queue.GetConnectedNamespaceToQueue(testQ)
			wl := &schedulingv1alpha1.Workload{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: wlName},
				Spec: schedulingv1alpha1.WorkloadSpec{
					PodGroups: []schedulingv1alpha1.PodGroup{{
						Name: "workers",
						Policy: schedulingv1alpha1.PodGroupPolicy{
							Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 2},
						},
					}},
				},
			}
			Expect(testCtx.ControllerClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				_ = testCtx.ControllerClient.Delete(ctx, wl)
			})

			pod := rd.CreatePodObject(testQ, corev1.ResourceRequirements{})
			pod.Spec.WorkloadRef = &corev1.WorkloadReference{
				Name: wlName, PodGroup: "workers", PodGroupReplicaKey: "0",
			}
			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			expected := fmt.Sprintf("%s-workers-0", wlName)
			pg := waitForPodGroup(ctx, testCtx, ns, expected)
			Expect(pg.Spec.MinMember).NotTo(BeNil())
			Expect(*pg.Spec.MinMember).To(Equal(int32(2)))
			Expect(pg.Spec.SubGroups).To(BeEmpty(), "Workload API ignores SubGroups")
		})

		It("collapses replica keys into a single KAI PodGroup for a Basic-policy Workload", func(ctx context.Context) {
			wlName := "basic-" + rand.String(6)
			ns := queue.GetConnectedNamespaceToQueue(testQ)
			wl := &schedulingv1alpha1.Workload{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: wlName},
				Spec: schedulingv1alpha1.WorkloadSpec{
					PodGroups: []schedulingv1alpha1.PodGroup{{
						Name:   "replicas",
						Policy: schedulingv1alpha1.PodGroupPolicy{Basic: &schedulingv1alpha1.BasicSchedulingPolicy{}},
					}},
				},
			}
			Expect(testCtx.ControllerClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				_ = testCtx.ControllerClient.Delete(ctx, wl)
			})

			for _, key := range []string{"a", "b"} {
				pod := rd.CreatePodObject(testQ, corev1.ResourceRequirements{})
				pod.Spec.WorkloadRef = &corev1.WorkloadReference{
					Name: wlName, PodGroup: "replicas", PodGroupReplicaKey: key,
				}
				_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).NotTo(HaveOccurred())
			}

			expected := fmt.Sprintf("%s-replicas", wlName)
			pg := waitForPodGroup(ctx, testCtx, ns, expected)
			Expect(*pg.Spec.MinMember).To(Equal(int32(1)))

			// Replica-key-specific name should NOT exist — Basic collapses.
			err := testCtx.ControllerClient.Get(ctx, types.NamespacedName{
				Namespace: ns, Name: expected + "-a",
			}, &schedulingv2alpha2.PodGroup{})
			Expect(kerrors.IsNotFound(err)).To(BeTrue(),
				fmt.Sprintf("expected NotFound for replica-specific PG, got %v", err))
		})

		It("recovers instantly when a previously-missing Workload appears", func(ctx context.Context) {
			wlName := "late-" + rand.String(6)
			ns := queue.GetConnectedNamespaceToQueue(testQ)

			pod := rd.CreatePodObject(testQ, corev1.ResourceRequirements{})
			pod.Spec.WorkloadRef = &corev1.WorkloadReference{
				Name: wlName, PodGroup: "workers",
			}
			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			// No PodGroup exists yet.
			Consistently(func() bool {
				err := testCtx.ControllerClient.Get(ctx, types.NamespacedName{
					Namespace: ns, Name: wlName + "-workers",
				}, &schedulingv2alpha2.PodGroup{})
				return kerrors.IsNotFound(err)
			}, 3*time.Second, pgPollTick).Should(BeTrue(),
				"no PodGroup should exist before the Workload is created")

			wl := &schedulingv1alpha1.Workload{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: wlName},
				Spec: schedulingv1alpha1.WorkloadSpec{
					PodGroups: []schedulingv1alpha1.PodGroup{{
						Name: "workers",
						Policy: schedulingv1alpha1.PodGroupPolicy{
							Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 1},
						},
					}},
				},
			}
			Expect(testCtx.ControllerClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				_ = testCtx.ControllerClient.Delete(ctx, wl)
			})

			pg := waitForPodGroup(ctx, testCtx, ns, wlName+"-workers")
			Expect(*pg.Spec.MinMember).To(Equal(int32(1)))
		})

		It("honors the kai.scheduler/ignore-workload-api opt-out annotation", func(ctx context.Context) {
			wlName := "optout-" + rand.String(6)
			ns := queue.GetConnectedNamespaceToQueue(testQ)
			wl := &schedulingv1alpha1.Workload{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: wlName},
				Spec: schedulingv1alpha1.WorkloadSpec{
					PodGroups: []schedulingv1alpha1.PodGroup{{
						Name: "g",
						Policy: schedulingv1alpha1.PodGroupPolicy{
							Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 4},
						},
					}},
				},
			}
			Expect(testCtx.ControllerClient.Create(ctx, wl)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				_ = testCtx.ControllerClient.Delete(ctx, wl)
			})

			pod := rd.CreatePodObject(testQ, corev1.ResourceRequirements{})
			pod.Spec.WorkloadRef = &corev1.WorkloadReference{Name: wlName, PodGroup: "g"}
			pod.Annotations[commonconstants.WorkloadIgnoreAnnotationKey] = "true"
			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())

			// The Workload-derived PodGroup must NOT appear.
			Consistently(func() bool {
				err := testCtx.ControllerClient.Get(ctx, types.NamespacedName{
					Namespace: ns, Name: wlName + "-g",
				}, &schedulingv2alpha2.PodGroup{})
				return kerrors.IsNotFound(err)
			}, 3*time.Second, pgPollTick).Should(BeTrue(),
				"Workload-derived PodGroup must not be created when opt-out is set")
		})

	})
}

// skipIfWorkloadAPIUnavailable probes server discovery for the upstream
// Workload type. The feature is Alpha in Kubernetes 1.35 and off by default,
// so the suite has to be skippable on clusters that don't enable it.
func skipIfWorkloadAPIUnavailable(ctx context.Context, tc *testcontext.TestContext) {
	const groupVersion = "scheduling.k8s.io/v1alpha1"
	disc := tc.KubeClientset.Discovery()
	resources, err := disc.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		Skip(fmt.Sprintf("scheduling.k8s.io/v1alpha1 group not available (feature gate GenericWorkload likely off): %v", err))
	}
	for _, r := range resources.APIResources {
		if r.Name == "workloads" {
			return
		}
	}
	Skip("scheduling.k8s.io/v1alpha1 group present but does not expose 'workloads' resource")
}

func waitForPodGroup(ctx context.Context, tc *testcontext.TestContext, ns, name string) *schedulingv2alpha2.PodGroup {
	pg := &schedulingv2alpha2.PodGroup{}
	Eventually(func() error {
		return tc.ControllerClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, pg)
	}, pgWaitTimeout, pgPollTick).Should(Succeed(),
		"PodGroup %s/%s should appear within %s", ns, name, pgWaitTimeout)
	return pg
}

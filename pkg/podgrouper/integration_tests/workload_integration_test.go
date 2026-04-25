// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

var _ = Describe("Workload API translation", func() {
	var ns string

	BeforeEach(func(ctx context.Context) {
		ns = "wl-" + rand.String(6)
		Expect(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		})).To(Succeed())
	})

	AfterEach(func(ctx context.Context) {
		_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	})

	It("creates a KAI PodGroup with MinMember=gang.minCount for a Gang-policy Workload", func(ctx context.Context) {
		wl := &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "my-training"},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{{
					Name: "workers",
					Policy: schedulingv1alpha1.PodGroupPolicy{
						Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 3},
					},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())

		pod := newPod(ns, "worker-0", &corev1.WorkloadReference{
			Name: "my-training", PodGroup: "workers", PodGroupReplicaKey: "0",
		})
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		// The podgrouper names the KAI PodGroup {workload}-{podGroup}-{replicaKey}.
		pg := &schedulingv2alpha2.PodGroup{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "my-training-workers-0"}, pg)
		}, assertTimeout, assertInterval).Should(Succeed())
		Expect(pg.Spec.MinMember).NotTo(BeNil())
		Expect(*pg.Spec.MinMember).To(Equal(int32(3)))
		Expect(pg.Spec.SubGroups).To(BeEmpty())
	})

	It("collapses all replica keys into one KAI PodGroup for a Basic-policy Workload", func(ctx context.Context) {
		wl := &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "serving"},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{{
					Name:   "replicas",
					Policy: schedulingv1alpha1.PodGroupPolicy{Basic: &schedulingv1alpha1.BasicSchedulingPolicy{}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())

		for _, key := range []string{"a", "b", "c"} {
			pod := newPod(ns, "p-"+key, &corev1.WorkloadReference{
				Name: "serving", PodGroup: "replicas", PodGroupReplicaKey: key,
			})
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		}

		pg := &schedulingv2alpha2.PodGroup{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "serving-replicas"}, pg)
		}, assertTimeout, assertInterval).Should(Succeed())
		Expect(pg.Spec.MinMember).NotTo(BeNil())
		Expect(*pg.Spec.MinMember).To(Equal(int32(1)))

		// The replica-key-specific PodGroup name should NOT exist — Basic
		// collapses them.
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "serving-replicas-a"}, &schedulingv2alpha2.PodGroup{})
		Expect(kerrors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("expected NotFound, got %v", err))
	})

	// "Recovery on missing-then-present Workload" is intentionally NOT
	// covered here. controller-runtime's manager-cached client uses a
	// lazily-started unstructured informer for `getTopOwnerInstance`, which
	// races with the test-side pod creation in envtest and produces
	// transient `Pod not found` errors that swamp the 30s deadline. The
	// behaviour is exercised end-to-end by test/e2e/suites/workload, which
	// runs against a real apiserver where the cache stays warm. Soft-failure
	// classification (ErrWorkloadNotFound) is unit-tested in the workload
	// plugin package.

	It("honors the kai.scheduler/ignore-workload-api annotation on the pod", func(ctx context.Context) {
		wl := &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "ignored"},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{{
					Name:   "g",
					Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 5}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())

		pod := newPod(ns, "optout", &corev1.WorkloadReference{Name: "ignored", PodGroup: "g"})
		pod.Annotations = map[string]string{commonconstants.WorkloadIgnoreAnnotationKey: "true"}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		// The Workload-based name must NOT be created — the default top-owner
		// grouper runs instead, producing `pg-<podName>-<podUID>`.
		Consistently(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "ignored-g"}, &schedulingv2alpha2.PodGroup{})
			return kerrors.IsNotFound(err)
		}, consistentlyWindow, assertInterval).Should(BeTrue(),
			"no Workload-derived PodGroup should exist when opt-out is set")

		// The default grouper *does* create a PodGroup for the orphan pod.
		// We only need to assert that at least one PodGroup exists for this
		// pod's namespace and that its name does not match the Workload
		// override.
		Eventually(func() int {
			pgList := &schedulingv2alpha2.PodGroupList{}
			if err := k8sClient.List(ctx, pgList, client.InNamespace(ns)); err != nil {
				return 0
			}
			return len(pgList.Items)
		}, assertTimeout, assertInterval).Should(BeNumerically(">=", 1))
	})
})

func newPod(ns, name string, ref *corev1.WorkloadReference) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: corev1.PodSpec{
			SchedulerName: testSchedulerName,
			WorkloadRef:   ref,
			Containers: []corev1.Container{{
				Name:  "c",
				Image: "busybox",
			}},
		},
	}
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
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

		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "serving-replicas-a"}, &schedulingv2alpha2.PodGroup{})
		Expect(kerrors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("expected NotFound, got %v", err))
	})

	It("does not propagate Workload kai.scheduler/queue label changes to the existing PodGroup", func(ctx context.Context) {
		const initialQueue = "ml-training"
		const updatedQueue = "ml-batch"
		wl := &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns, Name: "queue-frozen",
				Labels: map[string]string{commonconstants.DefaultQueueLabel: initialQueue},
			},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{{
					Name: "g",
					Policy: schedulingv1alpha1.PodGroupPolicy{
						Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 1},
					},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())
		Expect(k8sClient.Create(ctx, newPod(ns, "qfp", &corev1.WorkloadReference{Name: "queue-frozen", PodGroup: "g"}))).To(Succeed())

		Eventually(func() (string, error) {
			pg := &schedulingv2alpha2.PodGroup{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "queue-frozen-g"}, pg); err != nil {
				return "", err
			}
			return pg.Spec.Queue, nil
		}, assertTimeout, assertInterval).Should(Equal(initialQueue))

		// Retry on conflict against the watcher's race-edit of the same object.
		Eventually(func() error {
			cur := &schedulingv1alpha1.Workload{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "queue-frozen"}, cur); err != nil {
				return err
			}
			cur.Labels[commonconstants.DefaultQueueLabel] = updatedQueue
			return k8sClient.Update(ctx, cur)
		}, assertTimeout, assertInterval).Should(Succeed())

		Consistently(func() (string, error) {
			cur := &schedulingv2alpha2.PodGroup{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "queue-frozen-g"}, cur); err != nil {
				return "", err
			}
			return cur.Spec.Queue, nil
		}, consistentlyWindow, assertInterval).Should(Equal(initialQueue),
			"Spec.Queue is owned by the queue-assigner and must not follow Workload label mutations")
	})

	It("creates one independent KAI PodGroup per podGroup in the same Workload", func(ctx context.Context) {
		wl := &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "multi"},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{
					{
						Name:   "driver",
						Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 1}},
					},
					{
						Name:   "workers",
						Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 4}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())

		driverPod := newPod(ns, "driver-0", &corev1.WorkloadReference{Name: "multi", PodGroup: "driver"})
		workerPod := newPod(ns, "worker-0", &corev1.WorkloadReference{Name: "multi", PodGroup: "workers", PodGroupReplicaKey: "0"})
		Expect(k8sClient.Create(ctx, driverPod)).To(Succeed())
		Expect(k8sClient.Create(ctx, workerPod)).To(Succeed())

		driverPG := &schedulingv2alpha2.PodGroup{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "multi-driver"}, driverPG)
		}, assertTimeout, assertInterval).Should(Succeed())
		Expect(*driverPG.Spec.MinMember).To(Equal(int32(1)))

		workersPG := &schedulingv2alpha2.PodGroup{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "multi-workers-0"}, workersPG)
		}, assertTimeout, assertInterval).Should(Succeed())
		Expect(*workersPG.Spec.MinMember).To(Equal(int32(4)))

		Expect(driverPG.UID).NotTo(Equal(workersPG.UID))
		Expect(driverPG.Spec.SubGroups).To(BeEmpty())
		Expect(workersPG.Spec.SubGroups).To(BeEmpty())
	})

	It("creates separate KAI PodGroups per Gang replicaKey", func(ctx context.Context) {
		wl := &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "replicas"},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{{
					Name:   "workers",
					Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 2}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())

		for _, key := range []string{"0", "1"} {
			Expect(k8sClient.Create(ctx, newPod(ns, "p-"+key, &corev1.WorkloadReference{
				Name: "replicas", PodGroup: "workers", PodGroupReplicaKey: key,
			}))).To(Succeed())
		}

		for _, key := range []string{"0", "1"} {
			pg := &schedulingv2alpha2.PodGroup{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "replicas-workers-" + key}, pg)
			}, assertTimeout, assertInterval).Should(Succeed())
			Expect(*pg.Spec.MinMember).To(Equal(int32(2)))
		}
	})

	It("converges multiple pods sharing the same workloadRef into one PodGroup", func(ctx context.Context) {
		wl := &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "shared"},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{{
					Name:   "g",
					Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 3}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())

		for _, name := range []string{"a", "b", "c"} {
			Expect(k8sClient.Create(ctx, newPod(ns, name, &corev1.WorkloadReference{
				Name: "shared", PodGroup: "g", PodGroupReplicaKey: "0",
			}))).To(Succeed())
		}

		expected := "shared-g-0"
		for _, name := range []string{"a", "b", "c"} {
			Eventually(func() string {
				p := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, p); err != nil {
					return ""
				}
				return p.Annotations[commonconstants.PodGroupAnnotationForPod]
			}, assertTimeout, assertInterval).Should(Equal(expected),
				"pod %s should be annotated with PodGroup %s", name, expected)
		}

		pgList := &schedulingv2alpha2.PodGroupList{}
		Expect(k8sClient.List(ctx, pgList, client.InNamespace(ns))).To(Succeed())
		matching := 0
		for _, pg := range pgList.Items {
			if pg.Name == expected {
				matching++
			}
		}
		Expect(matching).To(Equal(1), "exactly one KAI PodGroup should converge for the shared workloadRef")
	})

	It("propagates Workload label mutations to the existing KAI PodGroup", func(ctx context.Context) {
		wl := &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns, Name: "mutating",
				Labels: map[string]string{"priorityClassName": "build"},
			},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{{
					Name:   "g",
					Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 1}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())
		Expect(k8sClient.Create(ctx, newPod(ns, "p", &corev1.WorkloadReference{Name: "mutating", PodGroup: "g"}))).To(Succeed())

		pg := &schedulingv2alpha2.PodGroup{}
		Eventually(func() (string, error) {
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "mutating-g"}, pg); err != nil {
				return "", err
			}
			return pg.Spec.PriorityClassName, nil
		}, assertTimeout, assertInterval).Should(Equal("build"))

		// Retry the update on conflict — the watcher's first reconcile race-edits via labels propagation.
		Eventually(func() error {
			cur := &schedulingv1alpha1.Workload{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "mutating"}, cur); err != nil {
				return err
			}
			cur.Labels["priorityClassName"] = "train"
			return k8sClient.Update(ctx, cur)
		}, assertTimeout, assertInterval).Should(Succeed())

		Eventually(func() (string, error) {
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "mutating-g"}, pg); err != nil {
				return "", err
			}
			return pg.Spec.PriorityClassName, nil
		}, assertTimeout, assertInterval).Should(Equal("train"),
			"updated Workload priorityClassName label must propagate to PodGroup.Spec.PriorityClassName")
	})

	It("preserves the existing KAI PodGroup when the Workload is deleted", func(ctx context.Context) {
		wl := &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "tombstone"},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{{
					Name:   "g",
					Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 2}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())
		Expect(k8sClient.Create(ctx, newPod(ns, "p", &corev1.WorkloadReference{Name: "tombstone", PodGroup: "g"}))).To(Succeed())

		pg := &schedulingv2alpha2.PodGroup{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "tombstone-g"}, pg)
		}, assertTimeout, assertInterval).Should(Succeed())
		originalUID := pg.UID

		Expect(k8sClient.Delete(ctx, wl)).To(Succeed())

		Consistently(func() (types.UID, error) {
			cur := &schedulingv2alpha2.PodGroup{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "tombstone-g"}, cur); err != nil {
				return "", err
			}
			return cur.UID, nil
		}, consistentlyWindow, assertInterval).Should(Equal(originalUID))
	})

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

		Consistently(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "ignored-g"}, &schedulingv2alpha2.PodGroup{})
			return kerrors.IsNotFound(err)
		}, consistentlyWindow, assertInterval).Should(BeTrue(),
			"no Workload-derived PodGroup should exist when opt-out is set")

		Eventually(func() int {
			pgList := &schedulingv2alpha2.PodGroupList{}
			if err := k8sClient.List(ctx, pgList, client.InNamespace(ns)); err != nil {
				return 0
			}
			return len(pgList.Items)
		}, assertTimeout, assertInterval).Should(BeNumerically(">=", 1))
	})

	It("uses the Workload as PodGroup owner when bare pods reference it via OwnerReference", func(ctx context.Context) {
		wl := &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "owns-pg"},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{{
					Name:   "g",
					Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 3}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())

		wlOwnerRef := metav1.OwnerReference{
			APIVersion: schedulingv1alpha1.SchemeGroupVersion.String(),
			Kind:       "Workload",
			Name:       wl.Name,
			UID:        wl.UID,
		}

		for _, name := range []string{"a", "b", "c"} {
			p := newPod(ns, name, &corev1.WorkloadReference{Name: "owns-pg", PodGroup: "g"})
			p.OwnerReferences = []metav1.OwnerReference{wlOwnerRef}
			Expect(k8sClient.Create(ctx, p)).To(Succeed())
		}

		pg := &schedulingv2alpha2.PodGroup{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "owns-pg-g"}, pg)
		}, assertTimeout, assertInterval).Should(Succeed())

		Expect(pg.OwnerReferences).To(HaveLen(1))
		Expect(pg.OwnerReferences[0].Kind).To(Equal("Workload"))
		Expect(pg.OwnerReferences[0].UID).To(Equal(wl.UID),
			"PodGroup must be owned by the Workload, not by any individual pod")

		Consistently(func() (types.UID, error) {
			cur := &schedulingv2alpha2.PodGroup{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "owns-pg-g"}, cur); err != nil {
				return "", err
			}
			if len(cur.OwnerReferences) == 0 {
				return "", nil
			}
			return cur.OwnerReferences[0].UID, nil
		}, consistentlyWindow, assertInterval).Should(Equal(wl.UID),
			"PodGroup OwnerReference must stay pinned to the Workload, not thrash across pod reconciles")
	})

	It("lets the Workload override an owning controller's grouping decision", func(ctx context.Context) {
		Expect(k8sClient.Create(ctx, &schedulingv1alpha1.Workload{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "smallwl"},
			Spec: schedulingv1alpha1.WorkloadSpec{
				PodGroups: []schedulingv1alpha1.PodGroup{{
					Name:   "g",
					Policy: schedulingv1alpha1.PodGroupPolicy{Gang: &schedulingv1alpha1.GangSchedulingPolicy{MinCount: 1}},
				}},
			},
		})).To(Succeed())

		// envtest doesn't run kube-controller-manager — Pods must be parented to the Job explicitly.
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   ns,
				Name:        "bigjob",
				Annotations: map[string]string{"kai.scheduler/batch-min-member": "4"},
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"job": "bigjob"}},
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers:    []corev1.Container{{Name: "c", Image: "busybox"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).To(Succeed())

		jobOwnerRef := metav1.OwnerReference{
			APIVersion: "batch/v1", Kind: "Job",
			Name: job.Name, UID: job.UID,
			Controller:         ptr(true),
			BlockOwnerDeletion: ptr(true),
		}

		for _, name := range []string{"p0", "p1", "p2", "p3"} {
			p := newPod(ns, name, &corev1.WorkloadReference{Name: "smallwl", PodGroup: "g"})
			p.OwnerReferences = []metav1.OwnerReference{jobOwnerRef}
			Expect(k8sClient.Create(ctx, p)).To(Succeed())
		}

		pg := &schedulingv2alpha2.PodGroup{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "smallwl-g"}, pg)
		}, assertTimeout, assertInterval).Should(Succeed())
		Expect(*pg.Spec.MinMember).To(Equal(int32(1)),
			"Workload.gang.minCount=1 must override the Job's batch-min-member=4")
		Expect(pg.Spec.SubGroups).To(BeEmpty(),
			"Workload override must drop SubGroups produced by the top-owner plugin")

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: ns, Name: "pg-bigjob-" + string(job.UID),
			}, &schedulingv2alpha2.PodGroup{})
			return kerrors.IsNotFound(err)
		}, consistentlyWindow, assertInterval).Should(BeTrue(),
			"no Job-derived PodGroup should exist when a Workload override is active")

		for _, name := range []string{"p0", "p1", "p2", "p3"} {
			Eventually(func() string {
				p := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, p); err != nil {
					return ""
				}
				return p.Annotations[commonconstants.PodGroupAnnotationForPod]
			}, assertTimeout, assertInterval).Should(Equal("smallwl-g"),
				"pod %s should be annotated with the Workload-derived PodGroup", name)
		}

		Eventually(func() error {
			cur := &schedulingv1alpha1.Workload{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "smallwl"}, cur); err != nil {
				return err
			}
			if cur.Labels == nil {
				cur.Labels = map[string]string{}
			}
			cur.Labels["priorityClassName"] = "inference"
			return k8sClient.Update(ctx, cur)
		}, assertTimeout, assertInterval).Should(Succeed())

		Eventually(func() (string, error) {
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "smallwl-g"}, pg); err != nil {
				return "", err
			}
			return pg.Spec.PriorityClassName, nil
		}, assertTimeout, assertInterval).Should(Equal("inference"))
	})
})

func ptr[T any](v T) *T { return &v }

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

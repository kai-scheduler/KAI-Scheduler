// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaicommon "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
)

var _ = Describe("BuildVPA", func() {
	It("should return nil when vpaSpec is nil", func() {
		Expect(BuildVPA(nil, "name", "ns", "Deployment")).To(BeNil())
	})

	It("should return nil when Enabled is nil", func() {
		Expect(BuildVPA(&kaicommon.VPASpec{}, "name", "ns", "Deployment")).To(BeNil())
	})

	It("should return nil when Enabled is false", func() {
		spec := &kaicommon.VPASpec{Enabled: ptr.To(false)}
		Expect(BuildVPA(spec, "name", "ns", "Deployment")).To(BeNil())
	})

	It("should build a VPA targeting the given resource when enabled", func() {
		mode := vpav1.UpdateModeAuto
		spec := &kaicommon.VPASpec{
			Enabled:      ptr.To(true),
			UpdatePolicy: &vpav1.PodUpdatePolicy{UpdateMode: &mode},
		}

		result := BuildVPA(spec, "my-deploy", "my-ns", "Deployment")
		Expect(result).ToNot(BeNil())

		vpa := result.(*vpav1.VerticalPodAutoscaler)
		Expect(vpa.Name).To(Equal("my-deploy"))
		Expect(vpa.Namespace).To(Equal("my-ns"))
		Expect(vpa.Spec.TargetRef).To(Equal(&autoscalingv1.CrossVersionObjectReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "my-deploy",
		}))
		Expect(*vpa.Spec.UpdatePolicy.UpdateMode).To(Equal(vpav1.UpdateModeAuto))
	})
})

var _ = Describe("BuildVPAFromObjects", func() {
	It("should return nil when vpaSpec is nil", func() {
		Expect(BuildVPAFromObjects(nil, nil, "ns")).To(BeNil())
	})

	It("should return nil when disabled", func() {
		spec := &kaicommon.VPASpec{Enabled: ptr.To(false)}
		Expect(BuildVPAFromObjects(spec, nil, "ns")).To(BeNil())
	})

	It("should return nil when no Deployment or DaemonSet found", func() {
		spec := &kaicommon.VPASpec{Enabled: ptr.To(true)}
		objects := []client.Object{
			&metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "svc"}},
		}
		Expect(BuildVPAFromObjects(spec, objects, "ns")).To(BeNil())
	})

	It("should build VPA from the first Deployment", func() {
		spec := &kaicommon.VPASpec{Enabled: ptr.To(true)}
		objects := []client.Object{
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "dep-1"}},
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "dep-2"}},
		}

		result := BuildVPAFromObjects(spec, objects, "ns")
		Expect(result).ToNot(BeNil())
		vpa := result.(*vpav1.VerticalPodAutoscaler)
		Expect(vpa.Name).To(Equal("dep-1"))
		Expect(vpa.Spec.TargetRef.Kind).To(Equal("Deployment"))
	})

	It("should build VPA from a DaemonSet", func() {
		spec := &kaicommon.VPASpec{Enabled: ptr.To(true)}
		objects := []client.Object{
			&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "ds-1"}},
		}

		result := BuildVPAFromObjects(spec, objects, "ns")
		Expect(result).ToNot(BeNil())
		vpa := result.(*vpav1.VerticalPodAutoscaler)
		Expect(vpa.Name).To(Equal("ds-1"))
		Expect(vpa.Spec.TargetRef.Kind).To(Equal("DaemonSet"))
	})
})

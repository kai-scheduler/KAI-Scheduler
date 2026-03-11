// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestScheduler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scheduler type suite")
}

var _ = Describe("Scheduler", func() {
	var (
		fakeClient               client.WithWatch
		kaiConfig                *kaiv1.Config
		shard                    *kaiv1.SchedulingShard
		schedulerOperandForShard *SchedulerForShard
	)

	BeforeEach(func() {
		fakeClientBuilder := fake.NewClientBuilder()
		fakeClient = fakeClientBuilder.Build()

		kaiConfig = &kaiv1.Config{}
		kaiConfig.Spec.SetDefaultsWhereNeeded()
		kaiConfig.Spec.Scheduler.Service.Enabled = ptr.To(true)
		shard = &kaiv1.SchedulingShard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		}

		shard.Spec.SetDefaultsWhereNeeded()
		schedulerOperandForShard = NewSchedulerForShard(shard)
	})

	It("Default kai config", func(ctx context.Context) {
		desiredState, err := schedulerOperandForShard.DesiredState(ctx, fakeClient, kaiConfig)

		Expect(err).To(BeNil())
		Expect(len(desiredState)).To(Equal(3))
	})

	It("Should maintain existing annotations", func(ctx context.Context) {
		existingDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deploymentName(kaiConfig, shard),
				Namespace: kaiConfig.Spec.Namespace,
				Annotations: map[string]string{
					"bla": "bla",
				},
			},
			Spec:   appsv1.DeploymentSpec{},
			Status: appsv1.DeploymentStatus{},
		}
		Expect(fakeClient.Create(ctx, existingDeployment)).To(Succeed())

		s := NewSchedulerForShard(shard)
		deploymentObj, err := s.deploymentForShard(ctx, fakeClient, kaiConfig, shard)
		deployment := deploymentObj.(*appsv1.Deployment)

		Expect(err).To(BeNil())
		Expect(deployment.Annotations).To(Not(BeNil()))
		Expect(deployment.Annotations["bla"]).To(Equal("bla"))
	})

	It("Should configure scheduler image", func(ctx context.Context) {
		kaiConfig.Spec.Scheduler.Service.Image.Repository = ptr.To("best-repo")
		kaiConfig.Spec.Scheduler.Service.Image.Name = ptr.To("best-name")
		kaiConfig.Spec.Scheduler.Service.Image.Tag = ptr.To("great-tag")

		s := NewSchedulerForShard(shard)
		deploymentObj, err := s.deploymentForShard(ctx, fakeClient, kaiConfig, shard)
		deployment := deploymentObj.(*appsv1.Deployment)

		Expect(err).To(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("best-repo/best-name:great-tag"))
	})

	It("Should handle multiple Shards", func(ctx context.Context) {
		desiredState, err := schedulerOperandForShard.DesiredState(ctx, fakeClient, kaiConfig)
		Expect(err).To(BeNil())

		for _, obj := range desiredState {
			Expect(fakeClient.Create(ctx, obj)).To(Succeed())
		}

		otherShard := &kaiv1.SchedulingShard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "other",
			},
		}
		otherShard.Spec.SetDefaultsWhereNeeded()

		otherShardOperand := NewSchedulerForShard(otherShard)
		otherDesiredState, err := otherShardOperand.DesiredState(ctx, fakeClient, kaiConfig)
		Expect(err).To(BeNil())

		for _, obj := range otherDesiredState {
			Expect(fakeClient.Create(ctx, obj)).To(Succeed())
		}

		deployments := &appsv1.DeploymentList{}
		Expect(fakeClient.List(ctx, deployments)).To(Succeed())
		Expect(len(deployments.Items)).To(Equal(2))

		configMaps := &v1.ConfigMapList{}
		Expect(fakeClient.List(ctx, configMaps)).To(Succeed())
		Expect(len(configMaps.Items)).To(Equal(2))

		services := &v1.ServiceList{}
		Expect(fakeClient.List(ctx, services, client.InNamespace(kaiConfig.Spec.Namespace))).To(Succeed())
		Expect(len(services.Items)).To(Equal(2))
	})

	Context("ConfigMap", func() {
		It("Should create configmap", func(ctx context.Context) {
			s := NewSchedulerForShard(shard)
			cmObj, err := s.configMapForShard(ctx, fakeClient, kaiConfig, shard)
			cm := cmObj.(*v1.ConfigMap)

			Expect(err).To(BeNil())
			Expect(cm.Data["config.yaml"]).To(MatchYAML(`actions: allocate, consolidation, reclaim, preempt, stalegangeviction
tiers:
    - plugins:
        - name: predicates
        - name: proportion
        - name: priority
        - name: nodeavailability
        - name: resourcetype
        - name: podaffinity
        - name: elastic
        - name: kubeflow
        - name: ray
        - name: subgrouporder
        - name: taskorder
        - name: nominatednode
        - name: dynamicresources
        - name: minruntime
        - name: topology
        - name: snapshot
        - name: gpupack
        - name: nodeplacement
          arguments:
            cpu: binpack
            gpu: binpack
        - name: gpusharingorder
`))
		})

		It("Should create different configmap for spread", func(ctx context.Context) {
			spreadShard := &kaiv1.SchedulingShard{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: kaiv1.SchedulingShardSpec{
					PlacementStrategy: &kaiv1.PlacementStrategy{
						CPU: ptr.To("spread"),
						GPU: ptr.To("spread"),
					},
				},
			}
			spreadShard.Spec.SetDefaultsWhereNeeded()
			s := NewSchedulerForShard(spreadShard)
			cmObj, err := s.configMapForShard(ctx, fakeClient, kaiConfig, spreadShard)
			cm := cmObj.(*v1.ConfigMap)

			Expect(err).To(BeNil())
			Expect(cm.Data["config.yaml"]).To(MatchYAML(`actions: allocate, reclaim, preempt, stalegangeviction
tiers:
    - plugins:
        - name: predicates
        - name: proportion
        - name: priority
        - name: nodeavailability
        - name: resourcetype
        - name: podaffinity
        - name: elastic
        - name: kubeflow
        - name: ray
        - name: subgrouporder
        - name: taskorder
        - name: nominatednode
        - name: dynamicresources
        - name: minruntime
        - name: topology
        - name: snapshot
        - name: gpuspread
        - name: nodeplacement
          arguments:
            cpu: spread
            gpu: spread
`))
		})
	})

	It("returns the same desired state if nothing changed", func(ctx context.Context) {
		desiredState, err := schedulerOperandForShard.DesiredState(ctx, fakeClient, kaiConfig)
		Expect(err).To(BeNil())
		Expect(len(desiredState)).To(Equal(3))

		desiredState, err = schedulerOperandForShard.DesiredState(ctx, fakeClient, kaiConfig)
		Expect(err).To(BeNil())
		Expect(len(desiredState)).To(Equal(3))
	})

	Describe("Scheduler Operand for kai config", func() {
		var (
			schedulerForConfig *SchedulerForConfig
		)
		BeforeEach(func(ctx context.Context) {
			schedulerForConfig = &SchedulerForConfig{}
		})

		It("Creates service accounts", func(ctx context.Context) {
			desiredState, err := schedulerForConfig.DesiredState(ctx, fakeClient, kaiConfig)
			Expect(err).To(BeNil())

			Expect(len(desiredState)).To(Equal(1))
			for _, obj := range desiredState {
				Expect(obj).To(BeAssignableToTypeOf(&v1.ServiceAccount{}))
			}
		})
	})
})

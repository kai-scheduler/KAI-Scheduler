// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_reconciler

import (
	"context"
	"fmt"
	"testing"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestStatusController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Status Controller Suite")
}

var _ = Describe("Status Controller", func() {
	var (
		fakeClient client.Client
		kaiConfig  *kaiv1.Config
		shard      *kaiv1.SchedulingShard
	)
	BeforeEach(func() {
		scheme := scheme.Scheme
		kaiv1.AddToScheme(scheme)
		monitoringv1.AddToScheme(scheme)

		kaiConfig = &kaiv1.Config{
			ObjectMeta: metav1.ObjectMeta{
				Name:       constants.DefaultKAIConfigSingeltonInstanceName,
				Generation: 1,
			},
		}
		shard = &kaiv1.SchedulingShard{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "default",
				Generation: 1,
			},
		}

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&kaiv1.Config{}, &kaiv1.SchedulingShard{}).
			WithObjects(kaiConfig, shard).
			Build()
	})

	Describe("updateStartReconcileStatus", func() {
		It("should set the reconciling condition", func() {
			fakeDeployable := &fakeDeployable{
				isDeployed:     true,
				isDeployedErr:  false,
				isAvailable:    true,
				isAvailableErr: false,
			}

			statusReconciler := &StatusReconciler{
				deployable: fakeDeployable,
				Client:     fakeClient,
			}

			err := statusReconciler.UpdateStartReconcileStatus(
				context.TODO(), &KAIConfigWithStatusWrapper{Config: kaiConfig},
			)
			Expect(err).ToNot(HaveOccurred())

			Expect(fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(kaiConfig), kaiConfig)).To(Succeed())
			found := false
			for _, condition := range kaiConfig.Status.Conditions {
				if condition.Type == string(kaiv1.ConditionTypeReconciling) &&
					condition.Status == metav1.ConditionTrue &&
					condition.ObservedGeneration == kaiConfig.Generation &&
					condition.Reason == string(kaiv1.Reconciling) {
					found = true
				}
			}

			Expect(found).To(BeTrue())
		})
	})

	Describe("reconcileStatus", func() {
		DescribeTable("should set the reconciling condition", func(
			getObject func() objectWithConditions,
			isDeployErr bool, isDeployed bool, isAvailableErr bool, isAvailable bool,
		) {
			object := getObject()
			fakeDeployable := &fakeDeployable{
				isDeployed:     isDeployed,
				isDeployedErr:  isDeployErr,
				isAvailable:    isAvailable,
				isAvailableErr: isAvailableErr,
			}

			statusReconciler := &StatusReconciler{
				deployable: fakeDeployable,
				Client:     fakeClient,
			}
			err := statusReconciler.ReconcileStatus(
				context.TODO(), object,
			)
			Expect(err).ToNot(HaveOccurred())

			Expect(fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(object), object.GetInternalObject())).To(Succeed())

			isDeployed = isDeployed && !isDeployErr
			isAvailable = isAvailable && !isAvailableErr
			Expect(checkIsDeployed(object.GetConditions())).To(Equal(isDeployed))
			Expect(checkIsAvailable(object.GetConditions())).To(Equal(isAvailable))
			Expect(checkIsReady(object.GetConditions())).To(Equal(isAvailable))
			Expect(checkIsReconciling(object.GetConditions())).To(BeFalse())
		},
			Entry("kai config - no errors, all set", func() objectWithConditions { return &KAIConfigWithStatusWrapper{Config: kaiConfig} }, false, true, false, true),
			Entry("kai config - no errors, deployed not avaialbe", func() objectWithConditions { return &KAIConfigWithStatusWrapper{Config: kaiConfig} }, false, true, false, false),
			Entry("kai config - no errors, not deployed not avaialbe", func() objectWithConditions { return &KAIConfigWithStatusWrapper{Config: kaiConfig} }, false, true, false, false),
			Entry("kai config - error isDeployed", func() objectWithConditions { return &KAIConfigWithStatusWrapper{Config: kaiConfig} }, true, false, false, false),
			Entry("kai config - error isAvailable", func() objectWithConditions { return &KAIConfigWithStatusWrapper{Config: kaiConfig} }, false, false, true, false),

			Entry("scheduling shard - no errors, all set", func() objectWithConditions { return &SchedulingShardWithStatusWrapper{SchedulingShard: shard} }, false, true, false, true),
			Entry("scheduling shard - no errors, deployed not avaialbe", func() objectWithConditions { return &SchedulingShardWithStatusWrapper{SchedulingShard: shard} }, false, true, false, false),
			Entry("scheduling shard - no errors, not deployed not avaialbe", func() objectWithConditions { return &SchedulingShardWithStatusWrapper{SchedulingShard: shard} }, false, true, false, false),
			Entry("scheduling shard - error isDeployed", func() objectWithConditions { return &SchedulingShardWithStatusWrapper{SchedulingShard: shard} }, true, false, false, false),
			Entry("scheduling shard - error isAvailable", func() objectWithConditions { return &SchedulingShardWithStatusWrapper{SchedulingShard: shard} }, false, false, true, false),
		)
	})
})

func checkIsDeployed(conditions []metav1.Condition) bool {
	isDeployedCondition := getConditionByType(conditions, string(kaiv1.ConditionTypeDeployed))
	if isDeployedCondition == nil {
		return false
	}
	return isDeployedCondition.Status == metav1.ConditionTrue
}

func checkIsAvailable(conditions []metav1.Condition) bool {
	isAvailableCondition := getConditionByType(conditions, string(kaiv1.ConditionTypeAvailable))
	if isAvailableCondition == nil {
		return false
	}
	return isAvailableCondition.Status == metav1.ConditionTrue
}

func checkIsReady(conditions []metav1.Condition) bool {
	isReadyCondition := getConditionByType(conditions, string(kaiv1.ConditionTypeReady))
	if isReadyCondition == nil {
		return false
	}
	return isReadyCondition.Status == metav1.ConditionTrue
}

func checkIsReconciling(conditions []metav1.Condition) bool {
	isReconcilingCondition := getConditionByType(conditions, string(kaiv1.ConditionTypeReconciling))
	if isReconcilingCondition == nil {
		return false
	}
	return isReconcilingCondition.Status == metav1.ConditionTrue
}
func getConditionByType(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return &condition
		}
	}
	return nil
}

type fakeDeployable struct {
	failDeploy     bool
	isDeployed     bool
	isDeployedErr  bool
	isAvailable    bool
	isAvailableErr bool
}

func (f *fakeDeployable) Deploy(ctx context.Context, runtimeClient client.Client, kaiConfig *kaiv1.Config, reconciler client.Object) error {
	if f.failDeploy {
		return fmt.Errorf("Deploy failed")
	}
	return nil
}

func (f *fakeDeployable) IsDeployed(ctx context.Context, runtimeClient client.Reader) (bool, error) {
	if f.isDeployedErr {
		return false, fmt.Errorf("IsDeployed failed")
	}
	return f.isDeployed, nil
}

func (f *fakeDeployable) IsAvailable(ctx context.Context, runtimeClient client.Reader) (bool, error) {
	if f.isAvailableErr {
		return false, fmt.Errorf("IsAvailable failed")
	}
	return f.isAvailable, nil
}

func (f *fakeDeployable) Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) error {
	return nil
}

func (f *fakeDeployable) HasMissingDependencies(ctx context.Context, readerClient client.Reader, obj client.Object) (string, error) {
	return "", nil
}

func (f *fakeDeployable) Name() string {
	return "fakeDeployable"
}

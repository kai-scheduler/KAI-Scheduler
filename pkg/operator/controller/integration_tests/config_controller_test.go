// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests

import (
	"context"
	"os"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiv1admission "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/admission"
	kaiv1binder "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/binder"
	kaiv1scheduler "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/scheduler"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	nvidiav1 "github.com/kai-scheduler/KAI-scheduler/third_party/nvidia/gpu-operator/api/nvidia/v1"

	appsv1 "k8s.io/api/apps/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1common "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
)

const (
	githubRoot = "../../../../"
	repository = "ghcr.io/kai-scheduler/kai-scheduler"
	tag        = "latest"
)

var _ = Describe("KAIConfigController", Ordered, func() {
	var (
		kaiConfig *kaiv1.Config
	)

	BeforeAll(func() {
		kaiConfig = &kaiv1.Config{
			ObjectMeta: metav1.ObjectMeta{
				Name: constants.DefaultKAIConfigSingeltonInstanceName,
			},
			Spec: kaiv1.ConfigSpec{
				Namespace: "kai-scheduler",
				Binder: &kaiv1binder.Binder{
					Service: &v1common.Service{
						Enabled: ptr.To(true),
					},
				},
			},
		}
		os.Setenv(v1common.DefaultRepositoryEnvVarName, repository)
		os.Setenv(v1common.DefaultTagEnvVarName, tag)

		Expect(k8sClient.Create(context.Background(), kaiConfig)).To(Succeed())
	})

	AfterAll(func() {
		Expect(k8sClient.Delete(context.Background(), kaiConfig)).To(Succeed())
		os.Unsetenv(v1common.DefaultRepositoryEnvVarName)
		os.Unsetenv(v1common.DefaultTagEnvVarName)
	})

	Context("Watching ClusterPolicy", Ordered, func() {
		It("Updates binder deployment when ClusterPolicy changes", func(ctx context.Context) {
			var binderDeploymentGeneration int64
			Eventually(func(g Gomega) {
				binderDeployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binder",
						Namespace: kaiConfig.Spec.Namespace,
					},
				}
				g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(binderDeployment), binderDeployment)).To(Succeed())
				binderDeploymentGeneration = binderDeployment.Generation
			}, "10s", "200ms").Should(Succeed())

			Expect(k8sClient.Create(ctx, &nvidiav1.ClusterPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example-cluster-policy",
				},
				Spec: nvidiav1.ClusterPolicySpec{
					Operator: nvidiav1.OperatorSpec{
						DefaultRuntime: nvidiav1.Docker,
					},
					CDI: nvidiav1.CDIConfigSpec{
						Enabled: ptr.To(true),
						Default: ptr.To(true),
					},
				},
			})).To(Succeed())

			Eventually(func(g Gomega) bool {
				binderDeployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binder",
						Namespace: kaiConfig.Spec.Namespace,
					},
				}
				g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(binderDeployment), binderDeployment)).To(Succeed())

				return binderDeploymentGeneration < binderDeployment.Generation
			}, "10s", "200ms").Should(BeTrue())

			Eventually(func(g Gomega) bool {
				updatedConfig := &kaiv1.Config{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: constants.DefaultKAIConfigSingeltonInstanceName}, updatedConfig)).To(Succeed())

				for _, condition := range updatedConfig.Status.Conditions {
					if condition.Type == string(kaiv1.ConditionTypeDeployed) && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, "10s", "200ms").Should(BeTrue())
		})
	})

	Context("Reconciling the admission PodDisruptionBudget", Ordered, func() {
		const pdbName = "admission"

		updateAdmissionPDBConfig := func(ctx context.Context, replicas int32, enabled bool) {
			Eventually(func() error {
				currentConfig := &kaiv1.Config{}
				if err := k8sClient.Get(
					ctx,
					types.NamespacedName{Name: constants.DefaultKAIConfigSingeltonInstanceName},
					currentConfig,
				); err != nil {
					return err
				}
				currentConfig.Spec.Admission = &kaiv1admission.Admission{
					Replicas: ptr.To(replicas),
					Service: &v1common.Service{
						Enabled: ptr.To(true),
						PodDisruptionBudget: &v1common.PodDisruptionBudget{
							Enabled:        ptr.To(enabled),
							MaxUnavailable: ptr.To(int32(1)),
						},
					},
				}
				return k8sClient.Update(ctx, currentConfig)
			}, "10s", "200ms").Should(Succeed())
		}

		getPDB := func(ctx context.Context) (*policyv1.PodDisruptionBudget, error) {
			pdb := &policyv1.PodDisruptionBudget{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      pdbName,
				Namespace: kaiConfig.Spec.Namespace,
			}, pdb)
			return pdb, err
		}

		It("creates and watches the PDB", func(ctx context.Context) {
			updateAdmissionPDBConfig(ctx, 2, true)

			Eventually(func(g Gomega) {
				pdb, err := getPDB(ctx)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(metav1.GetControllerOf(pdb)).NotTo(BeNil())
				g.Expect(metav1.GetControllerOf(pdb).Kind).To(Equal("Config"))
			}, "10s", "200ms").Should(Succeed())

			pdb, err := getPDB(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, pdb)).To(Succeed())

			Eventually(func() error {
				_, err := getPDB(ctx)
				return err
			}, "10s", "200ms").Should(Succeed())
		})

		It("removes the PDB after scaling admission down to one replica", func(ctx context.Context) {
			updateAdmissionPDBConfig(ctx, 1, true)

			Eventually(func() bool {
				_, err := getPDB(ctx)
				return apierrors.IsNotFound(err)
			}, "10s", "200ms").Should(BeTrue())
		})

		It("removes the PDB when it is disabled", func(ctx context.Context) {
			updateAdmissionPDBConfig(ctx, 2, true)
			Eventually(func() error {
				_, err := getPDB(ctx)
				return err
			}, "10s", "200ms").Should(Succeed())

			updateAdmissionPDBConfig(ctx, 2, false)
			Eventually(func() bool {
				_, err := getPDB(ctx)
				return apierrors.IsNotFound(err)
			}, "10s", "200ms").Should(BeTrue())
		})
	})

	Context("Reconciling the scheduler PodDisruptionBudget", Ordered, func() {
		const (
			shardName = "pdb-test"
			pdbName   = "kai-scheduler-" + shardName
		)

		updateSchedulerPDBConfig := func(ctx context.Context, replicas int32, enabled bool) {
			Eventually(func() error {
				currentConfig := &kaiv1.Config{}
				if err := k8sClient.Get(
					ctx,
					types.NamespacedName{Name: constants.DefaultKAIConfigSingeltonInstanceName},
					currentConfig,
				); err != nil {
					return err
				}
				currentConfig.Spec.Scheduler = &kaiv1scheduler.Scheduler{
					Replicas: ptr.To(replicas),
					Service: &v1common.Service{
						Enabled: ptr.To(true),
						PodDisruptionBudget: &v1common.PodDisruptionBudget{
							Enabled:        ptr.To(enabled),
							MaxUnavailable: ptr.To(int32(1)),
						},
					},
				}
				return k8sClient.Update(ctx, currentConfig)
			}, "10s", "200ms").Should(Succeed())
		}

		getPDB := func(ctx context.Context) (*policyv1.PodDisruptionBudget, error) {
			pdb := &policyv1.PodDisruptionBudget{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      pdbName,
				Namespace: kaiConfig.Spec.Namespace,
			}, pdb)
			return pdb, err
		}

		BeforeAll(func(ctx context.Context) {
			updateSchedulerPDBConfig(ctx, 2, true)
			Expect(k8sClient.Create(ctx, &kaiv1.SchedulingShard{
				ObjectMeta: metav1.ObjectMeta{Name: shardName},
			})).To(Succeed())
		})

		AfterAll(func(ctx context.Context) {
			shard := &kaiv1.SchedulingShard{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: shardName}, shard)
			if err == nil {
				Expect(k8sClient.Delete(ctx, shard)).To(Succeed())
			} else {
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
		})

		It("creates and watches one PDB for the shard", func(ctx context.Context) {
			Eventually(func(g Gomega) {
				pdb, err := getPDB(ctx)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(metav1.GetControllerOf(pdb)).NotTo(BeNil())
				g.Expect(metav1.GetControllerOf(pdb).Kind).To(Equal("SchedulingShard"))
				g.Expect(metav1.GetControllerOf(pdb).Name).To(Equal(shardName))
			}, "10s", "200ms").Should(Succeed())

			pdb, err := getPDB(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, pdb)).To(Succeed())

			Eventually(func() error {
				_, err := getPDB(ctx)
				return err
			}, "10s", "200ms").Should(Succeed())
		})

		It("removes the shard PDB after scheduler scales down", func(ctx context.Context) {
			updateSchedulerPDBConfig(ctx, 1, true)

			Eventually(func() bool {
				_, err := getPDB(ctx)
				return apierrors.IsNotFound(err)
			}, "10s", "200ms").Should(BeTrue())
		})
	})

})

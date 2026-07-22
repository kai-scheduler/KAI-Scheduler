// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deployable

import (
	"context"
	"errors"
	"testing"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/exp/maps"

	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/known_types"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestDeployable(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Deployable Suite")
}

var _ = Describe("Deployable", func() {
	var (
		fakeClientBuilder *fake.ClientBuilder
		kaiConfig         *kaiv1.Config
	)
	BeforeEach(func() {
		kaiConfig = &kaiv1.Config{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Config",
				APIVersion: kaiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "kai-config",
			},
		}

		testScheme := scheme.Scheme
		Expect(kaiv1.AddToScheme(testScheme)).To(Succeed())
		Expect(apiextensionsv1.AddToScheme(testScheme)).To(Succeed())
		Expect(monitoringv1.AddToScheme(testScheme)).To(Succeed())
		Expect(policyv1.AddToScheme(testScheme)).To(Succeed())
		Expect(vpav1.AddToScheme(testScheme)).To(Succeed())

		fakeClientBuilder = fake.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(kaiConfig)
	})

	Describe("Deploy", func() {
		var (
			deployable *DeployableOperands
			fakeClient client.Client
		)
		Context("object creation successfull", func() {

			BeforeEach(func() {
				operand := &fakeOperand{}

				deployable = New([]operands.Operand{operand}, known_types.KAIConfigRegisteredCollectible)
				fakeClient = getFakeClient(fakeClientBuilder, known_types.KAIConfigRegisteredCollectible)
			})
			It("should deploy operands desired state", func() {
				Expect(deployable.Deploy(context.TODO(), fakeClient, kaiConfig, kaiConfig)).To(Succeed())
				podsList := &v1.PodList{}
				err := fakeClient.List(context.TODO(), podsList)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(podsList.Items)).To(Equal(1))

				configMapList := &v1.ConfigMapList{}
				err = fakeClient.List(context.TODO(), configMapList)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(configMapList.Items)).To(Equal(1))
			})
			It("should not deploy operands on custom field inheritor", func() {
				desiredWithAnnotation := &v1.ConfigMap{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ConfigMap",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:        "foo",
						Namespace:   "bar",
						Annotations: map[string]string{"A": "a"},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: kaiConfig.GetObjectKind().GroupVersionKind().GroupVersion().String(),
								Kind:       kaiConfig.GetObjectKind().GroupVersionKind().Kind,
								Name:       kaiConfig.GetName(),
								UID:        kaiConfig.GetUID(),
								Controller: ptr.To(true),
							},
						},
					},
				}
				Expect(fakeClient.Create(context.TODO(), desiredWithAnnotation)).To(Succeed())

				deployable.RegisterFieldsInheritFromClusterObjects(&v1.ConfigMap{}, func(current, desired client.Object) {
					currentCm := current.(*v1.ConfigMap)
					desiredCm := desired.(*v1.ConfigMap)
					if desiredCm.Annotations == nil {
						desiredCm.Annotations = map[string]string{}
					}
					maps.Copy(desiredCm.Annotations, currentCm.Annotations)
				})
				Expect(deployable.Deploy(context.TODO(), fakeClient, kaiConfig, kaiConfig)).To(Succeed())

				cmList := &v1.ConfigMapList{}
				err := fakeClient.List(context.TODO(), cmList)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(cmList.Items)).To(Equal(1))
				Expect(len(cmList.Items[0].Annotations)).To(Equal(1))
			})

			It("should delete other resources", func() {
				otherConfigMap := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo2",
						Namespace: "bar2",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: kaiConfig.GetObjectKind().GroupVersionKind().GroupVersion().String(),
								Kind:       kaiConfig.GetObjectKind().GroupVersionKind().Kind,
								Name:       kaiConfig.GetName(),
								UID:        kaiConfig.GetUID(),
								Controller: ptr.To(true),
							},
						},
					},
				}
				Expect(fakeClient.Create(context.TODO(), otherConfigMap)).To(Succeed())

				Expect(deployable.Deploy(context.TODO(), fakeClient, kaiConfig, kaiConfig)).To(Succeed())

				configMapList := &v1.ConfigMapList{}
				err := fakeClient.List(context.TODO(), configMapList)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(configMapList.Items)).To(Equal(1))

				for _, item := range configMapList.Items {
					Expect(item.Name).ToNot(Equal(otherConfigMap.Name))
				}
			})
		})

		Context("Object creation fails", func() {
			var (
				fakeClient  client.Client
				createCalls int
				updateCalls int
				createError error
				updateError error
			)
			BeforeEach(func() {
				createCalls = 0
				updateCalls = 0
				createError = nil
				updateError = nil

				operand := &fakeOperand{}
				deployable = New([]operands.Operand{operand}, known_types.KAIConfigRegisteredCollectible)

				fakeClient = getFakeClient(fakeClientBuilder.
					WithInterceptorFuncs(interceptor.Funcs{
						Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
							createCalls++
							return createError
						},
						Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
							updateCalls++
							return updateError
						},
					}), known_types.KAIConfigRegisteredCollectible)
			})

			It("should create object successfully", func() {
				Expect(deployable.Deploy(context.TODO(), fakeClient, kaiConfig, kaiConfig)).To(Succeed())
				Expect(createCalls).To(Equal(2)) // Pod and ConfigMap creates
				Expect(updateCalls).To(Equal(0))
			})

			It("should update object if create fails due to existing resource", func() {
				createError = errors.New("already exists")
				defer func() { createError = nil }()
				Expect(deployable.Deploy(context.TODO(), fakeClient, kaiConfig, kaiConfig)).To(Succeed())
				Expect(createCalls).To(Equal(2))
				Expect(updateCalls).To(Equal(2))
			})

			It("should fail if both create and update fail", func() {
				createError = errors.New("already exists")
				updateError = errors.New("update failed")
				defer func() {
					createError = nil
					updateError = nil
				}()

				err := deployable.Deploy(context.TODO(), fakeClient, kaiConfig, kaiConfig)
				Expect(err).To(HaveOccurred())
				Expect(createCalls).To(Equal(1))
				Expect(updateCalls).To(Equal(1))
			})
		})

		Context("PodDisruptionBudget lifecycle", func() {
			var existingPDB *policyv1.PodDisruptionBudget

			BeforeEach(func() {
				existingPDB = &policyv1.PodDisruptionBudget{
					TypeMeta: metav1.TypeMeta{
						Kind:       "PodDisruptionBudget",
						APIVersion: policyv1.SchemeGroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "admission",
						Namespace: "kai-scheduler",
						OwnerReferences: []metav1.OwnerReference{{
							APIVersion: kaiv1.GroupVersion.String(),
							Kind:       "Config",
							Name:       kaiConfig.Name,
							UID:        kaiConfig.UID,
							Controller: ptr.To(true),
						}},
					},
				}
			})

			It("collects an existing PDB instead of attempting to create it again", func() {
				pdbCreateCalls := 0
				builder := fakeClientBuilder.
					WithObjects(existingPDB).
					WithInterceptorFuncs(interceptor.Funcs{
						Create: func(
							ctx context.Context,
							runtimeClient client.WithWatch,
							obj client.Object,
							opts ...client.CreateOption,
						) error {
							if _, ok := obj.(*policyv1.PodDisruptionBudget); ok {
								pdbCreateCalls++
							}
							return runtimeClient.Create(ctx, obj, opts...)
						},
					})
				fakeClient := getFakeClient(builder, known_types.KAIConfigRegisteredCollectible)
				deployable := New(
					[]operands.Operand{&fakePDBOperand{enabled: true}},
					known_types.KAIConfigRegisteredCollectible,
				)

				Expect(deployable.Deploy(context.Background(), fakeClient, kaiConfig, kaiConfig)).To(Succeed())
				Expect(pdbCreateCalls).To(BeZero())
			})

			It("deletes an owned PDB when it is no longer desired", func() {
				fakeClient := getFakeClient(
					fakeClientBuilder.WithObjects(existingPDB),
					known_types.KAIConfigRegisteredCollectible,
				)
				deployable := New(
					[]operands.Operand{&fakePDBOperand{enabled: false}},
					known_types.KAIConfigRegisteredCollectible,
				)

				Expect(deployable.Deploy(context.Background(), fakeClient, kaiConfig, kaiConfig)).To(Succeed())

				err := fakeClient.Get(
					context.Background(),
					client.ObjectKeyFromObject(existingPDB),
					&policyv1.PodDisruptionBudget{},
				)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})
	})

	Describe("IsDeployed", func() {
		var (
			fakeClient client.Client
		)
		It("should return true if all operands are deployed", func() {
			operand := &fakeOperand{isDeployed: true}
			deployable := New([]operands.Operand{operand}, known_types.KAIConfigRegisteredCollectible)
			fakeClient = getFakeClient(fakeClientBuilder, known_types.KAIConfigRegisteredCollectible)

			Expect(deployable.IsDeployed(context.TODO(), fakeClient)).To(BeTrue())
		})
		It("should return false if any operand is not deployed", func() {
			operand1 := &fakeOperand{isDeployed: true}
			operand2 := &fakeOperand{isDeployed: false, name: "operand2"}
			deployable := New([]operands.Operand{operand1, operand2}, known_types.KAIConfigRegisteredCollectible)
			deployed, err := deployable.IsDeployed(context.TODO(), fakeClient)
			Expect(deployed).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("operand2"))
		})
	})

	Describe("IsAvailable", func() {
		var (
			fakeClient client.Client
		)
		It("should return true if all operands are available", func() {
			operand := &fakeOperand{isAvailable: true}
			deployable := New([]operands.Operand{operand}, known_types.KAIConfigRegisteredCollectible)
			fakeClient = getFakeClient(fakeClientBuilder, known_types.KAIConfigRegisteredCollectible)

			Expect(deployable.IsAvailable(context.TODO(), fakeClient)).To(BeTrue())
		})
		It("should return false if any operand is not available", func() {
			operand1 := &fakeOperand{isAvailable: true, name: "operand1"}
			operand2 := &fakeOperand{isAvailable: false, name: "operand2"}
			operand3 := &fakeOperand{isAvailable: false, name: "operand3"}

			deployable := New([]operands.Operand{operand1, operand2, operand3}, known_types.KAIConfigRegisteredCollectible)
			fakeClient = getFakeClient(fakeClientBuilder, known_types.KAIConfigRegisteredCollectible)

			available, err := deployable.IsAvailable(context.TODO(), fakeClient)
			Expect(available).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("operand2"))
			Expect(err.Error()).To(ContainSubstring("operand3"))
		})
	})

	Describe("Monitor", func() {
		var (
			deployable *DeployableOperands
			fakeClient client.Client
		)
		BeforeEach(func() {
			operand := &fakeOperand{}
			deployable = New([]operands.Operand{operand}, known_types.KAIConfigRegisteredCollectible)
			fakeClient = getFakeClient(fakeClientBuilder, known_types.KAIConfigRegisteredCollectible)
		})

		It("should not return error when monitoring", func() {
			Expect(deployable.Monitor(context.TODO(), fakeClient, kaiConfig)).To(Succeed())
		})

		It("should return error if operand's Monitor fails", func() {
			errOperand := &fakeOperandWithMonitorError{monitorErr: errors.New("monitor failed")}
			deployable = New([]operands.Operand{errOperand}, known_types.KAIConfigRegisteredCollectible)
			Expect(deployable.Monitor(context.TODO(), fakeClient, kaiConfig)).To(MatchError(ContainSubstring("monitor failed")))
			Expect(errOperand.monitorErr).To(HaveOccurred())
		})
	})

	Describe("HasMissingDependencies", func() {
		var (
			deployable *DeployableOperands
			fakeClient client.Reader
		)
		BeforeEach(func() {
			operand := &fakeOperand{}
			deployable = New([]operands.Operand{operand}, known_types.KAIConfigRegisteredCollectible)
			fakeClient = getFakeClient(fakeClientBuilder, known_types.KAIConfigRegisteredCollectible)
		})

		It("should return no missing dependencies if all operands report none", func() {
			missing, err := deployable.HasMissingDependencies(context.TODO(), fakeClient, kaiConfig)
			Expect(missing).To(BeEmpty())
			Expect(err).ToNot(HaveOccurred())
		})

		It("should aggregate missing dependencies from operands", func() {
			operand1 := &fakeOperandWithDeps{missingDeps: "dep1", name: "operand1"}
			operand2 := &fakeOperandWithDeps{missingDeps: "dep2", name: "operand2"}
			deployable = New([]operands.Operand{operand1, operand2}, known_types.KAIConfigRegisteredCollectible)
			missing, err := deployable.HasMissingDependencies(context.TODO(), fakeClient, kaiConfig)
			Expect(missing).To(ContainSubstring("operand1 is missing dep1"))
			Expect(missing).To(ContainSubstring("operand2 is missing dep2"))
			Expect(err).ToNot(HaveOccurred())
		})

		It("should handle error from operand's HasMissingDependencies", func() {
			errOperand := &fakeOperandWithDeps{hasDepErr: errors.New("dependency check failed"), name: "errOperand"}
			deployable = New([]operands.Operand{errOperand}, known_types.KAIConfigRegisteredCollectible)
			_, err := deployable.HasMissingDependencies(context.TODO(), fakeClient, kaiConfig)
			Expect(err).To(MatchError(ContainSubstring("dependency check failed")))
		})
	})

	Describe("SortObjectByCreationOrder", func() {
		var (
			orderDefinition []string
			objects         []client.Object
			sortedObjects   []client.Object
		)

		It("reverse list", func() {
			orderDefinition = []string{
				"ServiceAccount",
			}
			objects = []client.Object{
				&appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						Kind: "Deployment",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "Deployment1",
					},
				},
				&v1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "ServiceAccount1",
					},
				},
			}
			sortedObjects = []client.Object{
				&v1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "ServiceAccount1",
					},
				},
				&appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						Kind: "Deployment",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "Deployment1",
					},
				},
			}

			sortObjectByCreationOrder(objects, orderDefinition)
			Expect(objects).To(BeEquivalentTo(sortedObjects))
		})

		It("two order types list", func() {
			orderDefinition = []string{
				"ServiceAccount", "Pod",
			}
			objects = []client.Object{
				&appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						Kind: "Deployment",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "Deployment1",
					},
				},
				&v1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "ServiceAccount1",
					},
				},
				&v1.Pod{
					TypeMeta: metav1.TypeMeta{
						Kind: "Pod",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "Pod1",
					},
				},
			}
			sortedObjects = []client.Object{
				&v1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "ServiceAccount1",
					},
				},
				&v1.Pod{
					TypeMeta: metav1.TypeMeta{
						Kind: "Pod",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "Pod1",
					},
				},
				&appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						Kind: "Deployment",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "Deployment1",
					},
				},
			}

			sortObjectByCreationOrder(objects, orderDefinition)
			Expect(objects).To(BeEquivalentTo(sortedObjects))
		})
	})
})

type fakeOperand struct {
	isDeployed, isAvailable bool
	name                    string
}

type fakePDBOperand struct {
	enabled bool
}

func (f *fakePDBOperand) DesiredState(
	ctx context.Context,
	runtimeClient client.Reader,
	_ *kaiv1.Config,
) ([]client.Object, error) {
	if !f.enabled {
		return nil, nil
	}

	pdb := &policyv1.PodDisruptionBudget{}
	err := runtimeClient.Get(ctx, client.ObjectKey{Name: "admission", Namespace: "kai-scheduler"}, pdb)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	pdb.TypeMeta = metav1.TypeMeta{
		Kind:       "PodDisruptionBudget",
		APIVersion: policyv1.SchemeGroupVersion.String(),
	}
	pdb.Name = "admission"
	pdb.Namespace = "kai-scheduler"
	return []client.Object{pdb}, nil
}

func (f *fakePDBOperand) IsDeployed(context.Context, client.Reader) (bool, error) {
	return true, nil
}

func (f *fakePDBOperand) IsAvailable(context.Context, client.Reader) (bool, error) {
	return true, nil
}

func (f *fakePDBOperand) Monitor(context.Context, client.Reader, *kaiv1.Config) error {
	return nil
}

func (f *fakePDBOperand) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}

func (f *fakePDBOperand) Name() string {
	return "fakePDBOperand"
}

func (f *fakeOperand) DesiredState(_ context.Context, _ client.Reader, _ *kaiv1.Config) ([]client.Object, error) {
	return []client.Object{
		&v1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "bar",
			},
		},
		&v1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "bar",
			},
		},
	}, nil
}

func (f *fakeOperand) IsDeployed(_ context.Context, _ client.Reader) (bool, error) {
	return f.isDeployed, nil
}

func (f *fakeOperand) IsAvailable(_ context.Context, _ client.Reader) (bool, error) {
	return f.isAvailable, nil
}

func (f *fakeOperand) Monitor(_ context.Context, _ client.Reader, _ *kaiv1.Config) error {
	return nil
}

func (f *fakeOperand) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}

func (f *fakeOperand) Name() string {
	if f.name == "" {
		return "fakeOperand"
	}
	return f.name
}

func getFakeClient(builder *fake.ClientBuilder, collectables []*known_types.Collectable) client.Client {
	for _, collectable := range collectables {
		if collectable.InitWithFakeClientBuilder == nil {
			continue
		}
		collectable.InitWithFakeClientBuilder(builder)
	}
	return builder.Build()
}

type fakeOperandWithMonitorError struct {
	monitorErr error
}

func (f *fakeOperandWithMonitorError) Monitor(_ context.Context, _ client.Reader, _ *kaiv1.Config) error {
	return f.monitorErr
}

func (f *fakeOperandWithMonitorError) Name() string { return "fakeOperandWithMonitorError" }
func (f *fakeOperandWithMonitorError) DesiredState(_ context.Context, _ client.Reader, _ *kaiv1.Config) ([]client.Object, error) {
	return nil, nil
}
func (f *fakeOperandWithMonitorError) IsDeployed(_ context.Context, _ client.Reader) (bool, error) {
	return true, nil
}
func (f *fakeOperandWithMonitorError) IsAvailable(_ context.Context, _ client.Reader) (bool, error) {
	return true, nil
}
func (f *fakeOperandWithMonitorError) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}

type fakeOperandWithDeps struct {
	missingDeps string
	hasDepErr   error
	name        string
}

func (f *fakeOperandWithDeps) HasMissingDependencies(_ context.Context, _ client.Reader, _ *kaiv1.Config) (string, error) {
	return f.missingDeps, f.hasDepErr
}
func (f *fakeOperandWithDeps) Name() string { return f.name }
func (f *fakeOperandWithDeps) DesiredState(_ context.Context, _ client.Reader, _ *kaiv1.Config) ([]client.Object, error) {
	return nil, nil
}
func (f *fakeOperandWithDeps) IsDeployed(_ context.Context, _ client.Reader) (bool, error) {
	return true, nil
}
func (f *fakeOperandWithDeps) IsAvailable(_ context.Context, _ client.Reader) (bool, error) {
	return true, nil
}
func (f *fakeOperandWithDeps) Monitor(_ context.Context, _ client.Reader, _ *kaiv1.Config) error {
	return nil
}

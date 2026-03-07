// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"context"
	"errors"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"

	rrmock "github.com/kai-scheduler/kai-scheduler/pkg/binder/binding/resourcereservation/mock"
	"github.com/kai-scheduler/kai-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/kai-scheduler/pkg/binder/plugins"
	bindinggpusharing "github.com/kai-scheduler/kai-scheduler/pkg/binder/plugins/gpusharing"
	"github.com/kai-scheduler/kai-scheduler/pkg/binder/test_utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

const (
	fakeGPUGroup = "abcd"
)

var happyFlowObjectsBc = []runtime.Object{
	&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-pod",
			Annotations: map[string]string{
				gpuSharingConfigMapAnnotation: "my-configmap-shared-gpu",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Env: []v1.EnvVar{
					{
						Name: constants.NvidiaVisibleDevices,
						ValueFrom: &v1.EnvVarSource{
							ConfigMapKeyRef: &v1.ConfigMapKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "my-configmap-shared-gpu-0",
								},
							},
						},
					}},
			}},
			Volumes: []v1.Volume{
				{
					Name: "my-configmap-vol",
					VolumeSource: v1.VolumeSource{
						ConfigMap: &v1.ConfigMapVolumeSource{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "my-configmap-shared-gpu-0",
							},
						},
					},
				},
			},
		},
	},
	&v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-node",
		},
	},
}

var happyFlowObjects = []runtime.Object{
	&v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-pod",
			Annotations: map[string]string{
				gpuSharingConfigMapAnnotation: "my-configmap-shared-gpu",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "my-container",
				},
			},
			Volumes: []v1.Volume{
				{
					Name: "my-configmap-vol",
					VolumeSource: v1.VolumeSource{
						ConfigMap: &v1.ConfigMapVolumeSource{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "my-configmap-shared-gpu-0",
							},
						},
					},
				},
			},
		},
	},
	&v1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-node",
		},
	},
}

var _ = Describe("FractionBinder", func() {
	Context("Bind-BC", func() {
		for testName, testData := range map[string]struct {
			kubeObjects           []runtime.Object
			clientInterceptFuncs  interceptor.Funcs
			gpuIndexByGroupIndex  string
			gpuIndexByGroupErr    error
			expectedErrorContains string
		}{
			"bind new pod": {
				kubeObjects:          happyFlowObjectsBc,
				gpuIndexByGroupIndex: "0",
			},
			"fail to get gpu index from reservation pod": {
				kubeObjects:           happyFlowObjectsBc,
				gpuIndexByGroupErr:    fmt.Errorf("failed to get from reservation"),
				expectedErrorContains: "get from reservation",
			},
			"bind operation fails": {
				kubeObjects:           happyFlowObjectsBc,
				clientInterceptFuncs:  test_utils.FailedBindingFuncs,
				gpuIndexByGroupIndex:  "0",
				expectedErrorContains: "bind",
			},
			"patch operation fails": {
				kubeObjects: happyFlowObjectsBc,
				clientInterceptFuncs: interceptor.Funcs{
					Patch: func(ctx context.Context, client client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						return fmt.Errorf("patch")
					},
				},
				gpuIndexByGroupIndex:  "0",
				expectedErrorContains: "patch",
			},
		} {
			testName := testName
			testData := testData
			It(testName, func() {
				controller := gomock.NewController(GinkgoT())
				rrs := rrmock.NewMockInterface(controller)

				rrs.EXPECT().SyncForNode(gomock.Any(), gomock.Any()).Times(1).Return(nil)
				rrs.EXPECT().ReserveGpuDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).MaxTimes(1).
					Return(testData.gpuIndexByGroupIndex, testData.gpuIndexByGroupErr)

				if testData.clientInterceptFuncs.SubResource == nil {
					testData.clientInterceptFuncs.SubResource = func(c client.WithWatch, subResource string) client.SubResourceClient {
						return &test_utils.FakeSubResourceClient{}
					}
				}

				fakeClient := fake.NewClientBuilder().WithRuntimeObjects(
					testData.kubeObjects...).WithInterceptorFuncs(testData.clientInterceptFuncs).Build()

				binderPlugins := plugins.New()
				bindingGpuSharingPlugin := bindinggpusharing.New(fakeClient, false)
				binderPlugins.RegisterPlugin(bindingGpuSharingPlugin)

				testedBinder := NewBinder(fakeClient, rrs, binderPlugins)

				pod := testData.kubeObjects[0].(*v1.Pod)

				bindRequest := &v1alpha2.BindRequest{
					Spec: v1alpha2.BindRequestSpec{
						SelectedNode:         "my-node",
						ReceivedResourceType: common.ReceivedTypeFraction,
						ReceivedGPU: &v1alpha2.ReceivedGPU{
							Count:   1,
							Portion: "0.5",
						},
						SelectedGPUGroups: []string{fakeGPUGroup},
					},
				}

				result := testedBinder.Bind(
					context.TODO(), pod, &v1.Node{ObjectMeta: metav1.ObjectMeta{
						Name: "my-node",
					}}, bindRequest)
				if testData.expectedErrorContains != "" {
					Expect(result).NotTo(BeNil())
					Expect(result.Error()).To(ContainSubstring(testData.expectedErrorContains))
					return
				}

				Expect(result).To(BeNil())

				configMap := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-configmap-shared-gpu-0",
						Namespace: "my-ns",
					},
				}

				if err := fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(configMap), configMap); err != nil {
					Fail(fmt.Sprintf("Failed to read configmap: %v", err))
				} else {
					Expect(configMap.Data[constants.NvidiaVisibleDevices]).To(Equal(testData.gpuIndexByGroupIndex))
					Expect(configMap.Data[common.NumOfGpusEnvVarBC]).To(Equal("0.5"))
					Expect(configMap.Data[common.GPUPortion]).To(Equal("0.5"))
				}
			})
		}
	})
	It("Bind - v1alpha1 converted based bindingRequest", func() {
		controller := gomock.NewController(GinkgoT())
		rrs := rrmock.NewMockInterface(controller)
		rrs.EXPECT().SyncForNode(gomock.Any(), gomock.Any()).Times(1).Return(nil)

		var clientInterceptFuncs interceptor.Funcs
		clientInterceptFuncs.SubResource = func(c client.WithWatch, subResource string) client.SubResourceClient {
			return &test_utils.FakeSubResourceClient{}
		}

		fakeClient := fake.NewClientBuilder().WithRuntimeObjects(
			happyFlowObjects...).WithInterceptorFuncs(clientInterceptFuncs).Build()

		binderPlugins := plugins.New()
		bindingGpuSharingPlugin := bindinggpusharing.New(fakeClient, false)
		binderPlugins.RegisterPlugin(bindingGpuSharingPlugin)

		testedBinder := NewBinder(fakeClient, rrs, binderPlugins)

		pod := happyFlowObjects[0].(*v1.Pod)

		bindRequest := &v1alpha2.BindRequest{
			Spec: v1alpha2.BindRequestSpec{
				SelectedNode:         "my-node",
				ReceivedResourceType: common.ReceivedTypeFraction,
				ReceivedGPU: &v1alpha2.ReceivedGPU{
					Count:   1,
					Portion: "0.5",
				},
				SelectedGPUGroups: []string{},
			},
		}

		result := testedBinder.Bind(
			context.TODO(), pod, &v1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: "my-node",
			}}, bindRequest)

		Expect(result).NotTo(BeNil())
		Expect(errors.Is(result, InvalidCrdWarning)).To(BeTrue())
	})
})

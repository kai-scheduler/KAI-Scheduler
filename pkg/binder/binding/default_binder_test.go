// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"

	rrmock "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	bindinggpusharing "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/test_utils"
)

const (
	gpuSharingConfigMapAnnotation = "runai/shared-gpu-configmap"
)

func TestBind(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-pod",
			Annotations: map[string]string{
				gpuSharingConfigMapAnnotation: "my-configmap-shared-gpu",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{},
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
	}
	kubeObjects := []runtime.Object{
		pod,
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-node",
			},
		},
	}
	controller := gomock.NewController(t)
	rrs := rrmock.NewMockInterface(controller)
	rrs.EXPECT().SyncForNode(gomock.Any(), gomock.Any()).Times(1).Return(nil)

	kubeClient := fake.NewClientBuilder().WithRuntimeObjects(kubeObjects...).WithInterceptorFuncs(test_utils.EmptyBind).Build()

	binderPlugins := plugins.New()
	bindingGpuSharingPlugin := bindinggpusharing.New(kubeClient, false)
	binderPlugins.RegisterPlugin(bindingGpuSharingPlugin)

	binder := NewBinder(kubeClient, rrs, binderPlugins)

	err := binder.Bind(
		context.TODO(),
		pod,
		&v1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: "my-node",
		}},
		&v1alpha2.BindRequest{
			Spec: v1alpha2.BindRequestSpec{
				SelectedNode:         "my-node",
				ReceivedResourceType: common.ReceivedTypeRegular,
			},
		},
	)

	assert.Nil(t, err)
}

func TestBindApplyResourceReceivedType(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-pod",
			Annotations: map[string]string{
				gpuSharingConfigMapAnnotation: "my-config",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Env: []v1.EnvVar{
						{
							Name: constants.NvidiaVisibleDevices,
							ValueFrom: &v1.EnvVarSource{
								ConfigMapKeyRef: &v1.ConfigMapKeySelector{
									Key: constants.NvidiaVisibleDevices,
									LocalObjectReference: v1.LocalObjectReference{
										Name: "my-config-0",
									},
								},
							},
						},
						{
							Name: common.GPUPortion,
							ValueFrom: &v1.EnvVarSource{
								ConfigMapKeyRef: &v1.ConfigMapKeySelector{
									Key: common.GPUPortion,
									LocalObjectReference: v1.LocalObjectReference{
										Name: "my-config-0",
									},
								},
							},
						},
					},
				},
			},
			Volumes: []v1.Volume{
				{
					Name: "my-configmap-vol",
					VolumeSource: v1.VolumeSource{
						ConfigMap: &v1.ConfigMapVolumeSource{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "my-config-0",
							},
						},
					},
				},
			},
		},
	}
	kubeObjects := []runtime.Object{
		pod,
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-node",
			},
		},
	}

	bindRequest := &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{
			SelectedNode:         "my-node",
			ReceivedResourceType: common.ReceivedTypeFraction,
			SelectedGPUGroups:    []string{"group1"},
			ReceivedGPU: &v1alpha2.ReceivedGPU{
				Count:   1,
				Portion: "1",
			},
		},
	}

	controller := gomock.NewController(t)
	rrs := rrmock.NewMockInterface(controller)
	rrs.EXPECT().SyncForNode(gomock.Any(), gomock.Any()).Times(1).Return(nil)
	rrs.EXPECT().ReserveGpuDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1).
		Return("1", nil)

	kubeClient := fake.NewClientBuilder().WithRuntimeObjects(kubeObjects...).WithInterceptorFuncs(test_utils.EmptyBind).Build()

	binderPlugins := plugins.New()
	bindingGpuSharingPlugin := bindinggpusharing.New(kubeClient, false)
	binderPlugins.RegisterPlugin(bindingGpuSharingPlugin)

	binder := NewBinder(kubeClient, rrs, binderPlugins)

	err := binder.Bind(context.TODO(), pod, &v1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "my-node",
	}}, bindRequest)

	assert.Nil(t, err)

	newPod := &v1.Pod{}
	err = kubeClient.Get(context.TODO(), client.ObjectKey{
		Namespace: "my-ns",
		Name:      "my-pod",
	}, newPod)
	assert.Nil(t, err)
	assert.Equal(t, common.ReceivedTypeFraction, newPod.Annotations[constants.ReceivedResourceType])
}

func TestBindFail(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "my-ns",
			Name:      "my-pod",
		},
	}
	kubeObjects := []runtime.Object{
		&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "my-ns",
				Name:      "my-pod-2",
			},
		},
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-node",
			},
		},
	}
	controller := gomock.NewController(t)
	rrs := rrmock.NewMockInterface(controller)
	rrs.EXPECT().SyncForNode(gomock.Any(), gomock.Any()).Times(1).Return(nil)

	kubeClient := fake.NewClientBuilder().WithRuntimeObjects(kubeObjects...).WithInterceptorFuncs(test_utils.EmptyBind).Build()

	binderPlugins := plugins.New()
	bindingGpuSharingPlugin := bindinggpusharing.New(kubeClient, false)
	binderPlugins.RegisterPlugin(bindingGpuSharingPlugin)

	binder := NewBinder(kubeClient, rrs, binderPlugins)

	err := binder.Bind(context.TODO(), pod, &v1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "my-node",
	}}, &v1alpha2.BindRequest{
		Spec: v1alpha2.BindRequestSpec{
			SelectedNode:         "my-node",
			ReceivedResourceType: common.ReceivedTypeRegular,
		},
	})

	assert.NotNil(t, err)
}

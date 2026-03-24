/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package rd

import (
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func CreateResourceClaim(namespace, deviceClassName string, deviceCount int) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.GenerateRandomK8sName(10),
			Namespace:   namespace,
			Annotations: map[string]string{},
			Labels: map[string]string{
				constants.AppLabelName: "engine-e2e",
			},
		},
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{
					{
						Name: "req",
						Exactly: &resourceapi.ExactDeviceRequest{
							DeviceClassName: deviceClassName,
							Selectors:       nil,
							AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
							Count:           int64(deviceCount),
						},
					},
				},
			},
		},
	}
}

func CreateResourceClaimTemplate(namespace, deviceClassName string, deviceCount int) *resourceapi.ResourceClaimTemplate {
	return &resourceapi.ResourceClaimTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.GenerateRandomK8sName(10),
			Namespace:   namespace,
			Annotations: map[string]string{},
			Labels: map[string]string{
				constants.AppLabelName: "engine-e2e",
			},
		},
		Spec: resourceapi.ResourceClaimTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.AppLabelName: "engine-e2e",
				},
			},
			Spec: resourceapi.ResourceClaimSpec{
				Devices: resourceapi.DeviceClaim{
					Requests: []resourceapi.DeviceRequest{
						{
							Name: "req",
							Exactly: &resourceapi.ExactDeviceRequest{
								DeviceClassName: deviceClassName,
								Selectors:       nil,
								AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
								Count:           int64(deviceCount),
							},
						},
					},
				},
			},
		},
	}
}

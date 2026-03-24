// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresource

import (
	"fmt"

	"github.com/xyproto/randomstring"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func CreateDeviceClass(name string) *resourceapi.DeviceClass {
	deviceClass := resourceapi.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	return &deviceClass
}

func CreateNodeResourceSlice(name, driver, nodeName string, deviceCount int) *resourceapi.ResourceSlice {
	resourceSlice := resourceapi.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: resourceapi.ResourceSliceSpec{
			Driver: driver,
			Pool: resourceapi.ResourcePool{
				Generation:         0,
				ResourceSliceCount: 1,
				Name:               fmt.Sprintf("%s-%s", driver, nodeName),
			},
			NodeName: ptr.To(nodeName),
		},
	}

	for i := range deviceCount {
		resourceSlice.Spec.Devices = append(resourceSlice.Spec.Devices, resourceapi.Device{
			Name: fmt.Sprintf("%s-%s-%d", driver, nodeName, i),
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"vendor": {
					StringValue: ptr.To("nvidia"),
				},
				"model": {
					StringValue: ptr.To("A420"),
				},
			},
		})
	}

	return &resourceSlice
}

type DeviceRequest struct {
	// Name is the name of the device to request, empty string will auto generate
	Name string
	// Class is the DeviceClass name of the device to request
	Class string
	// Count is the number of devices to request, -1 means all
	Count int
}

func CreateResourceClaim(name, namespace string, requests ...DeviceRequest) *resourceapi.ResourceClaim {
	resourceClaim := resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{},
			},
		},
	}

	for _, request := range requests {
		if request.Name == "" {
			request.Name = randomstring.HumanFriendlyEnglishString(10)
		}

		newRequest := resourceapi.DeviceRequest{
			Name: request.Name,
			Exactly: &resourceapi.ExactDeviceRequest{
				DeviceClassName: request.Class,
				Count:           int64(request.Count),
				AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
			},
		}

		if request.Count == -1 {
			newRequest.Exactly.AllocationMode = resourceapi.DeviceAllocationModeAll
			newRequest.Exactly.Count = 0
		}

		resourceClaim.Spec.Devices.Requests = append(
			resourceClaim.Spec.Devices.Requests,
			newRequest,
		)
	}

	return &resourceClaim
}

func UseClaim(pod *corev1.Pod, claim *resourceapi.ResourceClaim) {
	pod.Spec.ResourceClaims = append(pod.Spec.ResourceClaims,
		corev1.PodResourceClaim{
			Name:              claim.Name,
			ResourceClaimName: &claim.Name,
		},
	)
}

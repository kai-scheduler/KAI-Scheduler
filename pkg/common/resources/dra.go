// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	resourceapiv1beta1 "k8s.io/api/resource/v1beta1"
	resourceapiv1beta2 "k8s.io/api/resource/v1beta2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	draclient "k8s.io/dynamic-resource-allocation/client"
	resourceinstall "k8s.io/kubernetes/pkg/apis/resource/install"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetResourceClaimName(pod *v1.Pod, podClaim *v1.PodResourceClaim) (string, error) {
	if podClaim.ResourceClaimName != nil {
		return *podClaim.ResourceClaimName, nil
	}
	if podClaim.ResourceClaimTemplateName != nil {
		for _, status := range pod.Status.ResourceClaimStatuses {
			if status.Name == podClaim.Name && status.ResourceClaimName != nil {
				return *status.ResourceClaimName, nil
			}
		}
	}
	return "", fmt.Errorf("no resource claim name found for pod %s/%s and claim reference %s",
		pod.Namespace, pod.Name, podClaim.Name)
}

func UpsertReservedFor(claim *resourceapi.ResourceClaim, pod *v1.Pod) {
	for _, ref := range claim.Status.ReservedFor {
		if ref.Name == pod.Name &&
			ref.UID == pod.UID &&
			ref.Resource == "pods" &&
			ref.APIGroup == "" {
			return
		}
	}

	claim.Status.ReservedFor = append(
		claim.Status.ReservedFor,
		resourceapi.ResourceClaimConsumerReference{
			APIGroup: "",
			Resource: "pods",
			Name:     pod.Name,
			UID:      pod.UID,
		},
	)
}

func RemoveReservedFor(claim *resourceapi.ResourceClaim, pod *v1.Pod) {
	newReservedFor := make([]resourceapi.ResourceClaimConsumerReference, 0, len(claim.Status.ReservedFor))
	for _, ref := range claim.Status.ReservedFor {
		if ref.Name == pod.Name &&
			ref.UID == pod.UID &&
			ref.Resource == "pods" &&
			ref.APIGroup == "" {
			continue
		}

		newReservedFor = append(newReservedFor, ref)
	}
	claim.Status.ReservedFor = newReservedFor
}

// ExtractDRAGPUResources extracts GPU resources from DRA ResourceClaims in a pod.
// It loops through all ResourceClaims in the pod spec, identifies GPU claims by DeviceClassName,
// and returns a ResourceList with GPU resources aggregated.
func ExtractDRAGPUResources(ctx context.Context, pod *v1.Pod, kubeClient client.Client) (v1.ResourceList, error) {
	if len(pod.Spec.ResourceClaims) == 0 {
		return v1.ResourceList{}, nil
	}

	var podResourceClaims []*resourceapi.ResourceClaim
	for _, podClaim := range pod.Spec.ResourceClaims {
		claimName, err := GetResourceClaimName(pod, &podClaim)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource claim name for pod %s/%s, claim %s: %w",
				pod.Namespace, pod.Name, podClaim.Name, err)
		}

		claim := &resourceapi.ResourceClaim{}
		claimKey := types.NamespacedName{
			Namespace: pod.Namespace,
			Name:      claimName,
		}

		err = kubeClient.Get(ctx, claimKey, claim)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource claim %s/%s for pod %s/%s: %w",
				pod.Namespace, claimName, pod.Namespace, pod.Name, err)
		}

		podResourceClaims = append(podResourceClaims, claim)
	}

	deviceClassCounts := ExtractDRAGPUResourcesFromClaims(podResourceClaims)

	// Convert aggregated counts to ResourceList mapping deviceClass name to its count
	gpuResources := v1.ResourceList{}
	for deviceClassName, count := range deviceClassCounts {
		if count > 0 {
			gpuResources[v1.ResourceName(deviceClassName)] = *resource.NewQuantity(count, resource.DecimalSI)
		}
	}
	return gpuResources, nil
}

func ExtractDRAGPUResourcesFromClaims(podResourceClaims []*resourceapi.ResourceClaim) map[string]int64 {
	// Map to group claims by DeviceClassName and count devices
	deviceClassCounts := make(map[string]int64)

	for _, claim := range podResourceClaims {
		gpuCount := countGPUDevicesFromClaim(claim)
		if gpuCount > 0 {
			// Find the DeviceClassName for this claim
			deviceClassName := getGPUDeviceClassNameFromClaim(claim)
			if deviceClassName != "" {
				deviceClassCounts[deviceClassName] += gpuCount
			}
		}
	}

	return deviceClassCounts
}

func IsGpuResourceClaim(claim *resourceapi.ResourceClaim) bool {
	for _, request := range claim.Spec.Devices.Requests {
		if request.Exactly != nil && IsGPUDeviceClass(request.Exactly.DeviceClassName) {
			return true
		}
	}
	return false
}

func IsGPUDeviceClass(deviceClassName string) bool {
	return strings.Contains(strings.ToLower(deviceClassName), "gpu")
}

// getGPUDeviceClassNameFromClaim extracts the GPU DeviceClassName from a ResourceClaim.
// Returns empty string if no GPU device class is found.
func getGPUDeviceClassNameFromClaim(claim *resourceapi.ResourceClaim) string {
	for _, request := range claim.Spec.Devices.Requests {
		if request.Exactly != nil && IsGPUDeviceClass(request.Exactly.DeviceClassName) {
			return request.Exactly.DeviceClassName
		}
	}
	return ""
}

// countGPUDevicesFromClaim counts GPU devices from a ResourceClaim.
// Returns the total count of GPU devices requested by this claim.
func countGPUDevicesFromClaim(claim *resourceapi.ResourceClaim) int64 {
	totalCount := int64(0)

	for _, request := range claim.Spec.Devices.Requests {
		if request.Exactly == nil {
			continue
		}

		if !IsGPUDeviceClass(request.Exactly.DeviceClassName) {
			continue
		}

		switch request.Exactly.AllocationMode {
		case resourceapi.DeviceAllocationModeExactCount:
			if request.Exactly.Count > 0 {
				totalCount += request.Exactly.Count
			} else {
				// Default to 1 if Count is not specified for ExactCount mode
				totalCount += 1
			}
		case resourceapi.DeviceAllocationModeAll:
			// For "All" mode, we can't determine the exact count without allocation info.
			// For bookkeeping purposes, we'll treat it as requesting 1 device.
			// This is a conservative estimate for queue resource tracking.
			totalCount += 1
		default:
			// Unknown allocation mode, skip this request
			continue
		}
	}

	return totalCount
}

var draConversionScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	resourceinstall.Install(s)
	return s
}()

func NewDRAClient(config *rest.Config) *draclient.Client {
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil
	}
	return draclient.New(kubeClient)
}

// DetectDRAAPIVersion triggers DRA version negotiation and returns the detected
// API version string (e.g. "V1", "V1beta2", "V1beta1") or empty string if DRA is unavailable.
func DetectDRAAPIVersion(draClient *draclient.Client) string {
	if draClient == nil {
		return ""
	}
	_, err := draClient.ResourceClaims("").List(context.Background(), metav1.ListOptions{Limit: 1})
	if err != nil {
		return ""
	}
	return draClient.CurrentAPI()
}

func DRACacheObject(draAPIVersion string) client.Object {
	switch draAPIVersion {
	case "V1":
		return &resourceapi.ResourceClaim{}
	case "V1beta2":
		return &resourceapiv1beta2.ResourceClaim{}
	case "V1beta1":
		return &resourceapiv1beta1.ResourceClaim{}
	default:
		return nil
	}
}

func FetchPodResourceClaims(
	ctx context.Context, pod *v1.Pod, kubeClient client.Client, draAPIVersion string,
) ([]*resourceapi.ResourceClaim, error) {
	if len(pod.Spec.ResourceClaims) == 0 || draAPIVersion == "" {
		return nil, nil
	}

	var claims []*resourceapi.ResourceClaim
	for _, podClaim := range pod.Spec.ResourceClaims {
		claimName, err := GetResourceClaimName(pod, &podClaim)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource claim name for pod %s/%s, claim %s: %w",
				pod.Namespace, pod.Name, podClaim.Name, err)
		}

		key := types.NamespacedName{Namespace: pod.Namespace, Name: claimName}
		claim, err := fetchResourceClaim(ctx, kubeClient, key, draAPIVersion)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource claim %s/%s for pod %s/%s: %w",
				pod.Namespace, claimName, pod.Namespace, pod.Name, err)
		}
		claims = append(claims, claim)
	}
	return claims, nil
}

func fetchResourceClaim(
	ctx context.Context, kubeClient client.Client, key types.NamespacedName, draAPIVersion string,
) (*resourceapi.ResourceClaim, error) {
	switch draAPIVersion {
	case "V1":
		claim := &resourceapi.ResourceClaim{}
		return claim, kubeClient.Get(ctx, key, claim)
	case "V1beta2":
		beta := &resourceapiv1beta2.ResourceClaim{}
		if err := kubeClient.Get(ctx, key, beta); err != nil {
			return nil, err
		}
		v1Claim := &resourceapi.ResourceClaim{}
		return v1Claim, draConversionScheme.Convert(beta, v1Claim, nil)
	case "V1beta1":
		beta := &resourceapiv1beta1.ResourceClaim{}
		if err := kubeClient.Get(ctx, key, beta); err != nil {
			return nil, err
		}
		v1Claim := &resourceapi.ResourceClaim{}
		return v1Claim, draConversionScheme.Convert(beta, v1Claim, nil)
	default:
		return nil, fmt.Errorf("unsupported DRA API version %q", draAPIVersion)
	}
}

func DRAGPUResourceListFromClaims(claims []*resourceapi.ResourceClaim) v1.ResourceList {
	deviceClassCounts := ExtractDRAGPUResourcesFromClaims(claims)
	gpuResources := v1.ResourceList{}
	for deviceClassName, count := range deviceClassCounts {
		if count > 0 {
			gpuResources[v1.ResourceName(deviceClassName)] = *resource.NewQuantity(count, resource.DecimalSI)
		}
	}
	return gpuResources
}

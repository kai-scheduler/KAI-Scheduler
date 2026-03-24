// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package featuregates

import (
	"strconv"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	featureutil "k8s.io/apiserver/pkg/util/feature"
	discovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/kubernetes/pkg/features"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	minimalSupportedVersion = "v1beta1"
)

func SetDRAFeatureGate(config *rest.Config) error {
	// Create a DiscoveryClient
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return err
	}
	enabled := IsDynamicResourcesEnabled(discoveryClient)
	return featureutil.DefaultMutableFeatureGate.SetFromMap(
		map[string]bool{string(features.DynamicResourceAllocation): enabled})
}

func IsDynamicResourcesEnabled(discoveryClient discovery.DiscoveryInterface) bool {
	logger := log.Log.WithName("feature-gates")

	// Get API server version
	serverVersion, err := discoveryClient.ServerVersion()
	if err != nil {
		logger.Error(err, "Failed to get server version")
		return false
	}

	// Check if the API server version is compatible with DRA
	if majorVer, errMajor := strconv.Atoi(serverVersion.Major); errMajor != nil || majorVer < 1 {
		return false
	}
	if minorVer, errMinor := strconv.Atoi(serverVersion.Minor); errMinor != nil || minorVer < 26 {
		return false
	}

	// Get supported API versions
	serverGroups, err := discoveryClient.ServerGroups()
	if err != nil {
		logger.Error(err, "Failed to get server groups")
		return false
	}

	found := false
	var resourceGroup v1.APIGroup
	for _, group := range serverGroups.Groups {
		if group.Name == "resource.k8s.io" {
			resourceGroup = group
			found = true
			break
		}
	}
	if !found {
		return false
	}

	// Check if the DRA API group is supported
	for _, groupVersion := range resourceGroup.Versions {
		if version.CompareKubeAwareVersionStrings(groupVersion.Version, minimalSupportedVersion) >= 0 {
			return true
		}
	}

	return false
}

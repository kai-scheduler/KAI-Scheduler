// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package featuregates

import (
	"strconv"
	"strings"
	"sync/atomic"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	discovery "k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	minimalSupportedVersion = "v1beta1"
)

// dynamicResourcesEnabled is the process-wide decision on whether DRA is usable,
// set by SetDRAFeatureGate. It is the authoritative source for scheduler and
// binder components because the upstream DynamicResourceAllocation feature gate
// is GA and locked to true in Kubernetes v1.35+, so it can no longer be toggled
// off to reflect server-side DRA availability.
var dynamicResourcesEnabled atomic.Bool

func SetDRAFeatureGate(discoveryClient discovery.DiscoveryInterface) {
	dynamicResourcesEnabled.Store(IsDynamicResourcesEnabled(discoveryClient))
}

// DynamicResourcesEnabled reports whether DRA was determined to be usable
// against the cluster at startup. Use this instead of the upstream feature gate
// to gate DRA-specific scheduler behaviour.
func DynamicResourcesEnabled() bool {
	return dynamicResourcesEnabled.Load()
}

func SetDynamicResourcesEnabledForTest(enabled bool) {
	dynamicResourcesEnabled.Store(enabled)
}

var workloadAPIEnabled atomic.Bool

func SetWorkloadAPIFlag(discoveryClient discovery.DiscoveryInterface) {
	workloadAPIEnabled.Store(IsWorkloadAPIEnabled(discoveryClient))
}

func WorkloadAPIEnabled() bool {
	return workloadAPIEnabled.Load()
}

func SetWorkloadAPIEnabledForTest(enabled bool) {
	workloadAPIEnabled.Store(enabled)
}

func IsWorkloadAPIEnabled(discoveryClient discovery.DiscoveryInterface) bool {
	logger := log.Log.WithName("feature-gates")
	const groupVersion = "scheduling.k8s.io/v1alpha1"

	resources, err := discoveryClient.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		logger.V(4).Info("Workload API group-version not found", "groupVersion", groupVersion, "err", err)
		return false
	}
	for _, r := range resources.APIResources {
		if r.Name == "workloads" {
			return true
		}
	}
	return false
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
	if !isCompatibleDRAVersion(serverVersion) {
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

func isCompatibleDRAVersion(serverVersion *version.Info) bool {
	if majorVer, errMajor := strconv.Atoi(serverVersion.Major); errMajor != nil || majorVer < 1 {
		return false
	}

	normalizedMinorVersion := serverVersion.Minor
	minorVersionSuffix := strings.TrimLeft(normalizedMinorVersion, "0123456789")
	if len(minorVersionSuffix) > 0 {
		normalizedMinorVersion = strings.TrimSuffix(normalizedMinorVersion, minorVersionSuffix)
	}
	if minorVer, errMinor := strconv.Atoi(normalizedMinorVersion); errMinor != nil || minorVer < 26 {
		return false
	}

	return true
}

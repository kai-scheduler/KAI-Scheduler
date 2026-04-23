// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package featuregates

import (
	"strconv"
	"strings"
	"sync/atomic"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	featureutil "k8s.io/apiserver/pkg/util/feature"
	discovery "k8s.io/client-go/discovery"
	"k8s.io/kubernetes/pkg/features"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	minimalSupportedVersion = "v1beta1"
)

// dynamicResourcesEnabled is the process-wide decision on whether DRA is usable,
// set by SetDRAFeatureGate. It is the authoritative source for scheduler
// components because the upstream DynamicResourceAllocation feature gate is GA
// and locked to true in Kubernetes v1.35+, so it can no longer be toggled off
// to reflect server-side DRA availability.
var dynamicResourcesEnabled atomic.Bool

func SetDRAFeatureGate(discoveryClient discovery.DiscoveryInterface) error {
	enabled := IsDynamicResourcesEnabled(discoveryClient)
	dynamicResourcesEnabled.Store(enabled)
	err := featureutil.DefaultMutableFeatureGate.SetFromMap(
		map[string]bool{string(features.DynamicResourceAllocation): enabled})
	// DynamicResourceAllocation is GA and locked to true in k8s v1.35+, so
	// attempting to set it to false errors. That is expected — the process-wide
	// flag above is the source of truth. Ignore the error in this case.
	if err != nil && !enabled {
		return nil
	}
	return err
}

// DynamicResourcesEnabled reports whether DRA was determined to be usable
// against the cluster at startup. Use this instead of the upstream feature gate
// to gate DRA-specific scheduler behaviour.
func DynamicResourcesEnabled() bool {
	return dynamicResourcesEnabled.Load()
}

// SetDynamicResourcesEnabledForTest sets the process-wide DRA availability flag.
// Intended for tests that construct scheduler components without going through
// SetDRAFeatureGate (which requires a discovery client).
func SetDynamicResourcesEnabledForTest(enabled bool) {
	dynamicResourcesEnabled.Store(enabled)
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

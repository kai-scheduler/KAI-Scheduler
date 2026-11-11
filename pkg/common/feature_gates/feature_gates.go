// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package featuregates

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	discovery "k8s.io/client-go/discovery"
)

const (
	minimalSupportedVersion = "v1beta1"

	nodeResourceTopologyGroup = "topology.node.k8s.io"
)

// dynamicResourcesEnabled is the process-wide decision on whether DRA is usable,
// set by SetDRAFeatureGate. It is the authoritative source for scheduler and
// binder components because the upstream DynamicResourceAllocation feature gate
// is GA and locked to true in Kubernetes v1.35+, so it can no longer be toggled
// off to reflect server-side DRA availability.
var dynamicResourcesEnabled atomic.Bool

// serverMinorVersion is the minor version of the Kubernetes API server, as
// seen by SetDRAFeatureGate. Zero means unknown.
var serverMinorVersion atomic.Int32

func SetDRAFeatureGate(discoveryClient discovery.DiscoveryInterface) error {
	serverVersion, err := discoveryClient.ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to get server version: %w", err)
	}
	enabled, err := isDynamicResourcesEnabled(serverVersion, discoveryClient)
	if err != nil {
		return err
	}
	dynamicResourcesEnabled.Store(enabled)
	serverMinorVersion.Store(int32(kubeMinorVersion(serverVersion)))
	return nil
}

// ServerVersionAtLeast reports whether the API server seen by
// SetDRAFeatureGate is Kubernetes 1.<minor> or later. It returns false when the
// server version is unknown.
func ServerVersionAtLeast(minor int) bool {
	return int(serverMinorVersion.Load()) >= minor
}

// SetServerMinorVersionForTest sets the API server minor version that
// ServerVersionAtLeast compares against. Zero means unknown.
func SetServerMinorVersionForTest(minor int) {
	serverMinorVersion.Store(int32(minor))
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

var nodeResourceTopologyEnabled atomic.Bool

func SetNodeResourceTopologyFeatureGate(discoveryClient discovery.DiscoveryInterface) error {
	enabled, err := IsNodeResourceTopologyEnabled(discoveryClient)
	if err != nil {
		return err
	}
	nodeResourceTopologyEnabled.Store(enabled)
	return nil
}

func NodeResourceTopologyEnabled() bool {
	return nodeResourceTopologyEnabled.Load()
}

func SetNodeResourceTopologyEnabledForTest(enabled bool) {
	nodeResourceTopologyEnabled.Store(enabled)
}

// IsNodeResourceTopologyEnabled reports whether the cluster serves the
// topology.node.k8s.io API group (the NodeResourceTopology CRD).
func IsNodeResourceTopologyEnabled(discoveryClient discovery.DiscoveryInterface) (bool, error) {
	serverGroups, err := discoveryClient.ServerGroups()
	if err != nil {
		return false, fmt.Errorf("failed to get server groups: %w", err)
	}

	for _, group := range serverGroups.Groups {
		if group.Name == nodeResourceTopologyGroup {
			return true, nil
		}
	}
	return false, nil
}

func IsDynamicResourcesEnabled(discoveryClient discovery.DiscoveryInterface) (bool, error) {
	// Get API server version
	serverVersion, err := discoveryClient.ServerVersion()
	if err != nil {
		return false, fmt.Errorf("failed to get server version: %w", err)
	}
	return isDynamicResourcesEnabled(serverVersion, discoveryClient)
}

func isDynamicResourcesEnabled(serverVersion *version.Info, discoveryClient discovery.DiscoveryInterface) (bool, error) {
	// Check if the API server version is compatible with DRA
	if !isCompatibleDRAVersion(serverVersion) {
		return false, nil
	}

	// Get supported API versions
	serverGroups, err := discoveryClient.ServerGroups()
	if err != nil {
		return false, fmt.Errorf("failed to get server groups: %w", err)
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
		return false, nil
	}

	// Check if the DRA API group is supported
	for _, groupVersion := range resourceGroup.Versions {
		if version.CompareKubeAwareVersionStrings(groupVersion.Version, minimalSupportedVersion) >= 0 {
			return true, nil
		}
	}

	return false, nil
}

func isCompatibleDRAVersion(serverVersion *version.Info) bool {
	return kubeMinorVersion(serverVersion) >= 26
}

// kubeMinorVersion returns the minor version of a Kubernetes 1.x server, or 0
// when the version cannot be parsed. Provider suffixes such as "33+" are
// ignored.
func kubeMinorVersion(serverVersion *version.Info) int {
	if serverVersion == nil {
		return 0
	}
	if majorVer, errMajor := strconv.Atoi(serverVersion.Major); errMajor != nil || majorVer < 1 {
		return 0
	}

	normalizedMinorVersion := serverVersion.Minor
	minorVersionSuffix := strings.TrimLeft(normalizedMinorVersion, "0123456789")
	if len(minorVersionSuffix) > 0 {
		normalizedMinorVersion = strings.TrimSuffix(normalizedMinorVersion, minorVersionSuffix)
	}
	minorVer, errMinor := strconv.Atoi(normalizedMinorVersion)
	if errMinor != nil || minorVer < 0 {
		return 0
	}
	return minorVer
}

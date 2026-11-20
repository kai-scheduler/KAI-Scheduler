// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
	kubefeatures "k8s.io/kubernetes/pkg/features"
	k8splfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
)

// draDefaultOnByServerVersion lists DRA feature gates that are off by default in
// the vendored Kubernetes libraries but on by default (Beta) from the listed
// Kubernetes minor version. On an API server at that version or later they are
// enabled unless set with --feature-gates, so the allocator matches what
// kube-apiserver and kube-scheduler support. A gate that flips in a later
// release is one more row.
var draDefaultOnByServerVersion = []struct {
	gate   featuregate.Feature
	since  int
	enable func(*k8splfeature.Features)
}{
	{
		gate:   kubefeatures.DRAPartitionableDevices,
		since:  36,
		enable: func(f *k8splfeature.Features) { f.EnableDRAPartitionableDevices = true },
	},
	{
		gate:   kubefeatures.DRAConsumableCapacity,
		since:  36,
		enable: func(f *k8splfeature.Features) { f.EnableDRAConsumableCapacity = true },
	},
}

// GetK8sFeatures returns the upstream scheduler feature flags, with
// EnableDynamicResourceAllocation overridden by KAI's runtime DRA detection.
// The upstream gate is GA-locked-to-true in Kubernetes v1.35+ and cannot be
// turned off, so reading it directly would falsely report DRA as enabled on
// clusters where KAI determined it is unavailable.
//
// The upstream gate defaults come from the vendored Kubernetes libraries, not
// from the cluster. When the API server is newer than those libraries, DRA
// gates that became default-on in that server version are enabled as well,
// unless they were set explicitly.
func GetK8sFeatures() k8splfeature.Features {
	return getK8sFeatures(feature.DefaultMutableFeatureGate)
}

func getK8sFeatures(gates featuregate.MutableVersionedFeatureGate) k8splfeature.Features {
	f := k8splfeature.NewSchedulerFeaturesFromGates(gates)
	f.EnableDynamicResourceAllocation = featuregates.DynamicResourcesEnabled()
	if f.EnableDynamicResourceAllocation {
		for _, d := range draDefaultOnByServerVersion {
			if featuregates.ServerVersionAtLeast(d.since) && !gates.ExplicitlySet(d.gate) {
				d.enable(&f)
			}
		}
	}
	return f
}

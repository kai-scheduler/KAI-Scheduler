// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"k8s.io/apiserver/pkg/util/feature"
	k8splfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
)

// GetK8sFeatures returns the upstream scheduler feature flags, with
// EnableDynamicResourceAllocation overridden by KAI's runtime DRA detection.
// The upstream gate is GA-locked-to-true in Kubernetes v1.35+ and cannot be
// turned off, so reading it directly would falsely report DRA as enabled on
// clusters where KAI determined it is unavailable.
func GetK8sFeatures() k8splfeature.Features {
	f := k8splfeature.NewSchedulerFeaturesFromGates(feature.DefaultMutableFeatureGate)
	f.EnableDynamicResourceAllocation = featuregates.DynamicResourcesEnabled()
	return f
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"k8s.io/apiserver/pkg/util/feature"
	k8splfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
)

func GetK8sFeatures() k8splfeature.Features {
	return k8splfeature.NewSchedulerFeaturesFromGates(feature.DefaultMutableFeatureGate)
}

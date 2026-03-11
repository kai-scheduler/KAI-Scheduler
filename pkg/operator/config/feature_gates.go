// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"

	featureutil "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/kubernetes/pkg/features"
)

func FeatureGatesArg() string {
	ff := []string{}
	if featureutil.DefaultMutableFeatureGate.Enabled(features.DynamicResourceAllocation) {
		ff = append(ff, "DynamicResourceAllocation=true")
	}

	if len(ff) == 0 {
		return ""
	}

	return "--feature-gates=" + strings.Join(ff, ",")
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func GetNodePoolNameFromLabels(labels map[string]string, nodePoolLabelKey string) string {
	nodePoolName, found := labels[nodePoolLabelKey]
	if !found || nodePoolName == "" {
		nodePoolName = commonconstants.DefaultNodePoolName
	}

	return nodePoolName
}

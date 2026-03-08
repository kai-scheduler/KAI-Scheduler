// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
)

func IsSharedGPUAllocation(bindRequest *v1alpha2.BindRequest) bool {
	return bindRequest.Spec.ReceivedResourceType == ReceivedTypeFraction
}

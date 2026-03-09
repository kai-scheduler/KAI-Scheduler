// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_share

import (
	"math"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

type ResourceShare struct {
	Deserved                float64
	FairShare               float64
	MaxAllowed              float64
	OverQuotaWeight         float64
	Allocated               float64
	AllocatedNotPreemptible float64
	Request                 float64
	Usage                   float64
}

func EmptyResource() ResourceShare {
	return ResourceShare{}
}

func (rs *ResourceShare) Clone() *ResourceShare {
	return &ResourceShare{
		Deserved:                rs.Deserved,
		FairShare:               rs.FairShare,
		MaxAllowed:              rs.MaxAllowed,
		OverQuotaWeight:         rs.OverQuotaWeight,
		Allocated:               rs.Allocated,
		AllocatedNotPreemptible: rs.AllocatedNotPreemptible,
		Request:                 rs.Request,
		Usage:                   rs.Usage,
	}
}

func (rs *ResourceShare) GetRequestableShare() float64 {
	if rs.MaxAllowed == commonconstants.UnlimitedResourceQuantity {
		return rs.Request
	}
	return math.Min(rs.MaxAllowed, rs.Request)
}

func (rs *ResourceShare) GetRequestShare() float64 {
	return rs.Request
}

func (rs *ResourceShare) GetAllocatableShare() float64 {
	if rs.Deserved == commonconstants.UnlimitedResourceQuantity {
		return rs.MaxAllowed
	}

	allocatable := math.Max(rs.Deserved, rs.FairShare)
	if rs.MaxAllowed != commonconstants.UnlimitedResourceQuantity {
		allocatable = math.Min(rs.MaxAllowed, allocatable)
	}
	return allocatable
}

func (rs *ResourceShare) GetUsage() float64 {
	return rs.Usage
}

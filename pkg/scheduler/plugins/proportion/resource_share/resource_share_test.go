// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_share

import (
	"testing"

	"github.com/stretchr/testify/assert"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestEmptyResource(t *testing.T) {
	r := EmptyResource()
	assert.Zero(t, r.Request)
	assert.Zero(t, r.FairShare)
	assert.Zero(t, r.Allocated)
	assert.Zero(t, r.Deserved)
	assert.Zero(t, r.MaxAllowed)
	assert.Zero(t, r.AllocatedNotPreemptible)
	assert.Zero(t, r.OverQuotaWeight)
}

func TestResourceShareClone(t *testing.T) {
	r := createResourceShare()
	c := r.Clone()
	assert.Equal(t, r.Deserved, c.Deserved)
	assert.Equal(t, r.FairShare, c.FairShare)
	assert.Equal(t, r.Allocated, c.Allocated)
	assert.Equal(t, r.MaxAllowed, c.MaxAllowed)
	assert.Equal(t, r.AllocatedNotPreemptible, c.AllocatedNotPreemptible)
	assert.Equal(t, r.OverQuotaWeight, c.OverQuotaWeight)
}

func TestCloneAndModify(t *testing.T) {
	r := createResourceShare()
	c := r.Clone()
	r.FairShare = 33
	assert.Equal(t, float64(33), r.FairShare)
	assert.Equal(t, float64(22), c.FairShare)
}

func TestGetRequestedShareLimited(t *testing.T) {
	r := createResourceShare()
	requested := r.GetRequestableShare()
	assert.Equal(t, requested, r.MaxAllowed)
}

func TestGetRequestedShareUnlimited(t *testing.T) {
	r := createResourceShare()
	r.MaxAllowed = commonconstants.UnlimitedResourceQuantity
	requested := r.GetRequestableShare()
	assert.Equal(t, requested, r.Request)
}

func TestGetAllocatableShareLimited(t *testing.T) {
	r := createResourceShare()
	allocatable := r.GetAllocatableShare()
	assert.Equal(t, r.MaxAllowed, allocatable)
}

func TestGetAllocatableShareUnlimited(t *testing.T) {
	r := createResourceShare()
	r.MaxAllowed = commonconstants.UnlimitedResourceQuantity
	allocatable := r.GetAllocatableShare()
	assert.Equal(t, r.FairShare, allocatable)
}

func createResourceShare() ResourceShare {
	return ResourceShare{
		Deserved:                21,
		FairShare:               22,
		MaxAllowed:              10,
		OverQuotaWeight:         2,
		Allocated:               5,
		AllocatedNotPreemptible: 4,
		Request:                 17,
	}
}

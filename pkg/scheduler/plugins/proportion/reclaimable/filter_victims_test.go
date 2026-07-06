// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaimable

import (
	"testing"

	"github.com/stretchr/testify/assert"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

func TestCanBeDeservedQuotaReclaimCandidateOnlyChecksInvolvedResources(t *testing.T) {
	vectorMap := resource_info.NewResourceVectorMap()
	reclaimer := &ReclaimerInfo{
		RequiredResources: resource_info.NewResourceVectorWithValues(0, 0, 1, vectorMap),
		VectorMap:         vectorMap,
	}
	reclaimeeQueue := &rs.QueueAttributes{
		QueueResourceShare: rs.QueueResourceShare{
			CPU:    rs.ResourceShare{Allocated: 0, Deserved: 10},
			Memory: rs.ResourceShare{Allocated: 0, Deserved: 10},
			GPU:    rs.ResourceShare{Allocated: 2, Deserved: 2},
		},
	}

	assert.True(t, canBeDeservedQuotaReclaimCandidate(reclaimer, reclaimeeQueue))

	reclaimeeQueue.GPU.Allocated = 1
	assert.False(t, canBeDeservedQuotaReclaimCandidate(reclaimer, reclaimeeQueue))

	reclaimeeQueue.GPU.Allocated = 3
	assert.True(t, canBeDeservedQuotaReclaimCandidate(reclaimer, reclaimeeQueue))

	reclaimeeQueue.GPU.Deserved = commonconstants.UnlimitedResourceQuantity
	assert.True(t, canBeDeservedQuotaReclaimCandidate(reclaimer, reclaimeeQueue))
}

func TestCanBeDeservedQuotaReclaimCandidateDoesNotAllocate(t *testing.T) {
	vectorMap := resource_info.NewResourceVectorMap()
	reclaimer := &ReclaimerInfo{
		RequiredResources: resource_info.NewResourceVectorWithValues(0, 0, 1, vectorMap),
		VectorMap:         vectorMap,
	}
	reclaimeeQueue := &rs.QueueAttributes{
		QueueResourceShare: rs.QueueResourceShare{
			CPU:    rs.ResourceShare{Allocated: 0, Deserved: 10},
			Memory: rs.ResourceShare{Allocated: 0, Deserved: 10},
			GPU:    rs.ResourceShare{Allocated: 2, Deserved: 2},
		},
	}
	var result bool

	allocations := testing.AllocsPerRun(100, func() {
		result = canBeDeservedQuotaReclaimCandidate(reclaimer, reclaimeeQueue)
	})

	assert.True(t, result)
	assert.Zero(t, allocations)
}

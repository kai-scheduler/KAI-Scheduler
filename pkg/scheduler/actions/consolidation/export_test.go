// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation

import (
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// SetClockForTest pins the defragmentation clock and the last run time.
func (alloc *consolidationAction) SetClockForTest(now func() time.Time, last time.Time) {
	alloc.now = now
	alloc.lastDefragmentation = last
}

// DefragmentForTest runs one defragmentation pass.
func (alloc *consolidationAction) DefragmentForTest(
	ssn *framework.Session, budget *solvers.ActionSearchBudget,
) error {
	return alloc.defragment(ssn, budget)
}

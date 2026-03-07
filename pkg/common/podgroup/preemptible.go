// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
)

const nonPreemptiblePriorityThreshold = 100

// CalculatePreemptibility computes the preemptibility of a podgroup.
// When preemptibility is not explicitly specified, the determination is based on the podgroup's priority.
func CalculatePreemptibility(preemptibility v2alpha2.Preemptibility, priority int32) v2alpha2.Preemptibility {
	switch preemptibility {
	case v2alpha2.Preemptible:
		return v2alpha2.Preemptible
	case v2alpha2.NonPreemptible:
		return v2alpha2.NonPreemptible
	}

	if priority < nonPreemptiblePriorityThreshold {
		return v2alpha2.Preemptible
	}
	return v2alpha2.NonPreemptible
}

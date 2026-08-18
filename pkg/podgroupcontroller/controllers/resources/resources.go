// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	v1 "k8s.io/api/core/v1"
)

func SumResources(left, right v1.ResourceList) v1.ResourceList {
	total := left.DeepCopy()
	if total == nil {
		total = make(v1.ResourceList)
	}

	for resourceName, resourceQuantity := range right {
		sum, seenResource := total[resourceName]
		if seenResource {
			sum.Add(resourceQuantity)
			total[resourceName] = sum
		} else {
			total[resourceName] = resourceQuantity.DeepCopy()
		}
	}
	return total
}

// MaxResources returns the per-resource maximum of left and right, the way the scheduler folds a pod's init
// phase peak into its steady state. A resource missing from one side is taken from the other.
func MaxResources(left, right v1.ResourceList) v1.ResourceList {
	result := left.DeepCopy()
	if result == nil {
		result = make(v1.ResourceList)
	}

	for resourceName, resourceQuantity := range right {
		current, seenResource := result[resourceName]
		if !seenResource || resourceQuantity.Cmp(current) > 0 {
			result[resourceName] = resourceQuantity.DeepCopy()
		}
	}
	return result
}

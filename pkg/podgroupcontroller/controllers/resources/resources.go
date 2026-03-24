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

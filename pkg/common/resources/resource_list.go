// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"reflect"
	"sort"

	"golang.org/x/exp/maps"
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

func EqualResourceLists(got, want v1.ResourceList) error {
	gotKeys := maps.Keys(got)
	sortResourceNames(gotKeys)
	wantKeys := maps.Keys(want)
	sortResourceNames(wantKeys)

	if !reflect.DeepEqual(gotKeys, wantKeys) {
		return fmt.Errorf("found different sets of keys that expected. got = %v, want %v", got, want)
	}

	for resourceName, quantity := range got {
		if quantity.Cmp(want[resourceName]) != 0 {
			return fmt.Errorf("found a different value than expected for resource %s . got = %v, want %v",
				resourceName, got, want)
		}
	}

	return nil
}

func sortResourceNames(list []v1.ResourceName) {
	sort.Slice(list, func(i, j int) bool {
		return list[i] < list[j]
	})
}

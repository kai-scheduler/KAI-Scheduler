// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func FindTypeInObjects[T client.Object](objects []client.Object) *T {
	for _, obj := range objects {
		typedObj, ok := obj.(T)
		if ok {
			return &typedObj
		}
	}

	return nil
}

func FindTypesInObjects[T client.Object](objects []client.Object) []T {
	var result []T
	for _, obj := range objects {
		typedObj, ok := obj.(T)
		if ok {
			result = append(result, typedObj)
		}
	}

	return result
}

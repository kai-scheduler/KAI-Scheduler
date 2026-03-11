// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

func SetDefault[T any](target *T, value *T) *T {
	if target == nil {
		return value
	}
	return target
}

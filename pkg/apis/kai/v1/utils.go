// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1

func updateMap[K comparable, V any](m map[K]V, key K, modify func(*V)) {
	val := m[key]
	modify(&val)
	m[key] = val
}

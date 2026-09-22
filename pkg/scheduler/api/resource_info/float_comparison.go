// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import "math"

const floatComparisonTolerance = 1e-9

// LessOrEqualWithTolerance compares resource quantities while ignoring floating-point rounding noise.
func LessOrEqualWithTolerance(quantity, limit float64) bool {
	return quantity <= limit || math.Abs(quantity-limit) <= floatComparisonTolerance
}

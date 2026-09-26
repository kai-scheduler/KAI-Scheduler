// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

// NvFractions translates legacy GPU memory requests and validates fractional
// requests and binder-owned device annotations.
type NvFractions struct {
	binderServiceAccountUsername string
}

func New(binderServiceAccountUsername string) *NvFractions {
	return &NvFractions{binderServiceAccountUsername: binderServiceAccountUsername}
}

func (p *NvFractions) Name() string {
	return "nvfractions"
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

// GpuSharingMode selects a preset that sets defaults for GPU-sharing plugins
// across the admission and binder components. Explicit per-plugin values still
// take precedence over the defaults implied by the mode.
// +kubebuilder:validation:Enum=NonMemoryEnforced;HamiCore;NvFractions;Disabled
type GpuSharingMode string

const (
	// GpuSharingModeNonMemoryEnforced enables the standard GPU-sharing plugins without
	// memory limit enforcement (legacy fractions). This is the default.
	GpuSharingModeNonMemoryEnforced GpuSharingMode = "NonMemoryEnforced"

	// GpuSharingModeHamiCore enables the standard GPU-sharing plugins together
	// with the HAMI-core plugins that enforce the GPU memory limit.
	GpuSharingModeHamiCore GpuSharingMode = "HamiCore"

	// GpuSharingModeNvFractions enables the dedicated NvFractions plugins, which
	// do not use the shared GPU configmap mechanism.
	GpuSharingModeNvFractions GpuSharingMode = "NvFractions"

	// GpuSharingModeDisabled disables GPU sharing. Pods requesting a GPU fraction
	// are rejected by the admission webhook.
	GpuSharingModeDisabled GpuSharingMode = "Disabled"
)

// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	resourcev1 "k8s.io/api/resource/v1"
)

// redactDeviceClasses sanitizes cluster-scoped device preset profiles,
// blinding vendor configuration payloads, extended resource names, and hardware selectors.
func (r *Redactor) redactDeviceClasses(raw *snapshot.RawKubernetesObjects) {
	for i := range raw.DeviceClasses {
		dc := raw.DeviceClasses[i]
		if dc == nil {
			continue
		}

		// 1. Redact top-level cluster-scoped identifiers and ObjectMeta
		dc.Name = r.Obfuscate(dc.Name, "deviceclass")
		r.redactObjectMeta(&dc.ObjectMeta, "deviceclass")

		// 2. Redact the specification details
		r.redactDeviceClassSpec(&dc.Spec)

		// 3. Safely increment redaction statistics under a lock
		r.mu.Lock()
		r.stats.DeviceClassesRedacted++
		r.mu.Unlock()
	}
}

// redactDeviceClassSpec processes device selectors, vendor configurations, and resource names.
func (r *Redactor) redactDeviceClassSpec(spec *resourcev1.DeviceClassSpec) {
	if spec == nil {
		return
	}

	// 1. Sanitize Selectors (CEL expressions filtering specific hardware metrics)
	// These can leak proprietary device attributes, model versions, or vendor specifics.
	for i := range spec.Selectors {
		selector := &spec.Selectors[i]
		if selector.CEL != nil && selector.CEL.Expression != "" {
			// Mask or abstract the expression text to remove physical hardware specifics
			selector.CEL.Expression = r.Obfuscate(selector.CEL.Expression, "celexpression")
		}
	}

	// 2. Strip Opaque Vendor Configuration Parameters
	// These parameters are passed directly to the driver and often contain sensitive hardware configurations.
	for i := range spec.Config {
		configPreset := &spec.Config[i]

		if configPreset.Opaque != nil {
			// CRITICAL: Wipe raw configuration maps entirely to prevent nested telemetry leakage.
			configPreset.Opaque = nil
		}
	}

	// 3. Obfuscate the ExtendedResourceName to prevent leaking structural pod allocation targets.
	if spec.ExtendedResourceName != nil && *spec.ExtendedResourceName != "" {
		maskedResource := r.Obfuscate(*spec.ExtendedResourceName, "extendedresource")
		spec.ExtendedResourceName = &maskedResource
	}
}

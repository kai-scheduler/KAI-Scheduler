// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
)

// redactResourceSlices sanitizes inventory capacity records, blinding physical
// node names, driver identifiers, resource pool names, and device lists.
func (r *Redactor) redactResourceSlices(raw *snapshot.RawKubernetesObjects) {
	for i := range raw.ResourceSlices {
		slice := raw.ResourceSlices[i]
		if slice == nil {
			continue
		}

		// 1. Redact top-level identifiers and ObjectMeta
		slice.Name = r.Obfuscate(slice.Name, "resourceslice")
		if slice.Namespace != "" {
			slice.Namespace = r.Obfuscate(slice.Namespace, "namespace")
		}
		r.redactObjectMeta(&slice.ObjectMeta, "resourceslice")

		// 2. Redact the core spec telemetry
		r.redactResourceSliceSpec(&slice.Spec)

		// 3. Safely increment redaction statistics
		r.mu.Lock()
		r.stats.ResourceSlicesRedacted++
		r.mu.Unlock()
	}
}

// redactResourceSliceSpec processes driver, pool, node selections, devices, and shared counters.
func (r *Redactor) redactResourceSliceSpec(spec *resourcev1.ResourceSliceSpec) {
	if spec == nil {
		return
	}

	// 1. Obfuscate top-level architectural variables
	if spec.Driver != "" {
		spec.Driver = r.Obfuscate(spec.Driver, "driver")
	}
	if spec.Pool.Name != "" {
		spec.Pool.Name = r.Obfuscate(spec.Pool.Name, "pool")
	}
	if spec.Pool.Name != "" {
		spec.Pool.Name = r.Obfuscate(spec.Pool.Name, "resourceslice")
	}

	// 2. Redact NodeSelection fields (Exactly one of the options will be set)

	// Handle single NodeName assignments safely
	if spec.NodeName != nil && *spec.NodeName != "" {
		maskedNode := r.Obfuscate(*spec.NodeName, "node")
		spec.NodeName = &maskedNode
	}

	// Handle multi-node topology constraints via NodeSelector mapping
	if spec.NodeSelector != nil {
		for i := range spec.NodeSelector.NodeSelectorTerms {
			term := &spec.NodeSelector.NodeSelectorTerms[i]

			// Process match expressions targeting specific node labels
			for j := range term.MatchExpressions {
				expr := &term.MatchExpressions[j]
				if expr.Key == corev1.LabelHostname {
					for k := range expr.Values {
						expr.Values[k] = r.Obfuscate(expr.Values[k], "node")
					}
				} else {
					expr.Key = r.Obfuscate(expr.Key, "labelkey")
					for k := range expr.Values {
						expr.Values[k] = r.Obfuscate(expr.Values[k], "labelval")
					}
				}
			}

			// Process field selectors if the selector queries metadata details
			for j := range term.MatchFields {
				fieldExpr := &term.MatchFields[j]
				// e.g., metadata.name or spec.nodeName routing rules
				for k := range fieldExpr.Values {
					fieldExpr.Values[k] = r.Obfuscate(fieldExpr.Values[k], "node")
				}
			}
		}
	}

	// 3. Obfuscate hardware devices inventory list
	for i := range spec.Devices {
		device := &spec.Devices[i]
		if device.Name != "" {
			device.Name = r.Obfuscate(device.Name, "device")
		}

		// Handle optional per-device local node visibility matrices if enabled
		if device.Attributes != nil {
			// Clear or obscure opaque hardware capabilities map strings if they hold
			// unique hardware fingerprints, serial numbers, or MAC addresses.
			device.Attributes = nil
		}
	}

	// 4. Obfuscate capacity scaling shared counter profiles
	for i := range spec.SharedCounters {
		counterSet := &spec.SharedCounters[i]
		if counterSet.Name != "" {
			counterSet.Name = r.Obfuscate(counterSet.Name, "counterset")
		}
		// Note: The array of counters inside CounterSet represents pure numerical data
		// (e.g. available integer splits) which is essential to keep for the scheduling simulator.
	}
}

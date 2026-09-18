// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
)

// redactResourceClaims processes Dynamic Resource Allocation (DRA) claims.
// It keeps scheduling structures intact while blinding specific hardware IDs,
// tenant allocation pools, and consuming Pod linkages.
func (r *Redactor) redactResourceClaims(raw *snapshot.RawKubernetesObjects) {
	for _, rc := range raw.ResourceClaims {
		if rc == nil {
			continue
		}

		// 1. Redact top-level identifiers and ObjectMeta
		rc.Name = r.Obfuscate(rc.Name, "resourceclaim")
		if rc.Namespace != "" {
			rc.Namespace = r.Obfuscate(rc.Namespace, "namespace")
		}
		r.redactObjectMeta(&rc.ObjectMeta, "resourceclaim")

		// 2. Redact ResourceClaimSpec
		r.redactResourceClaimSpec(&rc.Spec)

		// 3. Redact ResourceClaimStatus
		r.redactResourceClaimStatus(&rc.Status)

		// 4. Safely increment redaction statistics
		r.mu.Lock()
		r.stats.ResourceClaimsRedacted++
		r.mu.Unlock()
	}
}

// redactResourceClaimSpec handles specific hardware request configurations
func (r *Redactor) redactResourceClaimSpec(spec *resourcev1.ResourceClaimSpec) {
	if spec == nil {
		return
	}

	for i := range spec.Devices.Requests {
		req := &spec.Devices.Requests[i]

		// Selectors live on req.Exactly, not on DeviceRequest directly.
		// The v1 API restructured DeviceRequest so that all per-device
		// fields are nested under Exactly (*ExactDeviceRequest).
		if req.Exactly != nil {
			for j := range req.Exactly.Selectors {
				sel := &req.Exactly.Selectors[j]
				if sel.CEL != nil && sel.CEL.Expression != "" {
					sel.CEL.Expression = r.Obfuscate(
						sel.CEL.Expression, "celexpression",
					)
				}
			}
		}

		// Also cover FirstAvailable subrequests, which carry their own
		// Selectors independently of the Exactly path.
		for k := range req.FirstAvailable {
			sub := &req.FirstAvailable[k]
			for j := range sub.Selectors {
				sel := &sub.Selectors[j]
				if sel.CEL != nil && sel.CEL.Expression != "" {
					sel.CEL.Expression = r.Obfuscate(
						sel.CEL.Expression, "celexpression",
					)
				}
			}
		}
	}

	for i := range spec.Devices.Config {
		conf := &spec.Devices.Config[i]
		// Requests are name references — must use "devicerequest" prefix
		// to stay consistent with how req.Name is obfuscated above.
		for j := range conf.Requests {
			conf.Requests[j] = r.Obfuscate(conf.Requests[j], "devicerequest")
		}
		// Opaque holds raw driver parameters (GPU UUIDs, PCI addresses, etc.)
		if conf.Opaque != nil {
			conf.Opaque = nil
		}
	}
}

// redactResourceClaimStatus blinds execution placement and consuming targets.
func (r *Redactor) redactResourceClaimStatus(status *resourcev1.ResourceClaimStatus) {
	if status == nil {
		return
	}

	// 1. Redact Allocations (Topology constraints and node constraints).
	// In resource/v1, AvailableOnNodes is gone. The node placement info
	// is now at status.Allocation.NodeSelector (*corev1.NodeSelector).
	if status.Allocation != nil {
		if status.Allocation.NodeSelector != nil {
			for i := range status.Allocation.NodeSelector.NodeSelectorTerms {
				term := &status.Allocation.NodeSelector.NodeSelectorTerms[i]
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
				for j := range term.MatchFields {
					field := &term.MatchFields[j]
					for k := range field.Values {
						field.Values[k] = r.Obfuscate(field.Values[k], "node")
					}
				}
			}
		}
	}

	// 2. Redact Consumers (ReservedFor).
	// Keeps simulators working by renaming Pod references using the canonical "pod" prefix.
	for i := range status.ReservedFor {
		consumer := &status.ReservedFor[i]
		if consumer.Resource == "pods" {
			consumer.Name = r.Obfuscate(consumer.Name, "pod")
		} else {
			consumer.Name = r.Obfuscate(consumer.Name, "consumer")
		}
		// Clear absolute tracking UIDs
		consumer.UID = ""
	}

	// 3. Redact Allocated Device Details.
	// Blinds underlying vendor configurations, slicing profiles, and serial IDs.
	for i := range status.Devices {
		devStatus := &status.Devices[i]

		if devStatus.Driver != "" {
			devStatus.Driver = r.Obfuscate(devStatus.Driver, "driver")
		}
		if devStatus.Pool != "" {
			devStatus.Pool = r.Obfuscate(devStatus.Pool, "pool")
		}
		if devStatus.Device != "" {
			devStatus.Device = r.Obfuscate(devStatus.Device, "device")
		}
		if devStatus.ShareID != nil && *devStatus.ShareID != "" {
			obfuscated := r.Obfuscate(*devStatus.ShareID, "shareid")
			devStatus.ShareID = &obfuscated
		}

		// CRITICAL: Clear opaque or raw driver configuration extensions entirely.
		// These fields commonly hold explicit GPU UUIDs, PCI addresses, or topology maps.
		// Setting it to nil prevents deep-nested data telemetry leaks.
		devStatus.Data = nil
	}
}

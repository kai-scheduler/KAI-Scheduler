// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	storagev1 "k8s.io/api/storage/v1"
)

// redactCSIDrivers redacts cluster-wide CSI driver definitions.
// It maps the driver name to the global "provisioner" identifier to maintain
// relational integrity with StorageClass objects and obscures authentication audiences.
func (r *Redactor) redactCSIDrivers(raw *snapshot.RawKubernetesObjects) {
	for _, csi := range raw.CSIDrivers {
		if csi == nil {
			continue
		}

		// 1. Redact top-level name using the "provisioner" prefix.
		// This ensures consistency across the snapshot where StorageClass.Provisioner matches CSIDriver.Name.
		csi.Name = r.Obfuscate(csi.Name, "provisioner")
		r.redactObjectMeta(&csi.ObjectMeta, "provisioner")

		// 2. Redact Spec details
		r.redactCSIDriverSpec(&csi.Spec)

		// 3. Safely increment redaction statistics
		r.mu.Lock()
		r.stats.CSIDriversRedacted++
		r.mu.Unlock()
	}
}

// redactCSIDriverSpec inspects token requests for security audience leaks.
func (r *Redactor) redactCSIDriverSpec(spec *storagev1.CSIDriverSpec) {
	if spec == nil {
		return
	}

	// TokenRequests contain target audience configurations for identity tokens.
	// These can leak internal IAM boundaries, corporate authentication domains, or security providers.
	for i := range spec.TokenRequests {
		if spec.TokenRequests[i].Audience != "" {
			spec.TokenRequests[i].Audience = r.Obfuscate(spec.TokenRequests[i].Audience, "audience")
		}
		// Note: ExpirationSeconds is purely numerical and safe to preserve for scheduling simulation.
	}
}

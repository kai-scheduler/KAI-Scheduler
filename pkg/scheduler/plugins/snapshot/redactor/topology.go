// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	corev1 "k8s.io/api/core/v1"
)

// redactTopologies processes the cluster's custom hierarchy maps.
// It blinds internal corporate failure domains and custom labels while preserving
// standard Kubernetes structural anchors to ensure validation rules don't break.
func (r *Redactor) redactTopologies(raw *snapshot.RawKubernetesObjects) {
	for i := range raw.Topologies {
		topology := raw.Topologies[i]
		if topology == nil {
			continue
		}

		// 1. Redact top-level resource identifiers and metadata
		topology.Name = r.Obfuscate(topology.Name, "topology")
		if topology.Namespace != "" {
			topology.Namespace = r.Obfuscate(topology.Namespace, "namespace")
		}
		r.redactObjectMeta(&topology.ObjectMeta, "topology")

		// 2. Redact Topology Specification levels
		r.redactTopologySpec(&topology.Spec)

		// 3. Safely increment redaction statistics under a lock
		r.mu.Lock()
		r.stats.TopologiesRedacted++
		r.mu.Unlock()
	}
}

// redactTopologySpec iterates through hierarchical architecture layers
func (r *Redactor) redactTopologySpec(spec *kaiv1alpha1.TopologySpec) {
	if spec == nil {
		return
	}

	for i := range spec.Levels {
		level := &spec.Levels[i]

		if level.NodeLabel != "" {
			switch level.NodeLabel {
			case corev1.LabelHostname:
				// DO NOT TOUCH: LabelHostname is already "kubernetes.io/hostname".
				// No second case needed — adding it separately causes DuplicateCase.
				continue

			case "topology.kubernetes.io/zone", "topology.kubernetes.io/region":
				// Keep upstream standard cloud labels untouched.
				continue

			default:
				// Obfuscate custom proprietary hardware topology domains
				level.NodeLabel = r.Obfuscate(level.NodeLabel, "topologylabel")
			}
		}
	}
}

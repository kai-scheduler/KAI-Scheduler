// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	corev1 "k8s.io/api/core/v1"
)

// redactCSIStorageCapacities redacts storage capacity names, namespaces,
// storage class names, and the node topology label selectors.
func (r *Redactor) redactCSIStorageCapacities(raw *snapshot.RawKubernetesObjects) {
	for _, csi := range raw.CSIStorageCapacities {
		if csi == nil {
			continue
		}

		// 1. Redact top-level identifiers and ObjectMeta
		csi.Name = r.Obfuscate(csi.Name, "csicapacity")
		if csi.Namespace != "" {
			csi.Namespace = r.Obfuscate(csi.Namespace, "namespace")
		}
		r.redactObjectMeta(&csi.ObjectMeta, "csicapacity")

		// 2. Redact StorageClassName
		// Must use the "storageclass" prefix to maintain bindings with PVs and PVCs.
		if csi.StorageClassName != "" {
			csi.StorageClassName = r.Obfuscate(csi.StorageClassName, "storageclass")
		}

		// 3. Redact NodeTopology (LabelSelector)
		// This defines which nodes can access the storage and frequently leaks topology.
		if csi.NodeTopology != nil {
			// Redact MatchLabels
			if csi.NodeTopology.MatchLabels != nil {
				newLabels := make(map[string]string, len(csi.NodeTopology.MatchLabels))
				for k, v := range csi.NodeTopology.MatchLabels {
					// If the label explicitly targets a node hostname, use the "node" prefix
					// to ensure the simulator can still match it to the redacted Node object.
					if k == corev1.LabelHostname {
						newLabels[k] = r.Obfuscate(v, "node")
					} else {
						newLabels[r.Obfuscate(k, "labelkey")] = r.Obfuscate(v, "labelval")
					}
				}
				csi.NodeTopology.MatchLabels = newLabels
			}

			// Redact MatchExpressions
			for i := range csi.NodeTopology.MatchExpressions {
				req := &csi.NodeTopology.MatchExpressions[i]

				if req.Key == corev1.LabelHostname {
					// Preserve the hostname key, but obfuscate the target values as nodes
					for j := range req.Values {
						req.Values[j] = r.Obfuscate(req.Values[j], "node")
					}
				} else {
					// Obfuscate standard label keys and values
					req.Key = r.Obfuscate(req.Key, "labelkey")
					for j := range req.Values {
						req.Values[j] = r.Obfuscate(req.Values[j], "labelval")
					}
				}
			}
		}

		// 4. Safely increment redaction statistics
		r.mu.Lock()
		r.stats.CSIStorageCapacitiesRedacted++
		r.mu.Unlock()
	}
}

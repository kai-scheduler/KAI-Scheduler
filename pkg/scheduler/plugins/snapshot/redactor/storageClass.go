// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	corev1 "k8s.io/api/core/v1"
)

func (r *Redactor) redactStorageClasses(raw *snapshot.RawKubernetesObjects) {
	var processed int
	for _, sc := range raw.StorageClasses {
		if sc == nil {
			continue
		}
		processed++

		sc.Name = r.Obfuscate(sc.Name, "storageclass")
		r.redactObjectMeta(&sc.ObjectMeta, "storageclass")

		if sc.Provisioner != "" {
			sc.Provisioner = r.Obfuscate(sc.Provisioner, "provisioner")
		}

		if sc.Parameters != nil {
			newParams := make(map[string]string, len(sc.Parameters))
			for k, v := range sc.Parameters {
				newParams[r.Obfuscate(k, "scparamkey")] = r.Obfuscate(v, "scparamval")
			}
			sc.Parameters = newParams
		}

		for i := range sc.MountOptions {
			sc.MountOptions[i] = r.Obfuscate(sc.MountOptions[i], "mountoption")
		}

		if sc.AllowedTopologies != nil {
			for i := range sc.AllowedTopologies {
				term := &sc.AllowedTopologies[i]
				for j := range term.MatchLabelExpressions {
					expr := &term.MatchLabelExpressions[j]

					switch expr.Key {
					case corev1.LabelHostname:
						for k := range expr.Values {
							expr.Values[k] = r.Obfuscate(expr.Values[k], "node")
						}
					case "topology.kubernetes.io/zone", "topology.kubernetes.io/region":
						// CRITICAL FIX: Keep upstream cloud label keys intact matching redactTopologySpec fallback behavior.
						// Obfuscate values uniquely to conceal exact geographic zones while leaving boundaries functional.
						for k := range expr.Values {
							expr.Values[k] = r.Obfuscate(expr.Values[k], "topologyval")
						}
					default:
						expr.Key = r.Obfuscate(expr.Key, "topologykey")
						for k := range expr.Values {
							expr.Values[k] = r.Obfuscate(expr.Values[k], "topologyval")
						}
					}
				}
			}
		}
	}

	if processed > 0 {
		r.mu.Lock()
		r.stats.StorageClassesRedacted += processed
		r.mu.Unlock()
	}
}

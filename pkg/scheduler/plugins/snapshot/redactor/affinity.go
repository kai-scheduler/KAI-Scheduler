// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	corev1 "k8s.io/api/core/v1"
)

// standardTopologyKeys lists well-known Kubernetes topology keys that the
// scheduler reads directly. Redacting these would break placement decisions,
// so they are explicitly preserved.
var standardTopologyKeys = map[string]bool{
	"kubernetes.io/hostname":           true,
	"topology.kubernetes.io/zone":      true,
	"topology.kubernetes.io/region":    true,
	"node.kubernetes.io/instance-type": true,
	"node.kubernetes.io/windows-build": true,
	"karpenter.sh/capacity-type":       true,
	"karpenter.sh/instance-family":     true,
	"topology.ebs.csi.aws.com/zone":    true,
	"workload-type":                    true,
	"disk-type":                        true,
}

// redactAffinity redacts all value fields inside node affinity, pod affinity,
// and pod anti-affinity rules while keeping operators, keys, and standard
// topology keys intact so that scheduling constraints remain valid.
func (r *Redactor) redactAffinity(affinity *corev1.Affinity) {
	if affinity == nil {
		return
	}

	if affinity.NodeAffinity != nil {
		na := affinity.NodeAffinity
		if na.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			for i := range na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				r.redactNodeSelectorTerm(
					&na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[i],
				)
			}
		}
		for i := range na.PreferredDuringSchedulingIgnoredDuringExecution {
			r.redactNodeSelectorTerm(
				&na.PreferredDuringSchedulingIgnoredDuringExecution[i].Preference,
			)
		}
	}

	if affinity.PodAffinity != nil {
		r.redactPodAffinityTerms(
			affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		)
		r.redactWeightedPodAffinityTerms(
			affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
		)
	}

	if affinity.PodAntiAffinity != nil {
		r.redactPodAffinityTerms(
			affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		)
		r.redactWeightedPodAffinityTerms(
			affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
		)
	}

	r.mu.Lock()
	r.stats.AffinityRedacted++
	r.mu.Unlock()
}

// redactNodeSelectorTerm redacts values in MatchExpressions and MatchFields
// while preserving keys and operators.
func (r *Redactor) redactNodeSelectorTerm(term *corev1.NodeSelectorTerm) {
	if term == nil {
		return
	}

	for i := range term.MatchExpressions {
		for j := range term.MatchExpressions[i].Values {
			term.MatchExpressions[i].Values[j] = r.Obfuscate(
				term.MatchExpressions[i].Values[j], "nodeaffval",
			)
		}
	}

	for i := range term.MatchFields {
		for j := range term.MatchFields[i].Values {
			term.MatchFields[i].Values[j] = r.Obfuscate(
				term.MatchFields[i].Values[j], "fieldselector",
			)
		}
	}
}

// redactPodAffinityTerms redacts label selector values inside pod affinity
// terms. Standard topology keys are preserved. Custom topology keys are
// redacted because they may encode internal zone or rack naming conventions.
func (r *Redactor) redactPodAffinityTerms(terms []corev1.PodAffinityTerm) {
	for i := range terms {
		if terms[i].LabelSelector != nil {
			if terms[i].LabelSelector.MatchLabels != nil {
				r.redactMapValues(terms[i].LabelSelector.MatchLabels, false)
			}
			for j := range terms[i].LabelSelector.MatchExpressions {
				for k := range terms[i].LabelSelector.MatchExpressions[j].Values {
					terms[i].LabelSelector.MatchExpressions[j].Values[k] = r.Obfuscate(
						terms[i].LabelSelector.MatchExpressions[j].Values[k], "podaffval",
					)
				}
			}
		}

		if terms[i].TopologyKey != "" && !standardTopologyKeys[terms[i].TopologyKey] {
			terms[i].TopologyKey = r.Obfuscate(terms[i].TopologyKey, "topokey")
		}
	}
}

// redactWeightedPodAffinityTerms redacts the inner PodAffinityTerm of each
// weighted term. The weight itself is not sensitive and is preserved.
func (r *Redactor) redactWeightedPodAffinityTerms(terms []corev1.WeightedPodAffinityTerm) {
	for i := range terms {
		inner := []corev1.PodAffinityTerm{terms[i].PodAffinityTerm}
		r.redactPodAffinityTerms(inner)
		terms[i].PodAffinityTerm = inner[0]
	}
}

// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	corev1 "k8s.io/api/core/v1"
)

// redactNode redacts all sensitive fields on a Node object including
// network addresses, hardware identifiers, and cloud provider details.
func (r *Redactor) redactNode(node *corev1.Node) {
	if node == nil {
		return
	}

	// The "node" prefix must match the prefix used in redactPodSpec for
	// NodeName so that cross-resource references stay consistent.
	node.Name = r.Obfuscate(node.Name, "node")
	r.redactObjectMeta(&node.ObjectMeta, "node")

	r.redactNodeSpec(&node.Spec)
	r.redactNodeStatus(&node.Status)
}

// redactNodeSpec redacts network topology ranges, cloud provider instances,
// and custom taint values.
func (r *Redactor) redactNodeSpec(spec *corev1.NodeSpec) {
	if spec == nil {
		return
	}

	// PodCIDR and PodCIDRs reveal internal network topology and IP ranges.
	if spec.PodCIDR != "" {
		spec.PodCIDR = r.Obfuscate(spec.PodCIDR, "podcidr")
	}
	for i := range spec.PodCIDRs {
		spec.PodCIDRs[i] = r.Obfuscate(spec.PodCIDRs[i], "podcidr")
	}

	// ProviderID reveals cloud provider details and instance IDs.
	if spec.ProviderID != "" {
		spec.ProviderID = r.Obfuscate(spec.ProviderID, "providerid")
	}

	// ExternalID is deprecated but can leak internal infrastructure identifiers.
	if spec.DoNotUseExternalID != "" {
		spec.DoNotUseExternalID = r.Obfuscate(spec.DoNotUseExternalID, "externalid")
	}

	// Taint values can reveal internal team names, hardware types, or workload groupings.
	// We preserve Keys and Effects as they are structural for the scheduler.
	for i := range spec.Taints {
		if spec.Taints[i].Value != "" {
			spec.Taints[i].Value = r.Obfuscate(spec.Taints[i].Value, "taintval")
		}
	}
}

// redactNodeStatus redacts hostnames, IPs, hardware UUIDs, cached images,
// and free-text kubelet messages.
func (r *Redactor) redactNodeStatus(status *corev1.NodeStatus) {
	if status == nil {
		return
	}

	// Addresses reveal internal network IPs and internal DNS hostnames.
	for i := range status.Addresses {
		status.Addresses[i].Address = r.Obfuscate(status.Addresses[i].Address, "address")
	}

	// Hardware identifiers reveal physical/virtual machine identities.
	if status.NodeInfo.MachineID != "" {
		status.NodeInfo.MachineID = r.Obfuscate(status.NodeInfo.MachineID, "machineid")
	}
	if status.NodeInfo.SystemUUID != "" {
		status.NodeInfo.SystemUUID = r.Obfuscate(status.NodeInfo.SystemUUID, "systemuuid")
	}
	if status.NodeInfo.BootID != "" {
		status.NodeInfo.BootID = r.Obfuscate(status.NodeInfo.BootID, "bootid")
	}

	// Node Images list leaks internal container registry paths and image names.
	for i := range status.Images {
		for j := range status.Images[i].Names {
			status.Images[i].Names[j] = r.Obfuscate(status.Images[i].Names[j], "image")
		}
	}

	// Conditions frequently contain free-text messages from the Kubelet
	// that leak internal mount paths, IPs, or storage secrets. We preserve
	// Type, Status, and Reason for the simulator, but drop the Message.
	for i := range status.Conditions {
		status.Conditions[i].Message = ""
	}
}

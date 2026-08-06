// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	corev1 "k8s.io/api/core/v1"
)

// redactPod redacts all sensitive fields on a Pod object including its
// metadata, spec, and runtime status. Called once per pod by RedactSnapshot.
func (r *Redactor) redactPod(pod *corev1.Pod) {
	if pod == nil {
		return
	}

	// Pod name and namespace are set both on ObjectMeta and as top-level
	// convenience fields — we redact at the spec level and ObjectMeta together.
	pod.Name = r.Obfuscate(pod.Name, "pod")
	pod.Namespace = r.Obfuscate(pod.Namespace, "namespace")

	r.redactObjectMeta(&pod.ObjectMeta, "pod")
	r.redactPodSpec(&pod.Spec)
	r.redactPodStatus(&pod.Status)
}

// redactPodSpec redacts every field in PodSpec that could reveal sensitive
// infrastructure details, team names, internal hostnames, or image registry
// paths, while preserving all fields the scheduler reads structurally
// (operators, effects, resource quantities, restart policies, etc.).
func (r *Redactor) redactPodSpec(spec *corev1.PodSpec) {
	if spec == nil {
		return
	}

	// ServiceAccountName reveals internal RBAC and team structure.
	spec.ServiceAccountName = r.Obfuscate(spec.ServiceAccountName, "serviceaccount")

	// DeprecatedServiceAccount is an old alias for ServiceAccountName.
	// It must be redacted with the same prefix so both fields stay consistent.
	spec.DeprecatedServiceAccount = r.Obfuscate(spec.DeprecatedServiceAccount, "serviceaccount")

	// NodeName is a cross-resource reference. The "node" prefix must match
	// the prefix used in redactNode so both sides resolve to the same value.
	spec.NodeName = r.Obfuscate(spec.NodeName, "node")

	// Hostname and Subdomain reveal internal DNS naming conventions.
	spec.Hostname = r.Obfuscate(spec.Hostname, "hostname")
	spec.Subdomain = r.Obfuscate(spec.Subdomain, "subdomain")

	// SchedulerName can reveal internal scheduler topology.
	spec.SchedulerName = r.Obfuscate(spec.SchedulerName, "scheduler")

	// PriorityClassName links to a PriorityClass object by name.
	// We redact the value but use the same "priorityclass" prefix as the
	// PriorityClass objects themselves so cross-references stay consistent.
	spec.PriorityClassName = r.Obfuscate(spec.PriorityClassName, "priorityclass")

	// ImagePullSecrets reveal internal registry secret names.
	for i := range spec.ImagePullSecrets {
		spec.ImagePullSecrets[i].Name = r.Obfuscate(
			spec.ImagePullSecrets[i].Name, "secret",
		)
		r.mu.Lock()
		r.stats.SecretsRedacted++
		r.mu.Unlock()
	}

	// HostAliases inject custom /etc/hosts entries and can reveal internal
	// IP addresses and hostnames.
	for i := range spec.HostAliases {
		spec.HostAliases[i].IP = r.Obfuscate(spec.HostAliases[i].IP, "hostalias-ip")
		for j := range spec.HostAliases[i].Hostnames {
			spec.HostAliases[i].Hostnames[j] = r.Obfuscate(
				spec.HostAliases[i].Hostnames[j], "hostalias-host",
			)
		}
	}

	// Regular containers.
	for i := range spec.Containers {
		r.redactContainer(&spec.Containers[i], "container")
	}

	// Init containers run before regular containers and often contain
	// setup scripts with secrets or internal endpoints.
	for i := range spec.InitContainers {
		r.redactContainer(&spec.InitContainers[i], "initcontainer")
	}

	// Ephemeral containers are injected for debugging and frequently
	// contain internal tooling images and sensitive commands.
	for i := range spec.EphemeralContainers {
		r.redactEphemeralContainer(&spec.EphemeralContainers[i])
	}

	// Volumes and their source-specific sensitive fields.
	r.redactVolumes(spec.Volumes)

	// Affinity rules — values are redacted, structure is preserved.
	if spec.Affinity != nil {
		r.redactAffinity(spec.Affinity)
	}

	// TopologySpreadConstraints can reference custom label keys and values
	// that reveal internal topology naming.
	r.redactTopologySpreadConstraints(spec.TopologySpreadConstraints)

	// NodeSelector values reveal infrastructure labels and team conventions.
	// Keys are standard Kubernetes selector keys and are preserved.
	for key, value := range spec.NodeSelector {
		if value == "" {
			continue
		}
		spec.NodeSelector[key] = r.Obfuscate(value, "nodeselectval")
		r.mu.Lock()
		r.stats.NodeSelectorsRedacted++
		r.mu.Unlock()
	}

	// Toleration values reveal taint details tied to specific hardware or
	// team workloads. Operators and effects control scheduling behavior
	// and must be preserved exactly.
	for i := range spec.Tolerations {
		if spec.Tolerations[i].Value == "" {
			continue
		}
		spec.Tolerations[i].Value = r.Obfuscate(spec.Tolerations[i].Value, "tolvalue")
		r.mu.Lock()
		r.stats.TolerationsRedacted++
		r.mu.Unlock()
	}

	// ResourceClaims (Dynamic Resource Allocation). The claim names must
	// match the obfuscated names of the actual ResourceClaim objects.
	// ResourceClaims (Dynamic Resource Allocation). The claim names must
	// match the obfuscated names of the actual ResourceClaim objects.
	for i := range spec.ResourceClaims {
		spec.ResourceClaims[i].Name = r.Obfuscate(spec.ResourceClaims[i].Name, "podclaimname")

		// As of K8s 1.31, these fields are directly on the struct, not nested under .Source
		if spec.ResourceClaims[i].ResourceClaimName != nil {
			obfuscated := r.Obfuscate(*spec.ResourceClaims[i].ResourceClaimName, "resourceclaim")
			spec.ResourceClaims[i].ResourceClaimName = &obfuscated
		}
		if spec.ResourceClaims[i].ResourceClaimTemplateName != nil {
			obfuscated := r.Obfuscate(*spec.ResourceClaims[i].ResourceClaimTemplateName, "resourceclaimtemplate")
			spec.ResourceClaims[i].ResourceClaimTemplateName = &obfuscated
		}
	}

	// Hostname Override bypasses standard Hostname and can leak internal DNS names.
	if spec.HostnameOverride != nil && *spec.HostnameOverride != "" {
		obfuscated := r.Obfuscate(*spec.HostnameOverride, "hostname")
		spec.HostnameOverride = &obfuscated
	}

	// Workload Reference links to batch/AI workloads which leak team and job names.
	// Note: Namespace is implicitly the Pod's namespace, so it isn't an explicit field.
	if spec.WorkloadRef != nil {
		spec.WorkloadRef.Name = r.Obfuscate(spec.WorkloadRef.Name, "workload")

		// PodGroup and PodGroupReplicaKey are DNS labels and should be obfuscated
		if spec.WorkloadRef.PodGroup != "" {
			spec.WorkloadRef.PodGroup = r.Obfuscate(spec.WorkloadRef.PodGroup, "podgroup")
		}
		if spec.WorkloadRef.PodGroupReplicaKey != "" {
			spec.WorkloadRef.PodGroupReplicaKey = r.Obfuscate(spec.WorkloadRef.PodGroupReplicaKey, "podgroupreplica")
		}
	}

	// Scheduling Gates frequently contain internal domain names.
	for i := range spec.SchedulingGates {
		spec.SchedulingGates[i].Name = r.Obfuscate(spec.SchedulingGates[i].Name, "schedulinggate")
	}
}

// redactPodStatus redacts all sensitive runtime state from a pod's observed
// status including IP addresses, container runtime IDs, image digests,
// and node references.
func (r *Redactor) redactPodStatus(status *corev1.PodStatus) {
	if status == nil {
		return
	}

	// HostIP and HostIPs reveal the node's network address.
	status.HostIP = r.Obfuscate(status.HostIP, "hostip")
	for i := range status.HostIPs {
		status.HostIPs[i].IP = r.Obfuscate(status.HostIPs[i].IP, "hostip")
	}

	// PodIP and PodIPs reveal the pod's in-cluster network address.
	status.PodIP = r.Obfuscate(status.PodIP, "podip")
	for i := range status.PodIPs {
		status.PodIPs[i].IP = r.Obfuscate(status.PodIPs[i].IP, "podip")
	}

	// NominatedNodeName is set when this pod preempts others.
	// It is a node name reference and must use the "node" prefix.
	status.NominatedNodeName = r.Obfuscate(status.NominatedNodeName, "node")

	// Container statuses contain runtime IDs and image digest paths.
	r.redactContainerStatuses(status.ContainerStatuses)
	r.redactContainerStatuses(status.InitContainerStatuses)
	r.redactContainerStatuses(status.EphemeralContainerStatuses)

	// Conditions often contain free-text messages from the Kubelet or scheduler
	// that leak node names, IPs, or internal volume paths. We preserve the
	// Type, Status, and Reason for the scheduler simulator, but drop the Message.
	for i := range status.Conditions {
		status.Conditions[i].Message = ""
	}
}

// redactContainerStatuses redacts ContainerID and ImageID from a slice of
// container status objects. These fields contain runtime-specific identifiers
// like docker://sha256:... that reveal infrastructure details.
func (r *Redactor) redactContainerStatuses(statuses []corev1.ContainerStatus) {
	for i := range statuses {
		statuses[i].ContainerID = r.Obfuscate(statuses[i].ContainerID, "containerid")
		statuses[i].ImageID = r.Obfuscate(statuses[i].ImageID, "imageid")
		// Image name in status also reveals registry paths.
		statuses[i].Image = r.Obfuscate(statuses[i].Image, "image")
	}
}

// redactEphemeralContainer redacts an ephemeral container's image, name,
// commands, args, and environment variables. Ephemeral containers share
// the same sensitive field set as regular containers but have a different
// Go type (EphemeralContainer wraps EphemeralContainerCommon).
func (r *Redactor) redactEphemeralContainer(ec *corev1.EphemeralContainer) {
	if ec == nil {
		return
	}

	ec.Image = r.Obfuscate(ec.Image, "image")
	ec.Name = r.Obfuscate(ec.Name, "ephemeralcontainer")

	for i := range ec.Command {
		ec.Command[i] = r.Obfuscate(ec.Command[i], "cmdarg")
	}
	for i := range ec.Args {
		ec.Args[i] = r.Obfuscate(ec.Args[i], "cmdarg")
	}

	// Env vars inside ephemeral containers are just as sensitive as in
	// regular containers — they often contain debugging credentials.
	for i := range ec.Env {
		if ec.Env[i].Value != "" {
			ec.Env[i].Value = r.Obfuscate(ec.Env[i].Value, "envval")
			r.mu.Lock()
			r.stats.EnvVarsRedacted++
			r.mu.Unlock()
		}
		if ec.Env[i].ValueFrom == nil {
			continue
		}
		if ref := ec.Env[i].ValueFrom.SecretKeyRef; ref != nil {
			ref.Name = r.Obfuscate(ref.Name, "secret")
			ref.Key = r.Obfuscate(ref.Key, "secretkey")
			r.mu.Lock()
			r.stats.SecretsRedacted++
			r.mu.Unlock()
		}
		if ref := ec.Env[i].ValueFrom.ConfigMapKeyRef; ref != nil {
			ref.Name = r.Obfuscate(ref.Name, "configmap")
			ref.Key = r.Obfuscate(ref.Key, "configkey")
			r.mu.Lock()
			r.stats.ConfigMapsRedacted++
			r.mu.Unlock()
		}
	}

	for i := range ec.EnvFrom {
		if ec.EnvFrom[i].SecretRef != nil {
			ec.EnvFrom[i].SecretRef.Name = r.Obfuscate(ec.EnvFrom[i].SecretRef.Name, "secret")
			r.mu.Lock()
			r.stats.SecretsRedacted++
			r.mu.Unlock()
		}
		if ec.EnvFrom[i].ConfigMapRef != nil {
			ec.EnvFrom[i].ConfigMapRef.Name = r.Obfuscate(ec.EnvFrom[i].ConfigMapRef.Name, "configmap")
			r.mu.Lock()
			r.stats.ConfigMapsRedacted++
			r.mu.Unlock()
		}
	}
}

// redactTopologySpreadConstraints redacts label selector values inside
// topology spread constraints. The TopologyKey is checked against the
// standard keys list just like pod affinity terms — standard keys are
// preserved, custom ones are redacted.
func (r *Redactor) redactTopologySpreadConstraints(constraints []corev1.TopologySpreadConstraint) {
	for i := range constraints {
		// TopologyKey uses the same logic as pod affinity terms.
		if constraints[i].TopologyKey != "" && !standardTopologyKeys[constraints[i].TopologyKey] {
			constraints[i].TopologyKey = r.Obfuscate(constraints[i].TopologyKey, "topokey")
		}

		// LabelSelector values within the constraint can reveal team or
		// workload names used to identify which pods this constraint applies to.
		if constraints[i].LabelSelector != nil {
			if constraints[i].LabelSelector.MatchLabels != nil {
				r.redactMapValues(constraints[i].LabelSelector.MatchLabels, false)
			}
			for j := range constraints[i].LabelSelector.MatchExpressions {
				for k := range constraints[i].LabelSelector.MatchExpressions[j].Values {
					constraints[i].LabelSelector.MatchExpressions[j].Values[k] = r.Obfuscate(
						constraints[i].LabelSelector.MatchExpressions[j].Values[k],
						"podaffval",
					)
				}
			}
		}
	}
}

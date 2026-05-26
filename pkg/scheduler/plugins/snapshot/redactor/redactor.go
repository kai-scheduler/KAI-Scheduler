// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"fmt"
	"maps"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	corev1 "k8s.io/api/core/v1"
)

// RedactionStats tracks what was redacted
type RedactionStats struct {
	PodsRedacted        int
	NodesRedacted       int
	LabelsRedacted      int
	AnnotationsRedacted int
	EnvVarsRedacted     int
	SecretsRedacted     int
	ConfigMapsRedacted  int
	VolumesRedacted     int
	Affinity            int
}

// Redactor handles the obfuscation of sensitive Kubernetes metadata.
type Redactor struct {
	translationTable map[string]string
	counters         map[string]int
	stats            RedactionStats
}

// NewRedactor initializes a new Redactor instance.
func NewRedactor() *Redactor {
	return &Redactor{
		translationTable: make(map[string]string),
		counters:         make(map[string]int),
		stats:            RedactionStats{},
	}
}

// Obfuscate checks if a string is already translated. If not, it creates a new one.
func (r *Redactor) Obfuscate(original, prefix string) string {
	if original == "" {
		return ""
	}
	if obfuscated, exists := r.translationTable[original]; exists {
		return obfuscated
	}
	r.counters[prefix]++
	obfuscated := fmt.Sprintf("%s-%d", prefix, r.counters[prefix])
	r.translationTable[original] = obfuscated
	return obfuscated
}

// RedactSnapshot modifies the snapshot in-place based on the RawKubernetesObjects struct.
func (r *Redactor) RedactSnapshot(snap *snapshot.Snapshot) error {
	if snap == nil || snap.RawObjects == nil {
		return nil
	}
	raw := snap.RawObjects

	for _, pod := range raw.Pods {
		if pod != nil {
			r.redactPod(pod)
			r.stats.PodsRedacted++
		}
	}

	for _, node := range raw.Nodes {
		if node != nil {
			r.redactNode(node)
			r.stats.NodesRedacted++
		}
	}

	for _, q := range raw.Queues {
		if q != nil {
			q.Name = r.Obfuscate(q.Name, "queue")
			q.Namespace = r.Obfuscate(q.Namespace, "namespace")
			if q.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(q.ObjectMeta.Labels, false)
			}
			if q.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(q.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, pg := range raw.PodGroups {
		if pg != nil {
			pg.Name = r.Obfuscate(pg.Name, "podgroup")
			pg.Namespace = r.Obfuscate(pg.Namespace, "namespace")
			if pg.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(pg.ObjectMeta.Labels, false)
			}
			if pg.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(pg.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, br := range raw.BindRequests {
		if br != nil {
			br.Name = r.Obfuscate(br.Name, "bindreq")
			br.Namespace = r.Obfuscate(br.Namespace, "namespace")
			if br.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(br.ObjectMeta.Labels, false)
			}
			if br.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(br.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, pc := range raw.PriorityClasses {
		if pc != nil {
			pc.Name = r.Obfuscate(pc.Name, "priorityclass")
			if pc.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(pc.ObjectMeta.Labels, false)
			}
			if pc.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(pc.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, cm := range raw.ConfigMaps {
		if cm != nil {
			r.redactConfigMap(cm)
		}
	}

	for _, pv := range raw.PersistentVolumes {
		if pv != nil {
			pv.Name = r.Obfuscate(pv.Name, "pv")
			if pv.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(pv.ObjectMeta.Labels, false)
			}
			if pv.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(pv.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, pvc := range raw.PersistentVolumeClaims {
		if pvc != nil {
			pvc.Name = r.Obfuscate(pvc.Name, "pvc")
			pvc.Namespace = r.Obfuscate(pvc.Namespace, "namespace")
			if pvc.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(pvc.ObjectMeta.Labels, false)
			}
			if pvc.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(pvc.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, csi := range raw.CSIStorageCapacities {
		if csi != nil {
			csi.Name = r.Obfuscate(csi.Name, "csicapacity")
			csi.Namespace = r.Obfuscate(csi.Namespace, "namespace")
			if csi.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(csi.ObjectMeta.Labels, false)
			}
			if csi.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(csi.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, sc := range raw.StorageClasses {
		if sc != nil {
			sc.Name = r.Obfuscate(sc.Name, "storageclass")
			if sc.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(sc.ObjectMeta.Labels, false)
			}
			if sc.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(sc.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, driver := range raw.CSIDrivers {
		if driver != nil {
			driver.Name = r.Obfuscate(driver.Name, "csidriver")
			if driver.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(driver.ObjectMeta.Labels, false)
			}
			if driver.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(driver.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, rc := range raw.ResourceClaims {
		if rc != nil {
			rc.Name = r.Obfuscate(rc.Name, "resourceclaim")
			rc.Namespace = r.Obfuscate(rc.Namespace, "namespace")
			if rc.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(rc.ObjectMeta.Labels, false)
			}
			if rc.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(rc.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, rs := range raw.ResourceSlices {
		if rs != nil {
			rs.Name = r.Obfuscate(rs.Name, "resourceslice")
			if rs.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(rs.ObjectMeta.Labels, false)
			}
			if rs.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(rs.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, dc := range raw.DeviceClasses {
		if dc != nil {
			dc.Name = r.Obfuscate(dc.Name, "deviceclass")
			if dc.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(dc.ObjectMeta.Labels, false)
			}
			if dc.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(dc.ObjectMeta.Annotations, true)
			}
		}
	}

	for _, top := range raw.Topologies {
		if top != nil {
			top.Name = r.Obfuscate(top.Name, "topology")
			top.Namespace = r.Obfuscate(top.Namespace, "namespace")
			if top.ObjectMeta.Labels != nil {
				r.redactLabelsAndAnnotations(top.ObjectMeta.Labels, false)
			}
			if top.ObjectMeta.Annotations != nil {
				r.redactLabelsAndAnnotations(top.ObjectMeta.Annotations, true)
			}
		}
	}

	return nil
}

// redactNode redacts sensitive information in node specs and status
func (r *Redactor) redactNode(node *corev1.Node) {
	if node == nil {
		return
	}
	node.Name = r.Obfuscate(node.Name, "node")

	// Redact labels (can contain sensitive infrastructure info)
	if node.ObjectMeta.Labels != nil {
		r.redactLabelsAndAnnotations(node.ObjectMeta.Labels, false)
	}

	// Redact annotations
	if node.ObjectMeta.Annotations != nil {
		r.redactLabelsAndAnnotations(node.ObjectMeta.Annotations, true)
	}

	// Redact node status (contains IPs, hostnames, etc.)
	if node.Status.Addresses != nil {
		for i := range node.Status.Addresses {
			node.Status.Addresses[i].Address = r.Obfuscate(node.Status.Addresses[i].Address, "address")
		}
	}

	// Redact node name from Status
	if node.Status.NodeInfo.MachineID != "" {
		node.Status.NodeInfo.MachineID = r.Obfuscate(node.Status.NodeInfo.MachineID, "machineid")
	}
	if node.Status.NodeInfo.SystemUUID != "" {
		node.Status.NodeInfo.SystemUUID = r.Obfuscate(node.Status.NodeInfo.SystemUUID, "systemuuid")
	}
}

// redactConfigMap redacts sensitive information in config maps
func (r *Redactor) redactConfigMap(cm *corev1.ConfigMap) {
	if cm == nil {
		return
	}
	cm.Name = r.Obfuscate(cm.Name, "configmap")
	cm.Namespace = r.Obfuscate(cm.Namespace, "namespace")

	// Redact labels and annotations
	if cm.ObjectMeta.Labels != nil {
		r.redactLabelsAndAnnotations(cm.ObjectMeta.Labels, false)
	}
	if cm.ObjectMeta.Annotations != nil {
		r.redactLabelsAndAnnotations(cm.ObjectMeta.Annotations, true)
	}

	// Redact data contents - they can contain sensitive info
	if cm.Data != nil {
		redactedData := make(map[string]string)
		for key, value := range cm.Data {
			redactedKey := r.Obfuscate(key, "configkey")
			redactedValue := r.Obfuscate(value, "configvalue")
			redactedData[redactedKey] = redactedValue
		}
		cm.Data = redactedData
		r.stats.ConfigMapsRedacted++
	}

	// Redact BinaryData if present
	if cm.BinaryData != nil {
		redactedBinaryData := make(map[string][]byte)
		for key := range cm.BinaryData {
			redactedKey := r.Obfuscate(key, "configbinkey")
			redactedBinaryData[redactedKey] = []byte("[REDACTED]")
		}
		cm.BinaryData = redactedBinaryData
	}
}

// redactLabelsAndAnnotations redacts sensitive values in labels/annotations maps
// isAnnotation parameter distinguishes between labels and annotations for proper stat tracking
func (r *Redactor) redactLabelsAndAnnotations(labelMap map[string]string, isAnnotation bool) {
	if labelMap == nil {
		return
	}
	for key, value := range labelMap {
		// Only redact values, not keys (keys are structural)
		if value != "" {
			redactedValue := r.Obfuscate(value, "labelval")
			labelMap[key] = redactedValue

			// Track stats accurately based on whether it's a label or annotation
			if isAnnotation {
				r.stats.AnnotationsRedacted++
			} else {
				r.stats.LabelsRedacted++
			}
		}
	}
}

func (r *Redactor) redactPod(pod *corev1.Pod) {
	if pod == nil {
		return
	}
	pod.Name = r.Obfuscate(pod.Name, "pod")
	pod.Namespace = r.Obfuscate(pod.Namespace, "namespace")
	pod.Spec.ServiceAccountName = r.Obfuscate(pod.Spec.ServiceAccountName, "serviceaccount")

	// Redact labels and annotations
	if pod.ObjectMeta.Labels != nil {
		r.redactLabelsAndAnnotations(pod.ObjectMeta.Labels, false)
	}
	if pod.ObjectMeta.Annotations != nil {
		r.redactLabelsAndAnnotations(pod.ObjectMeta.Annotations, true)
	}

	// Redact OwnerReferences to maintain relationships but hide original names
	if pod.ObjectMeta.OwnerReferences != nil {
		for i := range pod.ObjectMeta.OwnerReferences {
			pod.ObjectMeta.OwnerReferences[i].Name = r.Obfuscate(
				pod.ObjectMeta.OwnerReferences[i].Name,
				"owner",
			)
		}
	}

	// Redact pod spec details
	r.redactPodSpec(&pod.Spec)

	// Redact pod status
	if pod.Status.HostIP != "" {
		pod.Status.HostIP = r.Obfuscate(pod.Status.HostIP, "hostip")
	}
	if pod.Status.PodIP != "" {
		pod.Status.PodIP = r.Obfuscate(pod.Status.PodIP, "podip")
	}
	if pod.Status.PodIPs != nil {
		for i := range pod.Status.PodIPs {
			pod.Status.PodIPs[i].IP = r.Obfuscate(pod.Status.PodIPs[i].IP, "podip")
		}
	}

	// Redact container statuses
	if pod.Status.ContainerStatuses != nil {
		for i := range pod.Status.ContainerStatuses {
			if pod.Status.ContainerStatuses[i].ContainerID != "" {
				pod.Status.ContainerStatuses[i].ContainerID = r.Obfuscate(
					pod.Status.ContainerStatuses[i].ContainerID,
					"containerid",
				)
			}
			if pod.Status.ContainerStatuses[i].ImageID != "" {
				pod.Status.ContainerStatuses[i].ImageID = r.Obfuscate(
					pod.Status.ContainerStatuses[i].ImageID,
					"imageid",
				)
			}
		}
	}
}

// redactPodSpec redacts sensitive information in PodSpec
func (r *Redactor) redactPodSpec(spec *corev1.PodSpec) {
	if spec == nil {
		return
	}

	// Redact containers
	for i := range spec.Containers {
		r.redactContainer(&spec.Containers[i], "container")
	}

	// Redact init containers
	for i := range spec.InitContainers {
		r.redactContainer(&spec.InitContainers[i], "initcontainer")
	}

	// Redact volumes (including secrets and configmaps)
	r.redactVolumes(spec.Volumes)

	// Redact affinity (node/pod affinity can contain sensitive labels)
	if spec.Affinity != nil {
		r.redactAffinity(spec.Affinity)
	}

	// Redact node selector (can contain sensitive labels)
	if spec.NodeSelector != nil {
		r.redactLabelsAndAnnotations(spec.NodeSelector, false)
	}

	// Redact tolerations (values can be sensitive)
	if spec.Tolerations != nil {
		for i := range spec.Tolerations {
			if spec.Tolerations[i].Value != "" {
				spec.Tolerations[i].Value = r.Obfuscate(spec.Tolerations[i].Value, "tolvalue")
			}
		}
	}
}

// redactContainer redacts sensitive information in container specs
func (r *Redactor) redactContainer(container *corev1.Container, containerPrefix string) {
	if container == nil {
		return
	}

	container.Image = r.Obfuscate(container.Image, "image")
	container.Name = r.Obfuscate(container.Name, containerPrefix)

	// Redact command and args (can contain sensitive info like API keys)
	if len(container.Command) > 0 {
		for i := range container.Command {
			if container.Command[i] != "" {
				container.Command[i] = r.Obfuscate(container.Command[i], "cmdarg")
			}
		}
	}
	if len(container.Args) > 0 {
		for i := range container.Args {
			if container.Args[i] != "" {
				container.Args[i] = r.Obfuscate(container.Args[i], "cmdarg")
			}
		}
	}

	// Redact environment variables - track only actual redactions
	if len(container.Env) > 0 {
		for i := range container.Env {
			if container.Env[i].Value != "" {
				container.Env[i].Value = r.Obfuscate(container.Env[i].Value, "envval")
				r.stats.EnvVarsRedacted++
			}
			// Redact secret references
			if container.Env[i].ValueFrom != nil && container.Env[i].ValueFrom.SecretKeyRef != nil {
				container.Env[i].ValueFrom.SecretKeyRef.Name = r.Obfuscate(
					container.Env[i].ValueFrom.SecretKeyRef.Name,
					"secret",
				)
				container.Env[i].ValueFrom.SecretKeyRef.Key = r.Obfuscate(
					container.Env[i].ValueFrom.SecretKeyRef.Key,
					"secretkey",
				)
			}
			// Redact configmap references
			if container.Env[i].ValueFrom != nil && container.Env[i].ValueFrom.ConfigMapKeyRef != nil {
				container.Env[i].ValueFrom.ConfigMapKeyRef.Name = r.Obfuscate(
					container.Env[i].ValueFrom.ConfigMapKeyRef.Name,
					"configmap",
				)
				container.Env[i].ValueFrom.ConfigMapKeyRef.Key = r.Obfuscate(
					container.Env[i].ValueFrom.ConfigMapKeyRef.Key,
					"configkey",
				)
			}
		}
	}

	// Redact envFrom (Secret/ConfigMap refs)
	if len(container.EnvFrom) > 0 {
		for i := range container.EnvFrom {
			if container.EnvFrom[i].SecretRef != nil {
				container.EnvFrom[i].SecretRef.Name = r.Obfuscate(
					container.EnvFrom[i].SecretRef.Name,
					"secret",
				)
			}
			if container.EnvFrom[i].ConfigMapRef != nil {
				container.EnvFrom[i].ConfigMapRef.Name = r.Obfuscate(
					container.EnvFrom[i].ConfigMapRef.Name,
					"configmap",
				)
			}
		}
	}
}

// redactVolumes redacts volume references
func (r *Redactor) redactVolumes(volumes []corev1.Volume) {
	for i := range volumes {
		if volumes[i].Secret != nil {
			volumes[i].Secret.SecretName = r.Obfuscate(volumes[i].Secret.SecretName, "secret")
			r.stats.SecretsRedacted++
		}
		if volumes[i].ConfigMap != nil {
			volumes[i].ConfigMap.Name = r.Obfuscate(volumes[i].ConfigMap.Name, "configmap")
		}
		if volumes[i].PersistentVolumeClaim != nil {
			volumes[i].PersistentVolumeClaim.ClaimName = r.Obfuscate(
				volumes[i].PersistentVolumeClaim.ClaimName,
				"pvc",
			)
			r.stats.VolumesRedacted++
		}
	}
}

// redactAffinity redacts sensitive information in affinity rules
func (r *Redactor) redactAffinity(affinity *corev1.Affinity) {
	if affinity == nil {
		return
	}

	// Redact node affinity
	if affinity.NodeAffinity != nil {
		if affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			for i := range affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				r.redactNodeSelectorTerm(
					&affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[i],
				)
			}
		}
		if affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution != nil {
			for i := range affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
				r.redactNodeSelectorTerm(
					&affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution[i].Preference,
				)
			}
		}
	}

	// Redact pod affinity
	if affinity.PodAffinity != nil {
		r.redactPodAffinityTerms(affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
		r.redactWeightedPodAffinityTerms(affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
	}

	// Redact pod anti-affinity
	if affinity.PodAntiAffinity != nil {
		r.redactPodAffinityTerms(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
		r.redactWeightedPodAffinityTerms(affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
	}

	r.stats.Affinity++
}

// redactNodeSelectorTerm redacts label selectors in node affinity
func (r *Redactor) redactNodeSelectorTerm(term *corev1.NodeSelectorTerm) {
	if term == nil {
		return
	}
	for i := range term.MatchExpressions {
		if len(term.MatchExpressions[i].Values) > 0 {
			for j := range term.MatchExpressions[i].Values {
				term.MatchExpressions[i].Values[j] = r.Obfuscate(
					term.MatchExpressions[i].Values[j],
					"nodeaffval",
				)
			}
		}
	}
	if term.MatchFields != nil {
		for i := range term.MatchFields {
			for j := range term.MatchFields[i].Values {
				term.MatchFields[i].Values[j] = r.Obfuscate(
					term.MatchFields[i].Values[j],
					"fieldselector",
				)
			}
		}
	}
}

// redactPodAffinityTerms redacts pod affinity terms
func (r *Redactor) redactPodAffinityTerms(terms []corev1.PodAffinityTerm) {
	for i := range terms {
		if terms[i].LabelSelector != nil && terms[i].LabelSelector.MatchLabels != nil {
			r.redactLabelsAndAnnotations(terms[i].LabelSelector.MatchLabels, false)
		}
		if terms[i].LabelSelector != nil && len(terms[i].LabelSelector.MatchExpressions) > 0 {
			for j := range terms[i].LabelSelector.MatchExpressions {
				for k := range terms[i].LabelSelector.MatchExpressions[j].Values {
					terms[i].LabelSelector.MatchExpressions[j].Values[k] = r.Obfuscate(
						terms[i].LabelSelector.MatchExpressions[j].Values[k],
						"podaffval",
					)
				}
			}
		}
		if terms[i].TopologyKey != "" {
			terms[i].TopologyKey = r.Obfuscate(terms[i].TopologyKey, "topokey")
		}
	}
}

// redactWeightedPodAffinityTerms redacts weighted pod affinity terms
func (r *Redactor) redactWeightedPodAffinityTerms(terms []corev1.WeightedPodAffinityTerm) {
	for i := range terms {
		podTerms := []corev1.PodAffinityTerm{terms[i].PodAffinityTerm}
		r.redactPodAffinityTerms(podTerms)
		terms[i].PodAffinityTerm = podTerms[0]
	}
}

// GetTranslationTable returns a defensive copy of the mapping to prevent external mutations
func (r *Redactor) GetTranslationTable() map[string]string {
	out := make(map[string]string, len(r.translationTable))
	maps.Copy(out, r.translationTable)
	return out
}

// GetStats returns redaction statistics
func (r *Redactor) GetStats() RedactionStats {
	return r.stats
}

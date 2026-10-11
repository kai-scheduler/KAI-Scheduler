// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
)

func (r *Redactor) redactQueues(raw *snapshot.RawKubernetesObjects) {
	var processed int
	for _, q := range raw.Queues {
		if q == nil {
			continue
		}
		processed++

		q.Name = r.Obfuscate(q.Name, "queue")
		if q.Namespace != "" {
			q.Namespace = r.Obfuscate(q.Namespace, "namespace")
		}
		r.redactObjectMeta(&q.ObjectMeta, "queue")

		if q.Spec.DisplayName != "" {
			q.Spec.DisplayName = r.Obfuscate(q.Spec.DisplayName, "queuedisplay")
		}
		if q.Spec.ParentQueue != "" {
			q.Spec.ParentQueue = r.Obfuscate(q.Spec.ParentQueue, "queue")
		}

		for i := range q.Status.ChildQueues {
			q.Status.ChildQueues[i] = r.Obfuscate(q.Status.ChildQueues[i], "queue")
		}
		for i := range q.Status.Conditions {
			q.Status.Conditions[i].Message = ""
		}
	}

	if processed > 0 {
		r.mu.Lock()
		r.stats.QueuesRedacted += processed
		r.mu.Unlock()
	}
}

func (r *Redactor) redactPodGroups(raw *snapshot.RawKubernetesObjects) {
	var processed int
	for _, pg := range raw.PodGroups {
		if pg == nil {
			continue
		}
		processed++
		pg.Name = r.Obfuscate(pg.Name, "podgroup")
		pg.Namespace = r.Obfuscate(pg.Namespace, "namespace")
		r.redactObjectMeta(&pg.ObjectMeta, "podgroup")
	}

	if processed > 0 {
		r.mu.Lock()
		r.stats.PodGroupsRedacted += processed
		r.mu.Unlock()
	}
}

func (r *Redactor) redactBindRequests(raw *snapshot.RawKubernetesObjects) {
	var processed int
	for _, br := range raw.BindRequests {
		if br == nil {
			continue
		}
		processed++

		br.Name = r.Obfuscate(br.Name, "bindreq")
		if br.Namespace != "" {
			br.Namespace = r.Obfuscate(br.Namespace, "namespace")
		}
		r.redactObjectMeta(&br.ObjectMeta, "bindreq")

		if br.Spec.PodName != "" {
			br.Spec.PodName = r.Obfuscate(br.Spec.PodName, "pod")
		}
		if br.Spec.SelectedNode != "" {
			br.Spec.SelectedNode = r.Obfuscate(br.Spec.SelectedNode, "node")
		}

		for i := range br.Spec.SelectedGPUGroups {
			br.Spec.SelectedGPUGroups[i] = r.Obfuscate(br.Spec.SelectedGPUGroups[i], "gpugroup")
		}

		for i := range br.Spec.ResourceClaimAllocations {
			if br.Spec.ResourceClaimAllocations[i].Name != "" {
				br.Spec.ResourceClaimAllocations[i].Name = r.Obfuscate(
					br.Spec.ResourceClaimAllocations[i].Name, "podclaimname",
				)
			}
		}

		br.Status.Reason = ""
	}

	if processed > 0 {
		r.mu.Lock()
		r.stats.BindRequestsRedacted += processed
		r.mu.Unlock()
	}
}

func (r *Redactor) redactPriorityClasses(raw *snapshot.RawKubernetesObjects) {
	var processed int
	for _, pc := range raw.PriorityClasses {
		if pc == nil {
			continue
		}
		processed++

		pc.Name = r.Obfuscate(pc.Name, "priorityclass")
		r.redactObjectMeta(&pc.ObjectMeta, "priorityclass")
		pc.Description = ""
	}

	if processed > 0 {
		r.mu.Lock()
		r.stats.PriorityClassesRedacted += processed
		r.mu.Unlock()
	}
}

func (r *Redactor) redactConfigMaps(raw *snapshot.RawKubernetesObjects) {
	var processed int
	for _, cm := range raw.ConfigMaps {
		if cm == nil {
			continue
		}
		processed++

		cm.Name = r.Obfuscate(cm.Name, "configmap")
		if cm.Namespace != "" {
			cm.Namespace = r.Obfuscate(cm.Namespace, "namespace")
		}
		r.redactObjectMeta(&cm.ObjectMeta, "configmap")

		if cm.Data != nil {
			newData := make(map[string]string, len(cm.Data))
			for k, v := range cm.Data {
				obfuscatedKey := r.Obfuscate(k, "configkey")
				obfuscatedValue := ""
				if v != "" {
					obfuscatedValue = r.Obfuscate(v, "configval")
				}
				newData[obfuscatedKey] = obfuscatedValue
			}
			cm.Data = newData
		}

		if cm.BinaryData != nil {
			newBinaryData := make(map[string][]byte, len(cm.BinaryData))
			for k, v := range cm.BinaryData {
				obfuscatedKey := r.Obfuscate(k, "configkey")
				var obfuscatedValue []byte
				if len(v) > 0 {
					obfuscatedValue = []byte(r.Obfuscate(string(v), "configbin"))
				}
				newBinaryData[obfuscatedKey] = obfuscatedValue
			}
			cm.BinaryData = newBinaryData
		}
	}

	if processed > 0 {
		r.mu.Lock()
		r.stats.ConfigMapsRedacted += processed
		r.mu.Unlock()
	}
}

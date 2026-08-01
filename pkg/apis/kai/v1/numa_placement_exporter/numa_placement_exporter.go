// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package numa_placement_exporter

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
)

const imageName = "numa-placement-exporter"

const defaultNodeSelectorKey = "feature.node.kubernetes.io/memory-numa"

// NumaPlacementExporter configures the per-node NUMA placement exporter DaemonSet. The exporter reads
// the kubelet podresources API and publishes each pod's observed NUMA placement; the numa scheduler
// plugin consumes it. Deployment is gated by the operator on the numa plugin being enabled in a shard.
type NumaPlacementExporter struct {
	// Service holds the image and resources. Service.Enabled is used as a tri-state: nil = auto
	// (deploy iff the numa plugin is enabled in some shard), true = always deploy, false = never.
	// +kubebuilder:validation:Optional
	Service *common.Service `json:"service,omitempty"`

	// NodeSelector restricts the DaemonSet to a node subset (typically the GPU/NUMA nodes the
	// exporter observes). The global nodeSelector is deliberately not applied — it is used to confine
	// management pods off worker nodes, the opposite of what the exporter needs.
	// +kubebuilder:validation:Optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations let the DaemonSet run on tainted worker nodes. Merged with the global daemonset
	// tolerations (global.daemonsetsTolerations).
	// +kubebuilder:validation:Optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`

	// PollInterval is how often the exporter reads placement from the local kubelet podresources API.
	// This poll is node-local and does not contact the API server; the API server is written only on
	// a pod's initial placement. Defaults to 1s when unset.
	// +kubebuilder:validation:Optional
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`

	// DriftResyncInterval is how often the exporter reconciles annotations against the API server,
	// repairing pods whose annotation drifted (removed or modified out-of-band). 0 disables it.
	// Defaults to 60s when unset.
	// +kubebuilder:validation:Optional
	DriftResyncInterval *metav1.Duration `json:"driftResyncInterval,omitempty"`

	// PodResourcesHostPath is the host directory hostPath-mounted for the podresources socket.
	// Override to point the exporter at a non-kubelet podresources socket (e.g. a simulated one).
	// PodResourcesSocket must resolve inside it. Defaults to /var/lib/kubelet/pod-resources.
	// +kubebuilder:validation:Optional
	PodResourcesHostPath string `json:"podResourcesHostPath,omitempty"`

	// PodResourcesSocket is the podresources gRPC socket path the exporter dials. It must resolve
	// inside PodResourcesHostPath (mounted at the same in-container path).
	// Defaults to /var/lib/kubelet/pod-resources/kubelet.sock.
	// +kubebuilder:validation:Optional
	PodResourcesSocket string `json:"podResourcesSocket,omitempty"`

	// SysfsHostPath is the host sysfs directory hostPath-mounted for CPU-to-NUMA resolution.
	// Override to point the exporter at a synthetic sysfs tree. Defaults to /sys.
	// +kubebuilder:validation:Optional
	SysfsHostPath string `json:"sysfsHostPath,omitempty"`
}

func (n *NumaPlacementExporter) SetDefaultsWhereNeeded() {
	n.Service = common.SetDefault(n.Service, &common.Service{})
	if len(n.NodeSelector) == 0 {
		n.NodeSelector = map[string]string{defaultNodeSelectorKey: "true"}
	}

	// Service.SetDefaultsWhereNeeded forces Enabled=true; preserve the tri-state (nil = auto).
	enabled := n.Service.Enabled
	n.Service.SetDefaultsWhereNeeded(imageName)
	n.Service.Enabled = enabled

	n.Service.Resources = common.SetDefault(n.Service.Resources, &common.Resources{})
	if n.Service.Resources.Requests == nil {
		n.Service.Resources.Requests = v1.ResourceList{}
	}
	if n.Service.Resources.Limits == nil {
		n.Service.Resources.Limits = v1.ResourceList{}
	}
	setResourceDefault(n.Service.Resources.Requests, v1.ResourceCPU, "50m")
	setResourceDefault(n.Service.Resources.Requests, v1.ResourceMemory, "64Mi")
	setResourceDefault(n.Service.Resources.Limits, v1.ResourceCPU, "200m")
	setResourceDefault(n.Service.Resources.Limits, v1.ResourceMemory, "128Mi")
}

func setResourceDefault(list v1.ResourceList, name v1.ResourceName, value string) {
	if _, found := list[name]; !found {
		list[name] = resource.MustParse(value)
	}
}

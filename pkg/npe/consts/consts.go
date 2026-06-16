// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consts

import "time"

const (
	// NumaPlacementAnnotation is the pod annotation the exporter writes, carrying the observed
	// per-NUMA-node placement of the pod's topology-aligned resources. The NUMA scheduler
	// plugin consumes this value (resource -> {NUMA node -> quantity}) instead of predicting.
	NumaPlacementAnnotation = "kai.scheduler/numa-placement-observed"

	// DefaultPodResourcesSocket is the kubelet podresources gRPC socket, mounted read-only
	// into the exporter via a hostPath.
	DefaultPodResourcesSocket = "/var/lib/kubelet/pod-resources/kubelet.sock"

	// DefaultSysfsRoot is the sysfs mount used to resolve CPU -> NUMA node mappings.
	DefaultSysfsRoot = "/sys"

	// DefaultPollInterval is how often the exporter reconciles observed placement onto pods.
	// Placement is stable for a pod's lifetime, so this only needs to be frequent enough to
	// keep the initial-observation lag small.
	DefaultPollInterval = 1 * time.Second

	// DefaultDriftResyncInterval is how often the exporter reconciles against the API server to
	// repair pods whose annotation drifted from the observed placement (removed or modified
	// out-of-band). Set to 0 to disable, relying solely on the in-memory write cache.
	DefaultDriftResyncInterval = 60 * time.Second
)

// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa_placement_exporter

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	npeapi "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/numa_placement_exporter"
	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/consts"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
)

const (
	defaultResourceName = "numa-placement-exporter"

	// podResourcesDir is the kubelet podresources directory, hostPath-mounted read-only at the same
	// in-container path so the default socket path resolves.
	podResourcesDir = "/var/lib/kubelet/pod-resources"
	// sysfsHostPath is the node's sysfs; sysfsMountPath is where it is mounted in the container
	// (not over the container's own /sys), and is passed to the binary via --sysfs-root.
	sysfsHostPath  = "/sys"
	sysfsMountPath = "/host/sys"
)

func (e *NumaPlacementExporter) daemonSetForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	config := kaiConfig.Spec.NumaPlacementExporter
	ds, err := common.DaemonSetForKAIConfig(
		ctx, runtimeClient, kaiConfig, config.Service, config.NodeSelector, config.Tolerations, e.BaseResourceName)
	if err != nil {
		return nil, err
	}

	container := &ds.Spec.Template.Spec.Containers[0]
	container.Args = buildArgsList(config)
	container.Env = []v1.EnvVar{{
		Name:      "NODE_NAME",
		ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "spec.nodeName"}},
	}}
	// The kubelet podresources socket and its parent directory are root-owned; read as root.
	container.SecurityContext = &v1.SecurityContext{
		RunAsUser:    ptr.To(int64(0)),
		RunAsNonRoot: ptr.To(false),
	}
	podResHostPath := effectivePodResourcesDir(config)
	sysHostPath := effectiveSysfsHostPath(config)

	container.VolumeMounts = []v1.VolumeMount{
		{Name: "podresources", MountPath: podResHostPath, ReadOnly: true},
		{Name: "sysfs", MountPath: sysfsMountPath, ReadOnly: true},
	}

	hostPathDir := v1.HostPathDirectory
	ds.Spec.Template.Spec.Volumes = []v1.Volume{
		{Name: "podresources", VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{Path: podResHostPath, Type: &hostPathDir}}},
		{Name: "sysfs", VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{Path: sysHostPath, Type: &hostPathDir}}},
	}

	return []client.Object{ds}, nil
}

// buildArgsList passes the fixed mount-aligned paths plus any tunable intervals the Config sets;
// unset intervals fall through to the binary defaults.
func buildArgsList(config *npeapi.NumaPlacementExporter) []string {
	args := []string{
		fmt.Sprintf("--podresources-socket=%s", effectivePodResourcesSocket(config)),
		fmt.Sprintf("--sysfs-root=%s", sysfsMountPath),
	}
	if config.PollInterval != nil {
		args = append(args, fmt.Sprintf("--poll-interval=%s", config.PollInterval.Duration.String()))
	}
	if config.DriftResyncInterval != nil {
		args = append(args, fmt.Sprintf("--drift-resync-interval=%s", config.DriftResyncInterval.Duration.String()))
	}
	return args
}

func (e *NumaPlacementExporter) serviceAccountForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	sa, err := common.ObjectForKAIConfig(ctx, runtimeClient, &v1.ServiceAccount{}, e.BaseResourceName,
		kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	sa.(*v1.ServiceAccount).TypeMeta = metav1.TypeMeta{
		Kind:       "ServiceAccount",
		APIVersion: "v1",
	}
	return []client.Object{sa}, nil
}

// effectivePodResourcesDir returns the configured podresources host directory, or the kubelet default.
func effectivePodResourcesDir(config *npeapi.NumaPlacementExporter) string {
	if config.PodResourcesHostPath != "" {
		return config.PodResourcesHostPath
	}
	return podResourcesDir
}

// effectivePodResourcesSocket returns the configured podresources socket, or the kubelet default.
func effectivePodResourcesSocket(config *npeapi.NumaPlacementExporter) string {
	if config.PodResourcesSocket != "" {
		return config.PodResourcesSocket
	}
	return consts.DefaultPodResourcesSocket
}

// effectiveSysfsHostPath returns the configured sysfs host path, or /sys.
func effectiveSysfsHostPath(config *npeapi.NumaPlacementExporter) string {
	if config.SysfsHostPath != "" {
		return config.SysfsHostPath
	}
	return sysfsHostPath
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
)

const (
	CdiDeviceNameBase = "k8s.device-plugin.nvidia.com/gpu=%s"
)

type GPUSharing struct {
	kubeClient             client.Client
	gpuDevicePluginUsesCdi bool
}

func New(kubeClient client.Client, gpuDevicePluginUsesCdi bool) *GPUSharing {
	return &GPUSharing{
		kubeClient:             kubeClient,
		gpuDevicePluginUsesCdi: gpuDevicePluginUsesCdi,
	}
}

func (p *GPUSharing) Name() string {
	return "gpusharing"
}

func (p *GPUSharing) PreBind(
	ctx context.Context, pod *v1.Pod, _ *v1.Node, bindRequest *v1alpha2.BindRequest, state *state.BindingState,
) error {
	if !common.IsSharedGPUAllocation(bindRequest) {
		return nil
	}

	reservedGPUIds := slices.Clone(state.ReservedGPUIds)
	if p.gpuDevicePluginUsesCdi {
		for index, gpuIndex := range reservedGPUIds {
			reservedGPUIds[index] = fmt.Sprintf(CdiDeviceNameBase, gpuIndex)
		}
	}

	containerRef, err := common.GetFractionContainerRef(pod)
	if err != nil {
		return fmt.Errorf("failed to get fraction container ref: %w", err)
	}

	err = p.createDirectEnvMapIfMissing(ctx, pod, containerRef)
	if err != nil {
		return fmt.Errorf("failed to create env configmap: %w", err)
	}

	nVisibleDevicesStr := strings.Join(reservedGPUIds, ",")

	// For pods where NVIDIA_VISIBLE_DEVICES is sourced from a ConfigMapKeyRef (the standard
	// path set up by the admission webhook), write all capabilities data in a single
	// UpsertJobConfigMap call. This avoids a race condition between the informer-cache-backed
	// client and the API server: if Create and a subsequent Get happen before the watch
	// event updates the cache, the Get returns NotFound and the bind fails.
	if nvidiaVisibleDevicesViaConfigMapRef(containerRef.Container) {
		capabilitiesConfigMapName, err := gpusharingconfigmap.ExtractCapabilitiesConfigMapName(pod, containerRef)
		if err != nil {
			return fmt.Errorf("failed to get capabilities configmap name: %w", err)
		}
		data := map[string]string{
			constants.NvidiaVisibleDevices: nVisibleDevicesStr,
			common.NumOfGpusEnvVarBC:       bindRequest.Spec.ReceivedGPU.Portion,
			common.GPUPortion:              bindRequest.Spec.ReceivedGPU.Portion,
		}
		return gpusharingconfigmap.UpsertJobConfigMap(ctx, p.kubeClient, pod, capabilitiesConfigMapName, data)
	}

	// Backward compat: NVIDIA_VISIBLE_DEVICES is served via envFrom from the direct env vars ConfigMap.
	capabilitiesConfigMapName, err := gpusharingconfigmap.ExtractCapabilitiesConfigMapName(pod, containerRef)
	if err != nil {
		return fmt.Errorf("failed to get capabilities configmap name: %w", err)
	}
	if err = gpusharingconfigmap.UpsertJobConfigMap(ctx, p.kubeClient, pod, capabilitiesConfigMapName, map[string]string{}); err != nil {
		return fmt.Errorf("failed to create capabilities configmap: %w", err)
	}
	if err = common.SetNvidiaVisibleDevices(ctx, p.kubeClient, pod, containerRef, nVisibleDevicesStr); err != nil {
		return err
	}
	return common.SetGPUPortion(ctx, p.kubeClient, pod, containerRef, bindRequest.Spec.ReceivedGPU.Portion)
}

// nvidiaVisibleDevicesViaConfigMapRef reports whether the container's NVIDIA_VISIBLE_DEVICES
// env var is sourced from a ConfigMapKeyRef (i.e. the pod was admitted by the current webhook).
func nvidiaVisibleDevicesViaConfigMapRef(container *v1.Container) bool {
	for _, envVar := range container.Env {
		if envVar.Name == constants.NvidiaVisibleDevices &&
			envVar.ValueFrom != nil && envVar.ValueFrom.ConfigMapKeyRef != nil {
			return true
		}
	}
	return false
}

func (p *GPUSharing) createDirectEnvMapIfMissing(ctx context.Context, pod *v1.Pod,
	containerRef *gpusharingconfigmap.PodContainerRef) error {
	directEnvVarsMapName, err := gpusharingconfigmap.ExtractDirectEnvVarsConfigMapName(pod, containerRef)
	if err != nil {
		return err
	}
	directEnvVars := make(map[string]string)
	return gpusharingconfigmap.UpsertJobConfigMap(ctx, p.kubeClient, pod, directEnvVarsMapName, directEnvVars)
}

func (p *GPUSharing) PostBind(
	context.Context, *v1.Pod, *v1.Node, *v1alpha2.BindRequest, *state.BindingState,
) {
}

func (p *GPUSharing) Rollback(
	ctx context.Context, pod *v1.Pod, _ *v1.Node, bindRequest *v1alpha2.BindRequest, _ *state.BindingState,
) error {
	logger := log.FromContext(ctx)

	if !common.IsSharedGPUAllocation(bindRequest) {
		return nil
	}

	var errs []error

	containerRef, err := common.GetFractionContainerRef(pod)
	if err != nil {
		logger.V(1).Info("Rollback: could not get fraction container ref, nothing to rollback",
			"namespace", pod.Namespace, "name", pod.Name, "error", err)
		return nil
	}

	var configMapNames []string
	capabilitiesConfigMapName, err := gpusharingconfigmap.ExtractCapabilitiesConfigMapName(pod, containerRef)
	if err != nil {
		logger.V(1).Info("could not extract capabilities configmap name",
			"namespace", pod.Namespace, "name", pod.Name, "error", err)
	} else if capabilitiesConfigMapName != "" {
		configMapNames = append(configMapNames, capabilitiesConfigMapName)
	}

	directEnvVarsMapName, err := gpusharingconfigmap.ExtractDirectEnvVarsConfigMapName(pod, containerRef)
	if err != nil {
		logger.V(1).Info("could not extract direct env vars configmap name",
			"namespace", pod.Namespace, "name", pod.Name, "error", err)
	} else if directEnvVarsMapName != "" {
		configMapNames = append(configMapNames, directEnvVarsMapName)
	}

	for _, configMapName := range configMapNames {
		if err = p.deleteConfigMap(ctx, pod.Namespace, configMapName); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete configmap %s/%s during rollback: %w",
				pod.Namespace, configMapName, err))
		}
		logger.V(1).Info("deleted configmap", "namespace", pod.Namespace, "name", pod.Name, "configmap", configMapName)
	}

	return errors.Join(errs...)
}

func (p *GPUSharing) deleteConfigMap(ctx context.Context, namespace, name string) error {
	cm := &v1.ConfigMap{}
	cm.Name = name
	cm.Namespace = namespace
	return client.IgnoreNotFound(p.kubeClient.Delete(ctx, cm))
}

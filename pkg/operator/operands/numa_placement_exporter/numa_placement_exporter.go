// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa_placement_exporter

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
)

// numaPluginName is the scheduler plugin whose presence in a shard auto-enables the exporter.
const numaPluginName = "numa"

type NumaPlacementExporter struct {
	lastDesiredState []client.Object
	BaseResourceName string
}

type resourceForKAIConfig func(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config) ([]client.Object, error)

func (e *NumaPlacementExporter) DesiredState(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	if e.BaseResourceName == "" {
		e.BaseResourceName = defaultResourceName
	}

	deploy, err := e.shouldDeploy(ctx, runtimeClient, kaiConfig)
	if err != nil {
		return nil, err
	}
	if !deploy {
		e.lastDesiredState = []client.Object{}
		return nil, nil
	}

	objects := []client.Object{}
	for _, resourceFunc := range []resourceForKAIConfig{
		e.daemonSetForKAIConfig,
		e.serviceAccountForKAIConfig,
	} {
		newResources, err := resourceFunc(ctx, runtimeClient, kaiConfig)
		if err != nil {
			return nil, err
		}
		objects = append(objects, newResources...)
	}

	e.lastDesiredState = objects
	return objects, nil
}

// shouldDeploy resolves the tri-state: an explicit service.enabled pin wins; otherwise deploy iff the
// numa plugin is enabled in some shard (the embedded Service.Enabled is left nil for this auto mode).
func (e *NumaPlacementExporter) shouldDeploy(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) (bool, error) {
	spec := kaiConfig.Spec.NumaPlacementExporter
	if spec != nil && spec.Service != nil && spec.Service.Enabled != nil {
		return *spec.Service.Enabled, nil
	}
	return numaEnabledInAnyShard(ctx, runtimeClient)
}

func numaEnabledInAnyShard(ctx context.Context, runtimeClient client.Reader) (bool, error) {
	shards := &kaiv1.SchedulingShardList{}
	if err := runtimeClient.List(ctx, shards); err != nil {
		return false, err
	}
	for i := range shards.Items {
		plugin, ok := shards.Items[i].Spec.Plugins[numaPluginName]
		if ok && plugin.Enabled != nil && *plugin.Enabled {
			return true, nil
		}
	}
	return false, nil
}

func (e *NumaPlacementExporter) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, e.lastDesiredState)
}

func (e *NumaPlacementExporter) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllControllersAvailable(ctx, readerClient, e.lastDesiredState)
}

func (e *NumaPlacementExporter) Name() string {
	return "NumaPlacementExporter"
}

func (e *NumaPlacementExporter) Monitor(context.Context, client.Reader, *kaiv1.Config) error {
	return nil
}

func (e *NumaPlacementExporter) HasMissingDependencies(context.Context, client.Reader, *kaiv1.Config) (string, error) {
	return "", nil
}

// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package karta

import (
	"context"
	"fmt"
	"strings"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/instructions"
	"github.com/run-ai/karta/pkg/resource"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/grouper"
)

const (
	KartaGroupLabel   = "run.ai/karta-group"
	KartaKindLabel    = "run.ai/karta-kind"
	KartaVersionLabel = "run.ai/karta-version"
)

type KartaGrouper struct {
	kartaSummary   *instructions.StructureSummary
	defaultGrouper grouper.Grouper
}

func (g *KartaGrouper) Name() string {
	return "Karta Grouper"
}

func (g *KartaGrouper) GetPodGroupMetadata(topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	ctx := context.Background()

	gangScheduling := g.getGangSchedulingInstructions()
	if gangScheduling == nil {
		return g.defaultGrouper.GetPodGroupMetadata(topOwner, pod)
	}
	if gangScheduling.PodGroup != nil {
		return g.getPodGroupMetadataV2(ctx, topOwner, pod, gangScheduling.PodGroup)
	}

	return g.getPodGroupMetadataAlpha(ctx, topOwner, pod)
}

func (g *KartaGrouper) getGangSchedulingInstructions() *kartav1alpha1.GangSchedulingInstruction {
	if g.kartaSummary == nil {
		return nil
	}

	karta := g.kartaSummary.GetKarta()
	if karta == nil {
		return nil
	}

	return karta.Spec.Instructions.GangScheduling
}

func (g *KartaGrouper) getPodGroupMetadataV2(
	ctx context.Context,
	topOwner *unstructured.Unstructured,
	pod *v1.Pod,
	podGroupDefinition *kartav1alpha1.PodGroupComponentsMapping,
) (*podgroup.Metadata, error) {
	podGroupMetadata, err := g.defaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}
	podGroupMetadata.Name = g.calcPodGroupName(topOwner, []string{podGroupDefinition.Name})
	podGroupMetadata.MinSubGroup = nil
	podGroupMetadata.SubGroups = nil

	if podGroupDefinition.Topology != nil {
		if err := validateTopologyConstraint(podGroupDefinition.Topology); err != nil {
			return nil, err
		}
		podGroupMetadata.PreferredTopologyLevel = podGroupDefinition.Topology.PreferredTopologyLevel
		podGroupMetadata.RequiredTopologyLevel = podGroupDefinition.Topology.RequiredTopologyLevel
		podGroupMetadata.Topology = podGroupDefinition.Topology.TopologyName
	}

	componentFactory := resource.NewComponentFactoryFromObject(g.kartaSummary.GetKarta(), topOwner)
	podQuerier := resource.NewPodQuerier(pod)
	for _, subGroupMapping := range podGroupDefinition.SubGroups {
		subGroupMetadata, err := g.constructSubgroup(ctx, componentFactory, subGroupMapping, podQuerier, pod)
		if err != nil {
			return nil, err
		}
		podGroupMetadata.SubGroups = append(podGroupMetadata.SubGroups, subGroupMetadata)
	}

	if len(podGroupMetadata.SubGroups) > 0 {
		podGroupMetadata.MinSubGroup = ptr.To(int32(len(podGroupMetadata.SubGroups)))
		podGroupMetadata.MinAvailable = 0
	} else {
		minAvailable, err := instructions.CalculateSubtreeScale(
			ctx, g.kartaSummary.GetKarta().Spec.StructureDefinition.RootComponent.Name, nil, componentFactory, g.kartaSummary,
		)
		if err != nil {
			return nil, err
		}
		podGroupMetadata.MinAvailable = minAvailable
	}

	return podGroupMetadata, nil
}

func (g *KartaGrouper) constructSubgroup(
	ctx context.Context,
	componentFactory *resource.ComponentFactory,
	subGroupMapping kartav1alpha1.SubGroupComponentMapping,
	podQuerier *resource.PodQuerier,
	pod *v1.Pod,
) (*podgroup.SubGroupMetadata, error) {
	if err := validateSubGroupMapping(componentFactory, subGroupMapping); err != nil {
		return nil, err
	}

	minAvailable, err := instructions.CalculateSubtreeScale(ctx, subGroupMapping.ComponentName, nil, componentFactory, g.kartaSummary)
	if err != nil {
		return nil, err
	}
	if minAvailable == 0 {
		return nil, fmt.Errorf("component %s must expose a replica scale definition", subGroupMapping.ComponentName)
	}

	subGroupMetadata := &podgroup.SubGroupMetadata{
		Name:         subGroupMapping.ComponentName,
		MinAvailable: minAvailable,
	}
	if subGroupMapping.Topology != nil {
		if err := validateTopologyConstraint(subGroupMapping.Topology); err != nil {
			return nil, err
		}
		subGroupMetadata.TopologyConstraints = &podgroup.TopologyConstraintMetadata{
			PreferredTopologyLevel: subGroupMapping.Topology.PreferredTopologyLevel,
			RequiredTopologyLevel:  subGroupMapping.Topology.RequiredTopologyLevel,
			Topology:               subGroupMapping.Topology.TopologyName,
		}
	}
	podInSubGroup, err := podMatchesComponentSubtree(ctx, componentFactory, podQuerier, subGroupMapping.ComponentName)
	if err != nil {
		return nil, err
	}
	if podInSubGroup {
		subGroupMetadata.PodsReferences = []string{pod.Name}
	}

	return subGroupMetadata, nil
}

func (g *KartaGrouper) getPodGroupMetadataAlpha(
	ctx context.Context,
	topOwner *unstructured.Unstructured,
	pod *v1.Pod,
) (*podgroup.Metadata, error) {
	podQuerier := resource.NewPodQuerier(pod)
	podComponentName, err := instructions.InferPodComponent(ctx, podQuerier, g.kartaSummary)
	if err != nil {
		return nil, err
	}

	effectiveMemberDefinition, err := instructions.GetPodGroupingEffectiveComponent(ctx, podQuerier, podComponentName, g.kartaSummary)
	if err != nil {
		return nil, err
	}
	if effectiveMemberDefinition == nil {
		return nil, fmt.Errorf("pod %s/%s does not match any Karta pod group member definition", pod.Namespace, pod.Name)
	}

	componentFactory := resource.NewComponentFactoryFromObject(g.kartaSummary.GetKarta(), topOwner)
	var podComponentInstance *string
	if effectiveMemberDefinition.EffectiveComponent == podComponentName {
		podComponentInstance, err = instructions.InferPodComponentInstance(ctx, podQuerier, podComponentName, componentFactory)
		if err != nil {
			return nil, err
		}
	}

	podGroupMetadata, err := g.defaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}

	groupingKeys, err := podQuerier.ExtractGroupKeys(ctx, effectiveMemberDefinition.MemberDefinition.GroupByKeyPaths)
	if err != nil {
		return nil, err
	}
	podGroupMetadata.Name = g.calcPodGroupName(topOwner, groupingKeys)
	if err := validatePodGroupNameFromGroupingKeys(podGroupMetadata.Name, groupingKeys); err != nil {
		return nil, err
	}

	minAvailable, err := instructions.CalculateSubtreeScale(ctx, effectiveMemberDefinition.EffectiveComponent, podComponentInstance, componentFactory, g.kartaSummary)
	if err != nil {
		return nil, err
	}
	podGroupMetadata.MinAvailable = minAvailable

	return podGroupMetadata, nil
}

func (g *KartaGrouper) calcPodGroupName(topOwner *unstructured.Unstructured, groupingKeys []string) string {
	baseName := fmt.Sprintf("%s-%s", constants.PodGroupNamePrefix, topOwner.GetUID())

	if len(groupingKeys) == 0 {
		return baseName
	}
	return fmt.Sprintf("%s-%s", baseName, strings.Join(groupingKeys, "-"))
}

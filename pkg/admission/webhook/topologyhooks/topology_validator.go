// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topologyhooks

import (
	"context"
	"fmt"
	"sort"
	"strings"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
)

var topologyValidatorLog = logf.Log.WithName("topology-validator")

type TopologyValidator interface {
	ValidateCreate(ctx context.Context, obj *kaiv1alpha1.Topology) (warnings admission.Warnings, err error)
	ValidateUpdate(ctx context.Context, oldObj, newObj *kaiv1alpha1.Topology) (warnings admission.Warnings, err error)
	ValidateDelete(ctx context.Context, obj *kaiv1alpha1.Topology) (warnings admission.Warnings, err error)
}

type topologyValidator struct{}

func NewTopologyValidator() TopologyValidator {
	return &topologyValidator{}
}

func (v *topologyValidator) ValidateCreate(_ context.Context, topology *kaiv1alpha1.Topology) (admission.Warnings, error) {
	topologyValidatorLog.Info("validate create", "name", topology.Name)
	return nil, validateAliases(topology.Spec.Levels)
}

func (v *topologyValidator) ValidateUpdate(_ context.Context, _, newTopology *kaiv1alpha1.Topology) (admission.Warnings, error) {
	topologyValidatorLog.Info("validate update", "name", newTopology.Name)
	return nil, validateAliases(newTopology.Spec.Levels)
}

func (v *topologyValidator) ValidateDelete(_ context.Context, _ *kaiv1alpha1.Topology) (admission.Warnings, error) {
	return nil, nil
}

// validateAliases enforces the one-to-one relation between level aliases and node labels: aliases are
// unique across levels, and an alias must not collide with any nodeLabel (which would make resolution
// ambiguous). nodeLabel uniqueness itself is enforced by the CRD's CEL rules.
func validateAliases(levels []kaiv1alpha1.TopologyLevel) error {
	nodeLabels := map[string]struct{}{}
	for _, level := range levels {
		nodeLabels[level.NodeLabel] = struct{}{}
	}

	seen := map[string]struct{}{}
	var duplicates, collisions []string
	for _, level := range levels {
		if level.Alias == "" {
			continue
		}
		if _, ok := seen[level.Alias]; ok {
			duplicates = append(duplicates, level.Alias)
		}
		seen[level.Alias] = struct{}{}
		if _, ok := nodeLabels[level.Alias]; ok {
			collisions = append(collisions, level.Alias)
		}
	}

	var errs []string
	if len(duplicates) > 0 {
		sort.Strings(duplicates)
		errs = append(errs, fmt.Sprintf("aliases must be unique within the topology; duplicated: %v", duplicates))
	}
	if len(collisions) > 0 {
		sort.Strings(collisions)
		errs = append(errs, fmt.Sprintf("an alias must not equal a nodeLabel; conflicting: %v", collisions))
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid topology aliases: %s", strings.Join(errs, "; "))
	}
	return nil
}

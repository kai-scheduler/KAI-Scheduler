// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package karta

import (
	"fmt"
	"strings"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/resource"
	"k8s.io/apimachinery/pkg/util/validation"
)

func validateSubGroupMapping(factory *resource.ComponentFactory, subGroupMapping kartav1alpha1.SubGroupComponentMapping) error {
	if subGroupMapping.ComponentName == "" {
		return fmt.Errorf("subgroup component name cannot be empty")
	}
	if _, err := factory.GetComponent(subGroupMapping.ComponentName); err != nil {
		return err
	}
	return nil
}

func validateTopologyConstraint(topology *kartav1alpha1.TopologyConstraint) error {
	if topology.TopologyName == "" {
		return fmt.Errorf("topology constraint must include topology name")
	}
	if topology.PreferredTopologyLevel == "" && topology.RequiredTopologyLevel == "" {
		return fmt.Errorf("topology constraint must include preferred or required topology level")
	}
	return nil
}

func validatePodGroupNameFromGroupingKeys(podGroupName string, groupingKeys []string) error {
	if len(groupingKeys) == 0 {
		return nil
	}

	if errs := validation.IsDNS1123Subdomain(podGroupName); len(errs) > 0 {
		return fmt.Errorf("Karta grouping keys %v produce invalid Kubernetes PodGroup name %q: %s",
			groupingKeys, podGroupName, strings.Join(errs, "; "))
	}
	return nil
}

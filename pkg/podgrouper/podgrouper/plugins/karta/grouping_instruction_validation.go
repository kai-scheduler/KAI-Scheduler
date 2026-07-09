// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package karta

import (
	"fmt"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/resource"
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

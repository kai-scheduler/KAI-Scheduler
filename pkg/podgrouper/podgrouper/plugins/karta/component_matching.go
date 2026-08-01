// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package karta

import (
	"context"

	"github.com/run-ai/karta/pkg/resource"
)

func podMatchesComponentSubtree(ctx context.Context, factory *resource.ComponentFactory, podQuerier *resource.PodQuerier, subtreeRoot string) (bool, error) {
	component, err := factory.GetComponent(subtreeRoot)
	if err != nil {
		return false, err
	}

	matches, err := podMatchesComponent(ctx, podQuerier, component)
	if err != nil {
		return false, err
	}
	if matches {
		return true, nil
	}

	children, err := factory.GetChildComponentsOf(subtreeRoot)
	if err != nil {
		return false, err
	}
	for _, child := range children {
		childMatches, err := podMatchesComponentSubtree(ctx, factory, podQuerier, child.Name())
		if err != nil {
			return false, err
		}
		if childMatches {
			return true, nil
		}
	}
	return false, nil
}

func podMatchesComponent(ctx context.Context, podQuerier *resource.PodQuerier, component *resource.Component) (bool, error) {
	definition := component.Definition()
	if definition.PodSelector == nil {
		return false, nil
	}
	return podQuerier.MatchesComponentType(ctx, definition.PodSelector.ComponentTypeSelector)
}

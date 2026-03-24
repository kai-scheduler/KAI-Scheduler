// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2alpha2

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func (p *PodGroup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(p).
		WithValidator(&PodGroup{}).
		Complete()
}

func (_ *PodGroup) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	logger := log.FromContext(ctx)
	podGroup, ok := obj.(*PodGroup)
	if !ok {
		return nil, fmt.Errorf("expected a PodGroup but got a %T", obj)
	}
	logger.Info("validate create", "namespace", podGroup.Namespace, "name", podGroup.Name)

	if err := validateSubGroups(podGroup.Spec.SubGroups); err != nil {
		logger.Info("Subgroups validation failed",
			"namespace", podGroup.Namespace, "name", podGroup.Name, "error", err)
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (_ *PodGroup) ValidateUpdate(ctx context.Context, _ runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	logger := log.FromContext(ctx)
	podGroup, ok := newObj.(*PodGroup)
	if !ok {
		return nil, fmt.Errorf("expected a PodGroup but got a %T", newObj)
	}
	logger.Info("validate update", "namespace", podGroup.Namespace, "name", podGroup.Name)

	if err := validateSubGroups(podGroup.Spec.SubGroups); err != nil {
		logger.Info("Subgroups validation failed",
			"namespace", podGroup.Namespace, "name", podGroup.Name, "error", err)
		return nil, err
	}
	return nil, nil
}

func (_ *PodGroup) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	logger := log.FromContext(ctx)
	podGroup, ok := obj.(*PodGroup)
	if !ok {
		return nil, fmt.Errorf("expected a PodGroup but got a %T", obj)
	}
	logger.Info("validate delete", "namespace", podGroup.Namespace, "name", podGroup.Name)
	return nil, nil
}

func validateSubGroups(subGroups []SubGroup) error {
	subGroupMap := map[string]*SubGroup{}
	for _, subGroup := range subGroups {
		if subGroupMap[subGroup.Name] != nil {
			return fmt.Errorf("duplicate subgroup name %s", subGroup.Name)
		}
		subGroupMap[subGroup.Name] = &subGroup
	}

	if err := validateParent(subGroupMap); err != nil {
		return err
	}

	if detectCycle(subGroupMap) {
		return errors.New("cycle detected in subgroups")
	}
	return nil
}

func validateParent(subGroupMap map[string]*SubGroup) error {
	for _, subGroup := range subGroupMap {
		if subGroup.Parent == nil {
			continue
		}
		if _, exists := subGroupMap[*subGroup.Parent]; !exists {
			return fmt.Errorf("parent %s of %s was not found", *subGroup.Parent, subGroup.Name)
		}
	}
	return nil
}

func detectCycle(subGroupMap map[string]*SubGroup) bool {
	graph := map[string][]string{}
	for _, subGroup := range subGroupMap {
		parent := ""
		if subGroup.Parent != nil {
			parent = *subGroup.Parent
		}
		graph[parent] = append(graph[parent], subGroup.Name)
	}

	visited := map[string]bool{}
	recStack := map[string]bool{}

	for name := range subGroupMap {
		if dfsCycleCheck(name, graph, visited, recStack) {
			return true
		}
	}
	return false
}

func dfsCycleCheck(node string, graph map[string][]string, visited, recStack map[string]bool) bool {
	if recStack[node] {
		return true // cycle detected
	}
	if visited[node] {
		return false // already checked this path
	}
	visited[node] = true
	recStack[node] = true

	children := graph[node]
	for _, child := range children {
		if dfsCycleCheck(child, graph, visited, recStack) {
			return true
		}
	}

	recStack[node] = false
	return false
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2alpha2

import (
	"context"
	"errors"
	"fmt"
	"sort"

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

	if err := validatePodGroupSpec(&podGroup.Spec); err != nil {
		logger.Info("PodGroup spec validation failed",
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

	if err := validatePodGroupSpec(&podGroup.Spec); err != nil {
		logger.Info("PodGroup spec validation failed",
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

// validatePodGroupSpec validates the PodGroup spec including top-level minMember/minSubGroup
// mutual exclusivity and subgroup structural rules.
func validatePodGroupSpec(spec *PodGroupSpec) error {
	if spec.MinMember > 0 && spec.MinSubGroup != nil {
		return fmt.Errorf("minMember and minSubGroup are mutually exclusive: "+
			"set minMember (%d) to schedule a fixed number of pods, or set minSubGroup to require a minimum number of child SubGroups, but not both",
			spec.MinMember)
	}

	if err := validateSubGroups(spec.SubGroups); err != nil {
		return err
	}

	if spec.MinSubGroup != nil {
		if *spec.MinSubGroup <= 0 {
			return errors.New("minSubGroup must be greater than 0")
		}
		rootCount := countRootSubGroups(spec.SubGroups)
		if int(*spec.MinSubGroup) > rootCount {
			return fmt.Errorf("minSubGroup (%d) exceeds the number of direct child SubGroups (%d)", *spec.MinSubGroup, rootCount)
		}
	}

	return nil
}

func validateSubGroups(subGroups []SubGroup) error {
	subGroupMap := map[string]*SubGroup{}
	for i := range subGroups {
		subGroup := &subGroups[i]
		if subGroupMap[subGroup.Name] != nil {
			return fmt.Errorf("duplicate subgroup name %s", subGroup.Name)
		}
		subGroupMap[subGroup.Name] = subGroup
	}

	if err := validateParent(subGroupMap); err != nil {
		return err
	}

	if detectCycle(subGroupMap) {
		return errors.New("cycle detected in subgroups")
	}

	childrenMap := buildChildrenMap(subGroupMap)

	// Sort SubGroup names for deterministic error reporting across API calls.
	subGroupNames := make([]string, 0, len(subGroupMap))
	for name := range subGroupMap {
		subGroupNames = append(subGroupNames, name)
	}
	sort.Strings(subGroupNames)
	for _, name := range subGroupNames {
		if err := validateSubGroupMinFields(subGroupMap[name], childrenMap); err != nil {
			return err
		}
	}

	return nil
}

// validateSubGroupMinFields checks mutual exclusivity and structural rules for minMember/minSubGroup
// on a single SubGroup.
func validateSubGroupMinFields(subGroup *SubGroup, childrenMap map[string][]string) error {
	if subGroup.MinMember > 0 && subGroup.MinSubGroup != nil {
		return fmt.Errorf("subgroup %q: minMember and minSubGroup are mutually exclusive", subGroup.Name)
	}

	children := childrenMap[subGroup.Name]
	isLeaf := len(children) == 0

	if isLeaf && subGroup.MinSubGroup != nil {
		return fmt.Errorf("subgroup %q: minSubGroup cannot be set on a leaf SubGroup (no child SubGroups)", subGroup.Name)
	}

	if !isLeaf && subGroup.MinMember > 0 {
		return fmt.Errorf("subgroup %q: minMember cannot be set on a mid-level SubGroup (has child SubGroups); use minSubGroup instead", subGroup.Name)
	}

	if subGroup.MinSubGroup != nil {
		if *subGroup.MinSubGroup <= 0 {
			return fmt.Errorf("subgroup %q: minSubGroup must be greater than 0", subGroup.Name)
		}
		if int(*subGroup.MinSubGroup) > len(children) {
			return fmt.Errorf("subgroup %q: minSubGroup (%d) exceeds the number of direct child SubGroups (%d)",
				subGroup.Name, *subGroup.MinSubGroup, len(children))
		}
	}

	return nil
}

// buildChildrenMap returns a map from parent name to list of child SubGroup names.
func buildChildrenMap(subGroupMap map[string]*SubGroup) map[string][]string {
	childrenMap := map[string][]string{}
	for _, sg := range subGroupMap {
		if sg.Parent != nil {
			childrenMap[*sg.Parent] = append(childrenMap[*sg.Parent], sg.Name)
		}
	}
	return childrenMap
}

// countRootSubGroups returns the number of SubGroups with no parent (direct children of the PodGroup).
func countRootSubGroups(subGroups []SubGroup) int {
	count := 0
	for _, sg := range subGroups {
		if sg.Parent == nil {
			count++
		}
	}
	return count
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

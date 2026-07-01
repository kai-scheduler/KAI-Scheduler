// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2alpha2

import (
	"context"
	"errors"
	"fmt"
	"sort"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func (p *PodGroup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, p).
		WithValidator(&PodGroup{}).
		Complete()
}

func (_ *PodGroup) ValidateCreate(ctx context.Context, podGroup *PodGroup) (admission.Warnings, error) {
	logger := log.FromContext(ctx)
	logger.Info("validate create", "namespace", podGroup.Namespace, "name", podGroup.Name)

	validationErrors := validatePodGroupSpec(&podGroup.Spec)

	if validationErrors.structuralError != nil {
		logger.Info("PodGroup spec validation failed on structural error",
			"namespace", podGroup.Namespace, "name", podGroup.Name, "error", validationErrors.structuralError)
		return nil, validationErrors.structuralError
	}
	if len(validationErrors.minDefinitionErrors) > 0 {
		return handleMinDefinitionErrors(ctx, validationErrors.minDefinitionErrors, podGroup,
			[]error{&minSubGroupExceedsChildCountError{}})
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (_ *PodGroup) ValidateUpdate(ctx context.Context, oldPodGroup *PodGroup, podGroup *PodGroup) (admission.Warnings, error) {
	logger := log.FromContext(ctx)
	logger.Info("validate update", "namespace", podGroup.Namespace, "name", podGroup.Name)

	validationErrors := validatePodGroupSpec(&podGroup.Spec)

	if validationErrors.structuralError != nil {
		logger.Info("PodGroup spec validation failed on structural error",
			"namespace", podGroup.Namespace, "name", podGroup.Name, "error", validationErrors.structuralError)
		return nil, validationErrors.structuralError
	}

	if oldPodGroup != nil && oldPodGroup.Spec.Preemptibility == SemiPreemptible {
		if err := validateSemiPreemptibleImmutability(&oldPodGroup.Spec, &podGroup.Spec); err != nil {
			logger.Info("PodGroup spec validation failed on semi-preemptible immutability",
				"namespace", podGroup.Namespace, "name", podGroup.Name, "error", err)
			return nil, err
		}
	}

	if len(validationErrors.minDefinitionErrors) > 0 {
		return handleMinDefinitionErrors(ctx, validationErrors.minDefinitionErrors, podGroup,
			[]error{&parentMinMemberError{}, &minSubGroupExceedsChildCountError{}})
	}

	return nil, nil
}

// validateSemiPreemptibleImmutability rejects increases to minMember or minSubGroup at the root spec and
// at every matching SubGroup entry (matched by name) on a semi-preemptible PodGroup. Decreases are allowed
// (they only widen the elastic tier). A nil value is treated as unset and cannot be increased into.
func validateSemiPreemptibleImmutability(old, updated *PodGroupSpec) error {
	if increasedInt32(old.MinMember, updated.MinMember) {
		return &semiPreemptibleImmutabilityError{msg: fmt.Sprintf(
			"cannot increase minMember (%s -> %s) on a semi-preemptible PodGroup: it would reclassify running elastic pods as core",
			formatInt32Ptr(old.MinMember), formatInt32Ptr(updated.MinMember))}
	}
	if increasedInt32(old.MinSubGroup, updated.MinSubGroup) {
		return &semiPreemptibleImmutabilityError{msg: fmt.Sprintf(
			"cannot increase minSubGroup (%s -> %s) on a semi-preemptible PodGroup: it would reclassify running elastic subgroups as core",
			formatInt32Ptr(old.MinSubGroup), formatInt32Ptr(updated.MinSubGroup))}
	}

	oldSubGroups := map[string]*SubGroup{}
	for i := range old.SubGroups {
		oldSubGroups[old.SubGroups[i].Name] = &old.SubGroups[i]
	}
	for i := range updated.SubGroups {
		newSG := &updated.SubGroups[i]
		oldSG, found := oldSubGroups[newSG.Name]
		if !found {
			continue
		}
		if increasedInt32(oldSG.MinMember, newSG.MinMember) {
			return &semiPreemptibleImmutabilityError{msg: fmt.Sprintf(
				"cannot increase minMember (%s -> %s) on subgroup %q of a semi-preemptible PodGroup",
				formatInt32Ptr(oldSG.MinMember), formatInt32Ptr(newSG.MinMember), newSG.Name)}
		}
		if increasedInt32(oldSG.MinSubGroup, newSG.MinSubGroup) {
			return &semiPreemptibleImmutabilityError{msg: fmt.Sprintf(
				"cannot increase minSubGroup (%s -> %s) on subgroup %q of a semi-preemptible PodGroup",
				formatInt32Ptr(oldSG.MinSubGroup), formatInt32Ptr(newSG.MinSubGroup), newSG.Name)}
		}
	}

	return nil
}

// increasedInt32 reports whether the value grew from old to updated. A nil old with a non-nil updated is
// treated as an increase (unset -> set raises the minimal satisfying set); other transitions to nil are not.
func increasedInt32(old, updated *int32) bool {
	if updated == nil {
		return false
	}
	if old == nil {
		return true
	}
	return *updated > *old
}

func formatInt32Ptr(v *int32) string {
	if v == nil {
		return "unset"
	}
	return fmt.Sprintf("%d", *v)
}

func handleMinDefinitionErrors(ctx context.Context,
	minDefinitionErrors []error, podGroup *PodGroup, validationWarningTypes []error) (admission.Warnings, error) {
	logger := log.FromContext(ctx)

	var warnings admission.Warnings
	var hardErrs []error
	for _, e := range minDefinitionErrors {
		isWarning := false
		for _, warningType := range validationWarningTypes {
			if errors.Is(e, warningType) {
				warnings = append(warnings, e.Error())
				isWarning = true
				break
			}
		}
		if !isWarning {
			hardErrs = append(hardErrs, e)
		}
	}

	if len(hardErrs) > 0 {
		logger.Info("PodGroup spec validation failed on min definition errors",
			"namespace", podGroup.Namespace, "name", podGroup.Name, "errors", hardErrs,
			"warnings", warnings)
		return warnings, errors.Join(hardErrs...)
	}
	logger.Info("PodGroup spec validation warning", "namespace", podGroup.Namespace, "name", podGroup.Name,
		"warnings", warnings)
	return warnings, nil
}

func (_ *PodGroup) ValidateDelete(ctx context.Context, podGroup *PodGroup) (admission.Warnings, error) {
	logger := log.FromContext(ctx)
	logger.Info("validate delete", "namespace", podGroup.Namespace, "name", podGroup.Name)
	return nil, nil
}

// validatePodGroupSpec validates the PodGroup spec including top-level minMember/minSubGroup
// mutual exclusivity and subgroup structural rules.
// Returns collected per-subgroup errors and a hard structural error.
func validatePodGroupSpec(spec *PodGroupSpec) *validationErrors {
	if spec.MinMember != nil && spec.MinSubGroup != nil {
		return &validationErrors{structuralError: &mutuallyExclusiveFieldsError{msg: fmt.Sprintf(
			"minMember and minSubGroup are mutually exclusive: "+
				"set minMember (%d) to schedule a fixed number of pods, or set minSubGroup to require a minimum number of child SubGroups, but not both",
			*spec.MinMember)}}
	}

	validationErrors := validateSubGroups(spec.SubGroups)
	if validationErrors.structuralError != nil {
		return validationErrors
	}

	if spec.MinSubGroup != nil {
		if *spec.MinSubGroup < 1 {
			validationErrors.minDefinitionErrors = append(validationErrors.minDefinitionErrors,
				&invalidMinSubGroupError{msg: "minSubGroup at the podgroup level must be equal to or greater than 1"})
			return validationErrors
		}
		rootCount := countRootSubGroups(spec.SubGroups)
		if int(*spec.MinSubGroup) > rootCount {
			validationErrors.minDefinitionErrors = append(validationErrors.minDefinitionErrors,
				&minSubGroupExceedsChildCountError{msg: fmt.Sprintf(
					"minSubGroup (%d) exceeds the number of direct child SubGroups (%d)",
					*spec.MinSubGroup, rootCount)})
		}
	}

	return validationErrors
}

// validateSubGroups validates the subgroup list.
// Returns collected per-subgroup min-field errors and a hard structural error (duplicate name, missing parent, cycle).
func validateSubGroups(subGroups []SubGroup) *validationErrors {
	subGroupMap := map[string]*SubGroup{}
	for i := range subGroups {
		subGroup := &subGroups[i]
		if subGroupMap[subGroup.Name] != nil {
			return &validationErrors{structuralError: &subGroupGraphError{msg: fmt.Sprintf("duplicate subgroup name %s", subGroup.Name)}}
		}
		subGroupMap[subGroup.Name] = subGroup
	}

	if err := validateParent(subGroupMap); err != nil {
		return &validationErrors{structuralError: err}
	}

	childrenMap := buildChildrenMap(subGroupMap)

	if detectCycle(subGroupMap, childrenMap) {
		return &validationErrors{structuralError: &subGroupGraphError{msg: "cycle detected in subgroups"}}
	}

	// Sort SubGroup names for deterministic error reporting across API calls.
	subGroupNames := make([]string, 0, len(subGroupMap))
	for name := range subGroupMap {
		subGroupNames = append(subGroupNames, name)
	}
	sort.Strings(subGroupNames)

	subgroupsErrors := &validationErrors{}
	for _, name := range subGroupNames {
		subgroupValidationErrors := validateSubGroupMinFields(subGroupMap[name], childrenMap)
		subgroupsErrors.minDefinitionErrors = append(subgroupsErrors.minDefinitionErrors,
			subgroupValidationErrors.minDefinitionErrors...)
		if subgroupValidationErrors.structuralError != nil {
			subgroupsErrors.structuralError = subgroupValidationErrors.structuralError
			return subgroupsErrors
		}
	}

	return subgroupsErrors
}

// validateSubGroupMinFields returns all validation errors for minMember/minSubGroup on a single SubGroup.
func validateSubGroupMinFields(subGroup *SubGroup, childrenMap map[string][]string) validationErrors {
	var minFieldsErrors []error

	if subGroup.MinMember != nil && subGroup.MinSubGroup != nil {
		return validationErrors{structuralError: &mutuallyExclusiveFieldsError{msg: fmt.Sprintf(
			"subgroup %q: minMember and minSubGroup are mutually exclusive", subGroup.Name)}}
	}

	children := childrenMap[subGroup.Name]
	isLeaf := len(children) == 0

	if isLeaf {
		if subGroup.MinSubGroup != nil {
			minFieldsErrors = append(minFieldsErrors,
				&invalidMinSubGroupError{msg: fmt.Sprintf("subgroup %q: minSubGroup cannot be set on a leaf SubGroup (no child SubGroups)", subGroup.Name)})
		}
		if subGroup.MinMember == nil {
			minFieldsErrors = append(minFieldsErrors, &missingMinMemberError{msg: fmt.Sprintf(
				"subgroup %s: minMember is required", subGroup.Name)})
		}
	} else {
		if subGroup.MinMember != nil {
			minFieldsErrors = append(minFieldsErrors, &parentMinMemberError{msg: fmt.Sprintf(
				"subgroup %q: minMember cannot be set on a mid-level SubGroup (has child SubGroups); use minSubGroup instead", subGroup.Name)})
		}
		if subGroup.MinSubGroup != nil {
			if int(*subGroup.MinSubGroup) > len(children) {
				minFieldsErrors = append(minFieldsErrors, &minSubGroupExceedsChildCountError{msg: fmt.Sprintf(
					"subgroup %q: minSubGroup (%d) exceeds the number of direct child SubGroups (%d)",
					subGroup.Name, *subGroup.MinSubGroup, len(children))})
			}
		}
	}

	return validationErrors{minDefinitionErrors: minFieldsErrors}
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
			return &subGroupGraphError{msg: fmt.Sprintf(
				"parent %s of %s was not found", *subGroup.Parent, subGroup.Name)}
		}
	}
	return nil
}

func detectCycle(subGroupMap map[string]*SubGroup, childrenMap map[string][]string) bool {
	visited := map[string]bool{}
	recStack := map[string]bool{}

	for name := range subGroupMap {
		if dfsCycleCheck(name, childrenMap, visited, recStack) {
			return true
		}
	}
	return false
}

func dfsCycleCheck(node string, childrenMap map[string][]string, visited, recStack map[string]bool) bool {
	if recStack[node] {
		return true // cycle detected
	}
	if visited[node] {
		return false // already checked this path
	}
	visited[node] = true
	recStack[node] = true

	children := childrenMap[node]
	for _, child := range children {
		if dfsCycleCheck(child, childrenMap, visited, recStack) {
			return true
		}
	}

	recStack[node] = false
	return false
}

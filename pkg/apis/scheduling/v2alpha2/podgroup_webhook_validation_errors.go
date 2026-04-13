// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2alpha2

type validationErrors struct {
	minDefinitionErrors []error
	structuralError     error
}

// mutuallyExclusiveFieldsError is returned when both minMember and minSubGroup are set simultaneously.
type mutuallyExclusiveFieldsError struct{ msg string }

func (e *mutuallyExclusiveFieldsError) Error() string { return e.msg }

type invalidMinSubGroupError struct{ msg string }

func (e *invalidMinSubGroupError) Error() string { return e.msg }

// minSubGroupExceedsChildCountError is returned when minSubGroup exceeds the number of direct child SubGroups.
type minSubGroupExceedsChildCountError struct{ msg string }

func (e *minSubGroupExceedsChildCountError) Error() string { return e.msg }
func (e *minSubGroupExceedsChildCountError) Is(target error) bool {
	_, ok := target.(*minSubGroupExceedsChildCountError)
	return ok
}

// parentMinMemberError is returned when minMember is set on a non-leaf SubGroup.
// On update it is downgraded to a warning for backward compatibility.
type parentMinMemberError struct{ msg string }

func (e *parentMinMemberError) Error() string { return e.msg }
func (e *parentMinMemberError) Is(target error) bool {
	_, ok := target.(*parentMinMemberError)
	return ok
}

// missingMinMemberError is returned when a leaf SubGroup does not define minMember.
type missingMinMemberError struct{ msg string }

func (e *missingMinMemberError) Error() string { return e.msg }

// subGroupGraphError is returned for structural issues in the subgroup DAG:
// duplicate names, missing parents, or cycles.
type subGroupGraphError struct{ msg string }

func (e *subGroupGraphError) Error() string { return e.msg }

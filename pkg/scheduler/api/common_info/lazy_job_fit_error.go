// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common_info

import (
	"fmt"
	"sync"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

// LazyJobFitError is a JobFitError implementation that defers string
// formatting to publish time. It is created at action decision points from
// either a FilterResult (plugin rejection) or directly (action-level
// decisions) and produces its formatted message only when first read.
type LazyJobFitError struct {
	reason    enginev2alpha2.UnschedulableReason
	msgFormat string
	msgArgs   []any

	once   sync.Once
	cached string
}

// NewLazyJobFitError creates a job-level error with a deferred message.
// msgFormat is a fmt-style format string; args are captured and formatted only
// when the message is first read.
func NewLazyJobFitError(
	reason enginev2alpha2.UnschedulableReason,
	msgFormat string,
	args ...any,
) *LazyJobFitError {
	return &LazyJobFitError{
		reason:    reason,
		msgFormat: msgFormat,
		msgArgs:   args,
	}
}

// NewLazyJobFitErrorFromFilterResult converts a plugin FilterResult into a
// job-level error. The FilterResult's lazy args are transferred, not formatted.
// The caller is expected to ensure result.Passed is false.
func NewLazyJobFitErrorFromFilterResult(result FilterResult) *LazyJobFitError {
	return &LazyJobFitError{
		reason:    result.ReasonCode,
		msgFormat: result.MsgFormat(),
		msgArgs:   result.MsgArgs(),
	}
}

func (e *LazyJobFitError) message() string {
	e.once.Do(func() {
		if len(e.msgArgs) == 0 {
			e.cached = e.msgFormat
			return
		}
		e.cached = fmt.Sprintf(e.msgFormat, e.msgArgs...)
	})
	return e.cached
}

func (e *LazyJobFitError) Reason() enginev2alpha2.UnschedulableReason {
	return e.reason
}

func (e *LazyJobFitError) DetailedMessage() string { return e.message() }

func (e *LazyJobFitError) Messages() []string { return []string{e.message()} }

func (e *LazyJobFitError) ToUnschedulableExplanation() enginev2alpha2.UnschedulableExplanation {
	return enginev2alpha2.UnschedulableExplanation{
		Reason:  e.reason,
		Message: e.message(),
	}
}

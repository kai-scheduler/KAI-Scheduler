// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common_info

import (
	"fmt"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

// FilterResult carries a plugin's decision and the data needed to explain it.
// String formatting is deferred until Message() is called (at publish time),
// so the scheduling hot path pays no formatting cost on the common Pass() case
// and only the cost of capturing args on rejections.
type FilterResult struct {
	Passed     bool
	ReasonCode enginev2alpha2.UnschedulableReason
	msgFormat  string
	msgArgs    []any
}

// Pass returns a successful FilterResult.
func Pass() FilterResult {
	return FilterResult{Passed: true}
}

// Reject returns a failing FilterResult carrying a reason code and a lazily
// formatted message. msgFormat is a fmt-style format string; args are captured
// by reference and formatted only when Message() is called.
func Reject(reason enginev2alpha2.UnschedulableReason, msgFormat string, args ...any) FilterResult {
	return FilterResult{
		Passed:     false,
		ReasonCode: reason,
		msgFormat:  msgFormat,
		msgArgs:    args,
	}
}

// Message returns the human-readable explanation. Returns "" for a passing
// result. For rejections without format args, returns msgFormat unchanged at
// zero cost.
func (r *FilterResult) Message() string {
	if r.Passed || r.msgFormat == "" {
		return ""
	}
	if len(r.msgArgs) == 0 {
		return r.msgFormat
	}
	return fmt.Sprintf(r.msgFormat, r.msgArgs...)
}

// MsgFormat exposes the raw format string for callers that want to construct
// derived lazy errors without formatting.
func (r *FilterResult) MsgFormat() string { return r.msgFormat }

// MsgArgs exposes the captured format args for callers that want to construct
// derived lazy errors without formatting.
func (r *FilterResult) MsgArgs() []any { return r.msgArgs }

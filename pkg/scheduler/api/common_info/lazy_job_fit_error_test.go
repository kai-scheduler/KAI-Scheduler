// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common_info

import (
	"testing"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

func TestLazyJobFitError_StaticMessage(t *testing.T) {
	e := NewLazyJobFitError(enginev2alpha2.ReclaimQueueAtFairShare, "queue at fair share")
	if e.Reason() != enginev2alpha2.ReclaimQueueAtFairShare {
		t.Fatalf("Reason() mismatch")
	}
	if e.DetailedMessage() != "queue at fair share" {
		t.Fatalf("DetailedMessage() = %q", e.DetailedMessage())
	}
	msgs := e.Messages()
	if len(msgs) != 1 || msgs[0] != "queue at fair share" {
		t.Fatalf("Messages() = %v", msgs)
	}
}

func TestLazyJobFitError_FormattedMessage(t *testing.T) {
	e := NewLazyJobFitError(enginev2alpha2.PreemptNoEligibleVictims,
		"victim %s/%s rejected (priority %d)", "ns", "job", 7)
	want := "victim ns/job rejected (priority 7)"
	if got := e.DetailedMessage(); got != want {
		t.Fatalf("DetailedMessage() = %q, want %q", got, want)
	}
}

func TestLazyJobFitError_FormattedOnce(t *testing.T) {
	calls := 0
	stringer := stringerFn(func() string {
		calls++
		return "computed"
	})
	e := NewLazyJobFitError(enginev2alpha2.ReclaimNoSolutionFound, "value=%s", stringer)
	_ = e.DetailedMessage()
	_ = e.DetailedMessage()
	_ = e.Messages()
	if calls != 1 {
		t.Fatalf("expected 1 format call, got %d", calls)
	}
}

func TestLazyJobFitError_FromFilterResult(t *testing.T) {
	r := Reject(enginev2alpha2.ConsolidationInsufficientGPUs, "need %d GPUs", 4)
	e := NewLazyJobFitErrorFromFilterResult(r)
	if e.Reason() != enginev2alpha2.ConsolidationInsufficientGPUs {
		t.Fatalf("Reason() mismatch: %v", e.Reason())
	}
	if got := e.DetailedMessage(); got != "need 4 GPUs" {
		t.Fatalf("DetailedMessage() = %q", got)
	}
	expl := e.ToUnschedulableExplanation()
	if expl.Reason != enginev2alpha2.ConsolidationInsufficientGPUs {
		t.Fatalf("UnschedulableExplanation reason mismatch")
	}
	if expl.Message != "need 4 GPUs" {
		t.Fatalf("UnschedulableExplanation message mismatch: %q", expl.Message)
	}
}

// stringerFn lets a function act as a fmt.Stringer so we can count format
// invocations in TestLazyJobFitError_FormattedOnce.
type stringerFn func() string

func (s stringerFn) String() string { return s() }

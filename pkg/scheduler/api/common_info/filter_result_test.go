// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common_info

import (
	"testing"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

func TestFilterResult_Pass(t *testing.T) {
	r := Pass()
	if !r.Passed {
		t.Fatalf("Pass() should return Passed=true")
	}
	if msg := r.Message(); msg != "" {
		t.Fatalf("Pass() Message should be empty, got %q", msg)
	}
}

func TestFilterResult_RejectNoArgs(t *testing.T) {
	r := Reject(enginev2alpha2.ReclaimQueueAtFairShare, "static reason")
	if r.Passed {
		t.Fatalf("Reject() should return Passed=false")
	}
	if r.ReasonCode != enginev2alpha2.ReclaimQueueAtFairShare {
		t.Fatalf("ReasonCode mismatch: got %q", r.ReasonCode)
	}
	if msg := r.Message(); msg != "static reason" {
		t.Fatalf("Message() = %q, want %q", msg, "static reason")
	}
}

func TestFilterResult_RejectFormat(t *testing.T) {
	r := Reject(enginev2alpha2.PreemptNoEligibleVictims, "victim %s/%s rejected (priority %d)", "ns", "job", 7)
	if msg := r.Message(); msg != "victim ns/job rejected (priority 7)" {
		t.Fatalf("Message() = %q", msg)
	}
}

func TestFilterResult_MessageOnPassedIsEmpty(t *testing.T) {
	r := FilterResult{Passed: true, msgFormat: "should-not-render"}
	if msg := r.Message(); msg != "" {
		t.Fatalf("Message() on passed result should be empty, got %q", msg)
	}
}

func TestFilterResult_AccessorsExposeRawArgs(t *testing.T) {
	r := Reject(enginev2alpha2.ReclaimNoSolutionFound, "x %d", 42)
	if r.MsgFormat() != "x %d" {
		t.Fatalf("MsgFormat mismatch: %q", r.MsgFormat())
	}
	args := r.MsgArgs()
	if len(args) != 1 || args[0].(int) != 42 {
		t.Fatalf("MsgArgs mismatch: %v", args)
	}
}

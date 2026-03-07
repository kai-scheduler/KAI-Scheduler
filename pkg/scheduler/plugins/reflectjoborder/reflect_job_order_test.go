// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reflectjoborder

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
)

func TestJobOrderPlugin_OnSessionOpen(t *testing.T) {
	ssn := &framework.Session{
		ClusterInfo: &api.ClusterInfo{PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
			"pg1": {UID: "pg1", Priority: 5, Queue: "q1"},
			"pg2": {UID: "pg2", Priority: 2, Queue: "q2"},
		}},
		Config: &conf.SchedulerConfiguration{
			QueueDepthPerAction: map[string]int{"Allocate": 10},
		},
	}
	plugin := &JobOrderPlugin{}
	plugin.OnSessionOpen(ssn)

	if plugin.ReflectJobOrder == nil {
		t.Fatalf("ReflectJobOrder should be initialized")
	}
}

// Test serveJobs returns correct JSON and status when ReflectJobOrder is set
func TestServeJobs_ReflectJobOrderReady(t *testing.T) {
	plugin := &JobOrderPlugin{
		ReflectJobOrder: &ReflectJobOrder{
			GlobalOrder: []JobOrder{{ID: "pg1", Priority: 10}},
			QueueOrder:  map[common_info.QueueID][]JobOrder{"q1": {{ID: "pg1", Priority: 10}}},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/get-job-order", nil)
	rr := httptest.NewRecorder()
	plugin.serveJobs(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected HTTP 200 OK, got %d", rr.Code)
	}
	var resp ReflectJobOrder
	if err := json.NewDecoder(bytes.NewReader(rr.Body.Bytes())).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode serveJobs response: %v", err)
	}
	if len(resp.GlobalOrder) != 1 || resp.GlobalOrder[0].Priority != 10 {
		t.Errorf("Unexpected response json: %+v", resp)
	}
	if len(resp.QueueOrder) != 1 {
		t.Errorf("Expected 1 queue, got %d", len(resp.QueueOrder))
	}
}

// Test serveJobs returns 503 if ReflectJobOrder is nil
func TestServeJobs_ReflectJobOrderNotReady(t *testing.T) {
	plugin := &JobOrderPlugin{ReflectJobOrder: nil}
	req := httptest.NewRequest(http.MethodGet, "/get-job-order", nil)
	rr := httptest.NewRecorder()
	plugin.serveJobs(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected HTTP 503, got %d", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("Job order data not ready")) {
		t.Errorf("Expected error message in body, got: %s", rr.Body.String())
	}
}

// Test serveJobs handles encoding error gracefully
type brokenWriter struct{ http.ResponseWriter }

func (b *brokenWriter) Write(_ []byte) (int, error) { return 0, errEncode }

var errEncode = &encodeError{"forced encode error"}

type encodeError struct{ msg string }

func (e *encodeError) Error() string { return e.msg }

func TestServeJobs_EncodeError(t *testing.T) {
	plugin := &JobOrderPlugin{ReflectJobOrder: &ReflectJobOrder{}}
	req := httptest.NewRequest(http.MethodGet, "/get-job-order", nil)
	rr := httptest.NewRecorder()
	bw := &brokenWriter{rr}
	plugin.serveJobs(bw, req)
	// Should write 500 error
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected HTTP 500, got %d", rr.Code)
	}
}

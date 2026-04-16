// Copyright 2023 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_division"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

type SimulateRequest struct {
	TotalResource rs.ResourceQuantities `json:"totalResource"`
	Queues        []rs.QueueOverrides   `json:"queues"`
}

type QueueFairShare struct {
	GPU    float64 `json:"gpu"`
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
}

type server struct {
	enableCors bool
}

func (s *server) enableCorsHeaders(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func (s *server) simulateHandler(w http.ResponseWriter, r *http.Request) {
	if s.enableCors {
		s.enableCorsHeaders(&w)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	if r.Method != "POST" {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SimulateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	queues := SimulateSetResourcesShare(req.TotalResource, 0, req.Queues)

	resp := make(map[string]QueueFairShare)
	for id, qa := range queues {
		resp[string(id)] = QueueFairShare{
			GPU:    qa.GPU.FairShare,
			CPU:    qa.CPU.FairShare,
			Memory: qa.Memory.FairShare,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

func main() {
	var port = flag.Int("port", 8080, "Port to listen on")
	var enableCors = flag.Bool("enable-cors", false, "Enable CORS headers for cross-origin requests")
	flag.Parse()

	s := &server{
		enableCors: *enableCors,
	}

	http.HandleFunc("/simulate", s.simulateHandler)
	log.Printf("Starting server on port %d (CORS enabled: %v)...", *port, *enableCors)
	err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func SimulateSetResourcesShare(totalResource rs.ResourceQuantities, kValue float64, queueOverrides []rs.QueueOverrides) map[common_info.QueueID]*rs.QueueAttributes {
	queues := make(map[common_info.QueueID]*rs.QueueAttributes)
	for _, qo := range queueOverrides {
		qa := qo.ToQueueAttributes()
		queues[qa.UID] = qa
	}
	resource_division.SetResourcesShare(totalResource, kValue, queues)
	return queues
}

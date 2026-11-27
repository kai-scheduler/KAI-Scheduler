// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Registering handlers while the server serves requests must not race on the handler map.
func TestPluginServerRegisterWhileServing(t *testing.T) {
	ps := newPluginServer(http.NewServeMux())
	handler := func(http.ResponseWriter, *http.Request) {}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 10000 {
			_ = ps.registerPlugin("/get-snapshot", handler)
		}
	}()
	for range 10000 {
		ps.handlePlugin(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/get-snapshot", nil))
	}
	<-done
}

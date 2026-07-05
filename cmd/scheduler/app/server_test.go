// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnauthorizedRoundTripper_PassesNonUnauthorizedResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", http.StatusOK},
		{"403 Forbidden", http.StatusForbidden},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			transport := wrapExitOnUnauthorized(http.DefaultTransport)
			req, _ := http.NewRequest("GET", server.URL, nil)
			resp, err := transport.RoundTrip(req)

			assert.NoError(t, err)
			assert.Equal(t, tc.statusCode, resp.StatusCode)
		})
	}
}

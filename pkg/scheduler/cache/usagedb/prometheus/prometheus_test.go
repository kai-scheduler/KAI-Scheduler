// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package prometheus

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPrometheusClient(t *testing.T) {
	tests := []struct {
		name          string
		address       string
		expectError   bool
		errorContains string
	}{
		{
			name:          "valid address",
			address:       "http://localhost:9090",
			expectError:   false,
			errorContains: "",
		},
		{
			name:          "invalid address",
			address:       "://invalid:9090",
			expectError:   true,
			errorContains: "error creating prometheus client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := &api.UsageParams{}
			params.SetDefaults()
			client, err := NewPrometheusClient(tt.address, params)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				assert.Nil(t, client)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, client)
				promClient, ok := client.(*PrometheusClient)
				require.True(t, ok)
				assert.NotNil(t, promClient.client)
				assert.NotNil(t, promClient.promClient)
			}
		})
	}
}

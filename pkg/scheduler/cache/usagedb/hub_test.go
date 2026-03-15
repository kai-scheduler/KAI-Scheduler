// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package usagedb

import (
	"fmt"
	"os"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	usagedbapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClientFn is a helper function that creates a mock client
func mockClientFn(connectionString string, usageParams *api.UsageParams) (api.Interface, error) {
	if connectionString == "error-connection" {
		return nil, fmt.Errorf("mock client error")
	}
	return fake.NewFakeClient(connectionString, usageParams)
}

// errorClientFn is a helper function that always returns an error
func errorClientFn(connectionString string, usageParams *api.UsageParams) (api.Interface, error) {
	return nil, fmt.Errorf("client creation failed for connection: %s", connectionString)
}

func TestNewClientResolver(t *testing.T) {
	tests := []struct {
		name                string
		clientMapOverrides  map[string]GetClientFn
		expectedClientTypes []string
		description         string
	}{
		{
			name:                "no overrides - should have default fake and prometheus clients",
			clientMapOverrides:  nil,
			expectedClientTypes: []string{"fake", "prometheus"},
			description:         "When no overrides are provided, should contain default fake and prometheus clients",
		},
		{
			name:                "empty overrides - should have default fake and prometheus clients",
			clientMapOverrides:  map[string]GetClientFn{},
			expectedClientTypes: []string{"fake", "prometheus"},
			description:         "Empty overrides map should result in default behavior",
		},
		{
			name: "with additional client type",
			clientMapOverrides: map[string]GetClientFn{
				"custom": mockClientFn,
			},
			expectedClientTypes: []string{"fake", "prometheus", "custom"},
			description:         "Should contain default fake, prometheus, and custom clients",
		},
		{
			name: "override existing client type",
			clientMapOverrides: map[string]GetClientFn{
				"fake": mockClientFn,
			},
			expectedClientTypes: []string{"fake", "prometheus"},
			description:         "Should override the default fake client with custom implementation but keep prometheus",
		},
		{
			name: "multiple overrides",
			clientMapOverrides: map[string]GetClientFn{
				"custom1": mockClientFn,
				"custom2": errorClientFn,
				"fake":    mockClientFn,
			},
			expectedClientTypes: []string{"fake", "prometheus", "custom1", "custom2"},
			description:         "Should contain all client types from overrides and defaults",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewClientResolver(tt.clientMapOverrides)

			// Verify resolver is created
			require.NotNil(t, resolver, "NewClientResolver should not return nil")
			require.NotNil(t, resolver.clientMap, "clientMap should not be nil")

			// Verify expected client types are present
			for _, expectedType := range tt.expectedClientTypes {
				clientFn, exists := resolver.clientMap[expectedType]
				assert.True(t, exists, "Client type '%s' should exist in clientMap", expectedType)
				assert.NotNil(t, clientFn, "Client function for type '%s' should not be nil", expectedType)
			}
		})
	}
}

func TestClientResolver_GetClient(t *testing.T) {
	// Setup environment variable for testing
	testEnvVar := "TEST_HUB_CONNECTION_STRING"
	originalValue := os.Getenv(testEnvVar)
	defer func() {
		if originalValue != "" {
			os.Setenv(testEnvVar, originalValue)
		} else {
			os.Unsetenv(testEnvVar)
		}
	}()

	tests := []struct {
		name          string
		config        *api.UsageDBConfig
		setupEnv      func()
		cleanupEnv    func()
		resolver      *ClientResolver
		wantClient    bool
		wantError     bool
		expectedError string
		description   string
	}{
		{
			name:        "nil config should return nil client",
			config:      nil,
			resolver:    NewClientResolver(nil),
			wantClient:  false,
			wantError:   false,
			description: "Nil config should result in nil client without error",
		},
		{
			name: "empty client type should return error",
			config: &api.UsageDBConfig{
				ClientType:       "",
				ConnectionString: "test-connection",
			},
			resolver:      NewClientResolver(nil),
			wantClient:    false,
			wantError:     true,
			expectedError: "client type cannot be empty",
			description:   "Empty client type should return appropriate error",
		},
		{
			name: "unknown client type should return error",
			config: &api.UsageDBConfig{
				ClientType:       "unknown-type",
				ConnectionString: "test-connection",
			},
			resolver:      NewClientResolver(nil),
			wantClient:    false,
			wantError:     true,
			expectedError: "unknown client type: unknown-type",
			description:   "Unknown client type should return error with supported types",
		},
		{
			name: "successful fake client creation with connection string",
			config: &api.UsageDBConfig{
				ClientType:       "fake",
				ConnectionString: "fake-connection",
			},
			resolver:    NewClientResolver(nil),
			wantClient:  true,
			wantError:   false,
			description: "Valid fake client config should create client successfully",
		},
		{
			name: "successful fake client creation with env var",
			config: &api.UsageDBConfig{
				ClientType:             "fake",
				ConnectionStringEnvVar: testEnvVar,
			},
			setupEnv: func() {
				os.Setenv(testEnvVar, "env-connection-string")
			},
			cleanupEnv: func() {
				os.Unsetenv(testEnvVar)
			},
			resolver:    NewClientResolver(nil),
			wantClient:  true,
			wantError:   false,
			description: "Valid config with env var should create client successfully",
		},
		{
			name: "custom client type successful creation",
			config: &api.UsageDBConfig{
				ClientType:       "custom",
				ConnectionString: "custom-connection",
			},
			resolver: NewClientResolver(map[string]GetClientFn{
				"custom": mockClientFn,
			}),
			wantClient:  true,
			wantError:   false,
			description: "Custom client type should be created successfully",
		},
		{
			name: "custom client creation error",
			config: &api.UsageDBConfig{
				ClientType:       "error-client",
				ConnectionString: "test-connection",
			},
			resolver: NewClientResolver(map[string]GetClientFn{
				"error-client": errorClientFn,
			}),
			wantClient:    false,
			wantError:     true,
			expectedError: "client creation failed for connection",
			description:   "Client creation error should be propagated",
		},
		{
			name: "mock client with error connection string",
			config: &api.UsageDBConfig{
				ClientType:       "mock",
				ConnectionString: "error-connection",
			},
			resolver: NewClientResolver(map[string]GetClientFn{
				"mock": mockClientFn,
			}),
			wantClient:    false,
			wantError:     true,
			expectedError: "mock client error",
			description:   "Mock client with error connection should return error",
		},
		{
			name: "connection string resolution error - both set",
			config: &api.UsageDBConfig{
				ClientType:             "fake",
				ConnectionString:       "direct-connection",
				ConnectionStringEnvVar: testEnvVar,
			},
			setupEnv: func() {
				os.Setenv(testEnvVar, "env-connection-string")
			},
			cleanupEnv: func() {
				os.Unsetenv(testEnvVar)
			},
			resolver:      NewClientResolver(nil),
			wantClient:    false,
			wantError:     true,
			expectedError: "both connection string and connection string env var are set",
			description:   "Config with both connection string and env var should error",
		},
		{
			name: "connection string resolution error - neither set",
			config: &api.UsageDBConfig{
				ClientType:             "fake",
				ConnectionString:       "",
				ConnectionStringEnvVar: "",
			},
			resolver:      NewClientResolver(nil),
			wantClient:    false,
			wantError:     true,
			expectedError: "connection string and connection string env var are not set",
			description:   "Config with neither connection string nor env var should error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment if needed
			if tt.setupEnv != nil {
				tt.setupEnv()
			}
			defer func() {
				if tt.cleanupEnv != nil {
					tt.cleanupEnv()
				}
			}()

			client, err := tt.resolver.GetClient(tt.config)

			if tt.wantError {
				assert.Error(t, err, "Expected error but got none")
				assert.Nil(t, client, "Client should be nil when error occurs")
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError,
						"Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Unexpected error: %v", err)
				if tt.wantClient {
					assert.NotNil(t, client, "Expected client but got nil")
				} else {
					assert.Nil(t, client, "Expected nil client")
				}
			}
		})
	}
}

func TestClientResolver_GetClient_Integration(t *testing.T) {
	t.Run("end-to-end fake client usage", func(t *testing.T) {
		resolver := NewClientResolver(nil)

		config := &api.UsageDBConfig{
			ClientType:       "fake",
			ConnectionString: "integration-test-connection",
		}

		client, err := resolver.GetClient(config)
		require.NoError(t, err)
		require.NotNil(t, client)

		// Verify we can use the client
		usage, err := client.GetResourceUsage()
		assert.NoError(t, err)
		assert.NotNil(t, usage)
	})

	t.Run("custom client integration", func(t *testing.T) {
		customClientCreated := false
		customClientFn := func(connectionString string, usageParams *api.UsageParams) (api.Interface, error) {
			customClientCreated = true
			assert.Equal(t, "custom-integration-connection", connectionString)
			return fake.NewFakeClient(connectionString, nil)
		}

		resolver := NewClientResolver(map[string]GetClientFn{
			"custom-integration": customClientFn,
		})

		config := &api.UsageDBConfig{
			ClientType:       "custom-integration",
			ConnectionString: "custom-integration-connection",
		}

		client, err := resolver.GetClient(config)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.True(t, customClientCreated, "Custom client function should have been called")

		// Verify the client works
		usage, err := client.GetResourceUsage()
		assert.NoError(t, err)
		assert.NotNil(t, usage)
	})
}

func TestClientResolver_DefaultBehavior(t *testing.T) {
	t.Run("default client map should contain fake and prometheus", func(t *testing.T) {
		// Test the module-level defaultClientMap
		assert.NotNil(t, defaultClientMap)
		assert.Contains(t, defaultClientMap, "fake")
		assert.Contains(t, defaultClientMap, "prometheus")
		// Can't directly compare function pointers, but we can verify they're not nil
		assert.NotNil(t, defaultClientMap["fake"])
		assert.NotNil(t, defaultClientMap["prometheus"])
	})

	t.Run("resolver should clone default map", func(t *testing.T) {
		resolver1 := NewClientResolver(nil)
		resolver2 := NewClientResolver(map[string]GetClientFn{
			"custom": mockClientFn,
		})

		// Verify resolver1 has fake and prometheus
		assert.Len(t, resolver1.clientMap, 3)
		assert.Contains(t, resolver1.clientMap, "fake")
		assert.Contains(t, resolver1.clientMap, "fake-with-history")
		assert.Contains(t, resolver1.clientMap, "prometheus")

		// Verify resolver2 has fake, prometheus, and custom
		assert.Len(t, resolver2.clientMap, 4)
		assert.Contains(t, resolver2.clientMap, "fake")
		assert.Contains(t, resolver2.clientMap, "fake-with-history")
		assert.Contains(t, resolver2.clientMap, "prometheus")
		assert.Contains(t, resolver2.clientMap, "custom")

		// Verify that modifying one doesn't affect the default
		assert.Len(t, defaultClientMap, 3)
		assert.Contains(t, defaultClientMap, "fake")
		assert.Contains(t, defaultClientMap, "fake-with-history")
		assert.Contains(t, defaultClientMap, "prometheus")
	})
}

func TestGetClient(t *testing.T) {
	tests := []struct {
		name   string
		config *usagedbapi.UsageDBConfig

		wantError bool
		wantNil   bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantNil: true,
		},
		{
			name: "fake client",
			config: &usagedbapi.UsageDBConfig{
				ClientType:       "fake",
				ConnectionString: "fake-connection",
			},
		},
		{
			name: "prometheus client",
			config: &usagedbapi.UsageDBConfig{
				ClientType:       "prometheus",
				ConnectionString: "http://localhost:9090",
			},
		},
		{
			name: "unknown client type",
			config: &usagedbapi.UsageDBConfig{
				ClientType:       "unknown",
				ConnectionString: "test-connection",
			},
			wantError: true,
		},
		{
			name: "empty client type",
			config: &usagedbapi.UsageDBConfig{
				ClientType:       "",
				ConnectionString: "test-connection",
			},
			wantError: true,
		},
	}

	resolver := NewClientResolver(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			client, err := resolver.GetClient(tt.config)

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, client)
				if tt.config != nil {
					if tt.config.ClientType == "" {
						assert.Contains(t, err.Error(), "client type cannot be empty")
					} else {
						assert.Contains(t, err.Error(), "unknown client type")
						assert.Contains(t, err.Error(), tt.config.ClientType)
					}
				}
				return
			}

			if tt.wantNil {
				assert.NoError(t, err)
				assert.Nil(t, client)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, client)
		})
	}
}

func TestResolveConnectionString(t *testing.T) {
	// Save original env var values to restore later
	testEnvVar := "TEST_CONNECTION_STRING"
	originalValue := os.Getenv(testEnvVar)
	defer func() {
		if originalValue != "" {
			os.Setenv(testEnvVar, originalValue)
		} else {
			os.Unsetenv(testEnvVar)
		}
	}()

	tests := []struct {
		name        string
		config      *usagedbapi.UsageDBConfig
		envVarValue string
		setEnvVar   bool
		want        string
		wantErr     bool
		wantErrMsg  string
	}{
		{
			name: "both connection string and env var set - should error",
			config: &usagedbapi.UsageDBConfig{
				ConnectionString:       "direct-connection",
				ConnectionStringEnvVar: testEnvVar,
			},
			envVarValue: "env-connection",
			setEnvVar:   true,
			wantErr:     true,
			wantErrMsg:  "both connection string and connection string env var are set, only one is allowed",
		},
		{
			name: "neither connection string nor env var set - should error",
			config: &usagedbapi.UsageDBConfig{
				ConnectionString:       "",
				ConnectionStringEnvVar: "",
			},
			wantErr:    true,
			wantErrMsg: "connection string and connection string env var are not set, one is required",
		},
		{
			name: "only connection string set - should return connection string",
			config: &usagedbapi.UsageDBConfig{
				ConnectionString:       "direct-connection",
				ConnectionStringEnvVar: "",
			},
			want: "direct-connection",
		},
		{
			name: "only env var set with value - should return env var value",
			config: &usagedbapi.UsageDBConfig{
				ConnectionString:       "",
				ConnectionStringEnvVar: testEnvVar,
			},
			envVarValue: "env-connection-value",
			setEnvVar:   true,
			want:        "env-connection-value",
		},
		{
			name: "only env var set but empty value - should return empty string",
			config: &usagedbapi.UsageDBConfig{
				ConnectionString:       "",
				ConnectionStringEnvVar: testEnvVar,
			},
			envVarValue: "",
			setEnvVar:   true,
			want:        "",
		},
		{
			name: "env var set but not in environment - should return empty string",
			config: &usagedbapi.UsageDBConfig{
				ConnectionString:       "",
				ConnectionStringEnvVar: "NON_EXISTENT_ENV_VAR",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variable if needed
			if tt.setEnvVar {
				os.Setenv(testEnvVar, tt.envVarValue)
			} else {
				os.Unsetenv(testEnvVar)
			}

			got, err := resolveConnectionString(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				assert.Empty(t, got)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

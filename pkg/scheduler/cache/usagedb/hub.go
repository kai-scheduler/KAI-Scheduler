// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package usagedb

import (
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/prometheus"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// GetClientFn creates a usage db client. auth is nil when the usage database
// needs no authentication; a client type that cannot authenticate ignores it.
type GetClientFn func(connectionString string, usageParams *api.UsageParams, auth *api.Auth) (api.Interface, error)

var defaultClientMap = map[string]GetClientFn{
	"fake":              fake.NewFakeClient,
	"fake-with-history": fake.NewFakeWithHistoryClient,
	"prometheus":        prometheus.NewPrometheusClient,
}

type ClientResolver struct {
	clientMap map[string]GetClientFn
}

func NewClientResolver(clientMapOverrides map[string]GetClientFn) *ClientResolver {
	clientMap := maps.Clone(defaultClientMap)
	for k, v := range clientMapOverrides {
		clientMap[k] = v
	}

	return &ClientResolver{
		clientMap: clientMap,
	}
}

func (cr *ClientResolver) GetClient(config *api.UsageDBConfig) (api.Interface, error) {
	if config == nil {
		return nil, nil
	}

	if config.ClientType == "" {
		return nil, fmt.Errorf("client type cannot be empty")
	}

	client, ok := cr.clientMap[config.ClientType]
	if !ok {
		supportedTypes := slices.Collect(maps.Keys(cr.clientMap))
		return nil, fmt.Errorf("unknown client type: %s, supported types: %v", config.ClientType, supportedTypes)
	}

	connectionString, err := resolveConnectionString(config)
	if err != nil {
		return nil, err
	}

	if err = config.Auth.Validate(); err != nil {
		return nil, fmt.Errorf("invalid usage db auth: %w", err)
	}

	log.InfraLogger.V(3).Infof("getting usage db client of type: %s, connection string: %s",
		config.ClientType, redactConnectionString(connectionString))

	return client(connectionString, config.GetUsageParams(), config.Auth)
}

// redactConnectionString returns the connection string in a form that is safe
// to log: a URL keeps everything but the password of its userinfo, and a
// string that is not a URL, which may be a DSN with a credential anywhere in
// it, is not shown at all.
func redactConnectionString(connectionString string) string {
	if connectionString == "" {
		return ""
	}
	parsed, err := url.Parse(connectionString)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<redacted>"
	}
	return parsed.Redacted()
}

func resolveConnectionString(config *api.UsageDBConfig) (string, error) {
	if config.ConnectionString != "" && config.ConnectionStringEnvVar != "" {
		return "", fmt.Errorf("both connection string and connection string env var are set, only one is allowed")
	}

	if config.ConnectionString == "" && config.ConnectionStringEnvVar == "" {
		return "", fmt.Errorf("connection string and connection string env var are not set, one is required")
	}

	if config.ConnectionStringEnvVar != "" {
		log.InfraLogger.V(3).Infof("getting connection string from env var: %s", config.ConnectionStringEnvVar)
		return os.Getenv(config.ConnectionStringEnvVar), nil
	}

	return config.ConnectionString, nil
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package usagedb

import (
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache/usagedb/fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache/usagedb/prometheus"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
)

type GetClientFn func(connectionString string, usageParams *api.UsageParams) (api.Interface, error)

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

	log.InfraLogger.V(3).Infof("getting usage db client of type: %s, connection string: %s", config.ClientType, config.ConnectionString)

	connectionString, err := resolveConnectionString(config)
	log.InfraLogger.V(3).Infof("resolved connection string: %s", connectionString)
	if err != nil {
		return nil, err
	}

	log.InfraLogger.V(3).Infof("getting usage db client of type: %s, connection string: %s", config.ClientType, connectionString)

	return client(connectionString, config.GetUsageParams())
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

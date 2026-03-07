/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"net/http"
	"time"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/metrics"
)

func OpenSession(cache cache.Cache, config *conf.SchedulerConfiguration,
	schedulerParams *conf.SchedulerParams, sessionId string, mux *http.ServeMux) (*Session, error) {
	openSessionStart := time.Now()
	defer metrics.UpdateOpenSessionDuration(openSessionStart)

	if server == nil {
		server = newPluginServer(mux)
	}

	ssn, err := openSession(cache, sessionId, *schedulerParams, mux)
	if err != nil {
		return nil, err
	}
	ssn.Config = config

	for _, tier := range config.Tiers {
		for _, pluginOption := range tier.Plugins {
			pb, found := GetPluginBuilder(pluginOption.Name)
			if !found {
				log.InfraLogger.Errorf("Failed to get plugin %s.", pluginOption.Name)
				continue
			}

			plugin := pb(pluginOption.Arguments)
			ssn.plugins[plugin.Name()] = plugin

			onSessionOpenPluginStart := time.Now()
			plugin.OnSessionOpen(ssn)
			metrics.UpdatePluginDuration(plugin.Name(), metrics.OnSessionOpen, metrics.Duration(onSessionOpenPluginStart))
		}
	}

	return ssn, nil
}

func CloseSession(ssn *Session) {
	closeSessionStart := time.Now()
	defer metrics.UpdateCloseSessionDuration(closeSessionStart)

	for _, plugin := range ssn.plugins {
		onSessionCloseStart := time.Now()
		plugin.OnSessionClose(ssn)
		metrics.UpdatePluginDuration(plugin.Name(), metrics.OnSessionClose, metrics.Duration(onSessionCloseStart))
	}

	closeSession(ssn)
}

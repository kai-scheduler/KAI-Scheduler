/*
Copyright 2017 The Kubernetes Authors.

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

package scheduler

import (
	"fmt"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	kubeaischedulerver "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned"
	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	schedcache "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb"
	usagedbapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins"

	kueue "sigs.k8s.io/kueue/client-go/clientset/versioned"
)

type Scheduler struct {
	cache           schedcache.Cache
	config          *conf.SchedulerConfiguration
	schedulerParams *conf.SchedulerParams
	schedulePeriod  time.Duration
	mux             *http.ServeMux
}

func NewScheduler(
	config *rest.Config,
	schedulerConfPath string,
	schedulerParams *conf.SchedulerParams,
	mux *http.ServeMux,
) (*Scheduler, error) {
	kubeClient, kubeAiSchedulerClient, kueueClient := newClients(config)

	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	if err := featuregates.SetDRAFeatureGate(config); err != nil {
		log.InfraLogger.Errorf("Failed to set DRA feature gate: ", err)
		return nil, err
	}

	// Load configuration of scheduler
	schedConfig, err := conf_util.ResolveConfigurationFromFile(schedulerConfPath)
	if err != nil {
		return nil, fmt.Errorf("error resolving configuration from file: %v", err)
	}

	usageDBClient, err := getUsageDBClient(schedConfig.UsageDBConfig)
	if err != nil {
		return nil, fmt.Errorf("error getting usage db client: %v", err)
	}

	schedulerCacheParams := &schedcache.SchedulerCacheParams{
		KubeClient:                  kubeClient,
		KAISchedulerClient:          kubeAiSchedulerClient,
		KueueClient:                 kueueClient,
		UsageDBClient:               usageDBClient,
		SchedulerName:               schedulerParams.SchedulerName,
		NodePoolParams:              schedulerParams.PartitionParams,
		RestrictNodeScheduling:      schedulerParams.RestrictSchedulingNodes,
		DetailedFitErrors:           schedulerParams.DetailedFitErrors,
		ScheduleCSIStorage:          schedulerParams.ScheduleCSIStorage,
		FullHierarchyFairness:       schedulerParams.FullHierarchyFairness,
		NumOfStatusRecordingWorkers: schedulerParams.NumOfStatusRecordingWorkers,
		UpdatePodEvictionCondition:  schedulerParams.UpdatePodEvictionCondition,
	}

	scheduler := &Scheduler{
		config:          schedConfig,
		schedulerParams: schedulerParams,
		cache:           schedcache.New(schedulerCacheParams),
		schedulePeriod:  schedulerParams.SchedulePeriod,
		mux:             mux,
	}

	return scheduler, nil
}

func (s *Scheduler) Run(stopCh <-chan struct{}) {
	s.cache.Run(stopCh)
	s.cache.WaitForCacheSync(stopCh)

	go func() {
		wait.Until(s.runOnce, s.schedulePeriod, stopCh)
	}()
}

func (s *Scheduler) runOnce() {
	sessionId := uuid.NewUUID()
	log.InfraLogger.SetSessionID(string(sessionId))

	log.InfraLogger.V(1).Infof("Start scheduling ...")
	scheduleStartTime := time.Now()
	defer log.InfraLogger.V(1).Infof("End scheduling ...")

	defer metrics.UpdateE2eDuration(scheduleStartTime)

	ssn, err := framework.OpenSession(s.cache, s.config, s.schedulerParams, sessionId, s.mux)
	if err != nil {
		log.InfraLogger.Errorf("Error while opening session, will try again next cycle. \nCause: %+v", err)
		return
	}
	defer framework.CloseSession(ssn)

	actions, _ := conf_util.GetActionsFromConfig(s.config)
	for _, action := range actions {
		log.InfraLogger.SetAction(string(action.Name()))
		metrics.SetCurrentAction(string(action.Name()))
		actionStartTime := time.Now()
		action.Execute(ssn)
		metrics.UpdateActionDuration(string(action.Name()), metrics.Duration(actionStartTime))
	}
	log.InfraLogger.RemoveActionLogger()
}

func newClients(config *rest.Config) (kubernetes.Interface, kubeaischedulerver.Interface, kueue.Interface) {
	k8cClientConfig := rest.CopyConfig(config)

	// Force protobuf serialization for k8s built-in resources
	k8cClientConfig.AcceptContentTypes = "application/vnd.kubernetes.protobuf"
	k8cClientConfig.ContentType = "application/vnd.kubernetes.protobuf"
	return kubernetes.NewForConfigOrDie(k8cClientConfig), kubeaischedulerver.NewForConfigOrDie(config), kueue.NewForConfigOrDie(config)
}

func getUsageDBClient(dbConfig *usagedbapi.UsageDBConfig) (usagedbapi.Interface, error) {
	resolver := usagedb.NewClientResolver(nil)
	return resolver.GetClient(dbConfig)
}

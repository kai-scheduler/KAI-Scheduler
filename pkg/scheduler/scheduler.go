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
	"math/rand"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	kubeaischedulerver "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned"
	schedcache "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb"
	api "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	usagedbapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
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
	schedulerConf *conf.SchedulerConfiguration,
	schedulerParams *conf.SchedulerParams,
	mux *http.ServeMux,
) (*Scheduler, error) {
	kubeClient, kubeAiSchedulerClient := newClients(config)

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("Failed to create discovery client: %v", err)
	}

	usageDBClient, err := getUsageDBClient(schedulerConf.UsageDBConfig)
	if err != nil {
		return nil, fmt.Errorf("error getting usage db client: %v", err)
	}

	var usageDBParams *api.UsageParams
	if schedulerConf.UsageDBConfig != nil {
		usageDBParams = schedulerConf.UsageDBConfig.GetUsageParams()
	}

	schedulerCacheParams := &schedcache.SchedulerCacheParams{
		KubeClient:                  kubeClient,
		KAISchedulerClient:          kubeAiSchedulerClient,
		UsageDBParams:               usageDBParams,
		UsageDBClient:               usageDBClient,
		SchedulerName:               schedulerParams.SchedulerName,
		NodePoolParams:              schedulerParams.PartitionParams,
		RestrictNodeScheduling:      schedulerParams.RestrictSchedulingNodes,
		DetailedFitErrors:           schedulerParams.DetailedFitErrors,
		ScheduleCSIStorage:          schedulerParams.ScheduleCSIStorage,
		FullHierarchyFairness:       schedulerParams.FullHierarchyFairness,
		NumOfStatusRecordingWorkers: schedulerParams.NumOfStatusRecordingWorkers,
		UpdatePodEvictionCondition:  schedulerParams.UpdatePodEvictionCondition,
		DiscoveryClient:             discoveryClient,
	}

	scheduler := &Scheduler{
		config:          schedulerConf,
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
	sessionId := generateSessionID(6)
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

func newClients(config *rest.Config) (kubernetes.Interface, kubeaischedulerver.Interface) {
	k8cClientConfig := rest.CopyConfig(config)

	// Force protobuf serialization for k8s built-in resources
	k8cClientConfig.AcceptContentTypes = "application/vnd.kubernetes.protobuf"
	k8cClientConfig.ContentType = "application/vnd.kubernetes.protobuf"
	return kubernetes.NewForConfigOrDie(k8cClientConfig), kubeaischedulerver.NewForConfigOrDie(config)
}

func getUsageDBClient(dbConfig *usagedbapi.UsageDBConfig) (usagedbapi.Interface, error) {
	resolver := usagedb.NewClientResolver(nil)
	return resolver.GetClient(dbConfig)
}

func generateSessionID(l int) string {
	str := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	bytes := []byte(str)
	var result []byte
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for range l {
		result = append(result, bytes[r.Intn(len(bytes))])
	}

	return string(result)
}

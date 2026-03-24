// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	kubeaischedulerver "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned"
	featuregates "github.com/NVIDIA/KAI-scheduler/pkg/common/feature_gates"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions"
	schedcache "github.com/NVIDIA/KAI-scheduler/pkg/scheduler/cache"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/metrics"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins"
)

type Scheduler struct {
	cache             schedcache.Cache
	config            *conf.SchedulerConfiguration
	schedulerParams   *conf.SchedulerParams
	schedulerConfPath string
	schedulePeriod    time.Duration
	mux               *http.ServeMux
}

func NewScheduler(
	config *rest.Config,
	schedulerConfPath string,
	schedulerParams *conf.SchedulerParams,
	mux *http.ServeMux,
) (*Scheduler, error) {
	kubeClient, kubeAiSchedulerClient := newClients(config)

	if err := featuregates.SetDRAFeatureGate(config); err != nil {
		log.InfraLogger.Errorf("Failed to set DRA feature gate: ", err)
		return nil, err
	}
	schedulerCacheParams := &schedcache.SchedulerCacheParams{
		KubeClient:                  kubeClient,
		KAISchedulerClient:          kubeAiSchedulerClient,
		SchedulerName:               schedulerParams.SchedulerName,
		NodePoolParams:              schedulerParams.PartitionParams,
		RestrictNodeScheduling:      schedulerParams.RestrictSchedulingNodes,
		DetailedFitErrors:           schedulerParams.DetailedFitErrors,
		ScheduleCSIStorage:          schedulerParams.ScheduleCSIStorage,
		FullHierarchyFairness:       schedulerParams.FullHierarchyFairness,
		NodeLevelScheduler:          schedulerParams.NodeLevelScheduler,
		NumOfStatusRecordingWorkers: schedulerParams.NumOfStatusRecordingWorkers,
	}

	scheduler := &Scheduler{
		schedulerParams:   schedulerParams,
		schedulerConfPath: schedulerConfPath,
		cache:             schedcache.New(schedulerCacheParams),
		schedulePeriod:    schedulerParams.SchedulePeriod,
		mux:               mux,
	}

	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	return scheduler, nil
}

func (s *Scheduler) Run(stopCh <-chan struct{}) {
	var err error

	s.cache.Run(stopCh)
	s.cache.WaitForCacheSync(stopCh)

	// Load configuration of scheduler
	s.config, err = conf_util.ResolveConfigurationFromFile(s.schedulerConfPath)
	if err != nil {
		panic(err)
	}

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

func newClients(config *rest.Config) (kubernetes.Interface, kubeaischedulerver.Interface) {
	k8cClientConfig := rest.CopyConfig(config)

	// Force protobuf serialization for k8s built-in resources
	k8cClientConfig.AcceptContentTypes = "application/vnd.kubernetes.protobuf"
	k8cClientConfig.ContentType = "application/vnd.kubernetes.protobuf"
	return kubernetes.NewForConfigOrDie(k8cClientConfig), kubeaischedulerver.NewForConfigOrDie(config)
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8spolicyv1 "k8s.io/api/policy/v1"
	resourceapi "k8s.io/api/resource/v1"
	v14 "k8s.io/api/scheduling/v1"
	storage "k8s.io/api/storage/v1"

	schedulingv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	enginev2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	enginev2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/conf"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
)

const (
	SnapshotFileName = "snapshot.json"
)

// RawKubernetesObjects contains the raw Kubernetes objects from the cluster
type RawKubernetesObjects struct {
	Pods                   []*v1.Pod                          `json:"pods"`
	Nodes                  []*v1.Node                         `json:"nodes"`
	Queues                 []*enginev2.Queue                  `json:"queues"`
	PodGroups              []*enginev2alpha2.PodGroup         `json:"podGroups"`
	BindRequests           []*schedulingv1alpha2.BindRequest  `json:"bindRequests"`
	PodDisruptionBudgets   []*k8spolicyv1.PodDisruptionBudget `json:"podDisruptionBudgets"`
	PriorityClasses        []*v14.PriorityClass               `json:"priorityClasses"`
	ConfigMaps             []*v1.ConfigMap                    `json:"configMaps"`
	PersistentVolumeClaims []*v1.PersistentVolumeClaim        `json:"persistentVolumeClaims"`
	CSIStorageCapacities   []*storage.CSIStorageCapacity      `json:"csiStorageCapacities"`
	StorageClasses         []*storage.StorageClass            `json:"storageClasses"`
	CSIDrivers             []*storage.CSIDriver               `json:"csiDrivers"`
	ResourceClaims         []*resourceapi.ResourceClaim       `json:"resourceClaims"`
	ResourceSlices         []*resourceapi.ResourceSlice       `json:"resourceSlices"`
	DeviceClasses          []*resourceapi.DeviceClass         `json:"deviceClasses"`
}

type Snapshot struct {
	Config          *conf.SchedulerConfiguration `json:"config"`
	SchedulerParams *conf.SchedulerParams        `json:"schedulerParams"`
	RawObjects      *RawKubernetesObjects        `json:"rawObjects"`
}

type snapshotPlugin struct {
	session *framework.Session
}

func (sp *snapshotPlugin) Name() string {
	return "snapshot"
}

func (sp *snapshotPlugin) OnSessionOpen(ssn *framework.Session) {
	sp.session = ssn
	log.InfraLogger.V(3).Info("Snapshot plugin registering get-snapshot")
	ssn.AddHttpHandler("/get-snapshot", sp.serveSnapshot)
}

func (sp *snapshotPlugin) OnSessionClose(ssn *framework.Session) {}

func (sp *snapshotPlugin) serveSnapshot(writer http.ResponseWriter, request *http.Request) {
	rawObjects := &RawKubernetesObjects{}
	var err error

	dataLister := sp.session.Cache.GetDataLister()

	rawObjects.Pods, err = dataLister.ListPods()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw pods: %v", err)
		rawObjects.Pods = []*v1.Pod{}
	}

	rawObjects.Nodes, err = dataLister.ListNodes()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw nodes: %v", err)
		rawObjects.Nodes = []*v1.Node{}
	}

	rawObjects.Queues, err = dataLister.ListQueues()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw queues: %v", err)
		rawObjects.Queues = []*enginev2.Queue{}
	}

	rawObjects.PodGroups, err = dataLister.ListPodGroups()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw pod groups: %v", err)
		rawObjects.PodGroups = []*enginev2alpha2.PodGroup{}
	}

	rawObjects.BindRequests, err = dataLister.ListBindRequests()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw bind requests: %v", err)
		rawObjects.BindRequests = []*schedulingv1alpha2.BindRequest{}
	}

	rawObjects.PodDisruptionBudgets, err = dataLister.ListPodDisruptionBudgets()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw pod disruption budgets: %v", err)
		rawObjects.PodDisruptionBudgets = []*k8spolicyv1.PodDisruptionBudget{}
	}

	rawObjects.PriorityClasses, err = dataLister.ListPriorityClasses()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw priority classes: %v", err)
		rawObjects.PriorityClasses = []*v14.PriorityClass{}
	}

	rawObjects.ConfigMaps, err = dataLister.ListConfigMaps()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw config maps: %v", err)
		rawObjects.ConfigMaps = []*v1.ConfigMap{}
	}

	rawObjects.PersistentVolumeClaims, err = dataLister.ListPersistentVolumeClaims()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw persistent volume claims: %v", err)
		rawObjects.PersistentVolumeClaims = []*v1.PersistentVolumeClaim{}
	}

	rawObjects.CSIStorageCapacities, err = dataLister.ListCSIStorageCapacities()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw CSI storage capacities: %v", err)
		rawObjects.CSIStorageCapacities = []*storage.CSIStorageCapacity{}
	}

	rawObjects.StorageClasses, err = dataLister.ListStorageClasses()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw storage classes: %v", err)
		rawObjects.StorageClasses = []*storage.StorageClass{}
	}

	rawObjects.CSIDrivers, err = dataLister.ListCSIDrivers()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw CSI drivers: %v", err)
		rawObjects.CSIDrivers = []*storage.CSIDriver{}
	}

	fwork := sp.session.InternalK8sPlugins().FrameworkHandle

	rawObjects.ResourceClaims, err = fwork.SharedDRAManager().ResourceClaims().List()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw resource claims: %v", err)
		rawObjects.ResourceClaims = []*resourceapi.ResourceClaim{}
	}

	rawObjects.ResourceSlices, err = fwork.SharedDRAManager().ResourceSlices().ListWithDeviceTaintRules()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw resource slices: %v", err)
		rawObjects.ResourceSlices = []*resourceapi.ResourceSlice{}
	}

	rawObjects.DeviceClasses, err = fwork.SharedDRAManager().DeviceClasses().List()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw device classes: %v", err)
		rawObjects.DeviceClasses = []*resourceapi.DeviceClass{}
	}

	snapshotAndConfig := Snapshot{
		Config:          sp.session.Config,
		SchedulerParams: &sp.session.SchedulerParams,
		RawObjects:      rawObjects,
	}
	jsonBytes, err := json.Marshal(snapshotAndConfig)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Disposition", "attachment; filename=snapshot.zip")
	writer.Header().Set("Content-Type", "application/zip")

	zipWriter := zip.NewWriter(writer)
	jsonWriter, err := zipWriter.Create(SnapshotFileName)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = io.Copy(jsonWriter, strings.NewReader(string(jsonBytes)))
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	err = zipWriter.Close()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}

func New(arguments map[string]string) framework.Plugin {
	return &snapshotPlugin{}
}

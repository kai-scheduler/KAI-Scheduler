// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	v14 "k8s.io/api/scheduling/v1"
	storage "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	version "k8s.io/apimachinery/pkg/version"
	discovery "k8s.io/client-go/discovery"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	enginev2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	SnapshotFileName = "snapshot.json"
)

// RawKubernetesObjects contains the raw Kubernetes objects from the cluster
type RawKubernetesObjects struct {
	Pods                   []*v1.Pod                         `json:"pods"`
	Nodes                  []*v1.Node                        `json:"nodes"`
	Queues                 []*enginev2.Queue                 `json:"queues"`
	PodGroups              []*enginev2alpha2.PodGroup        `json:"podGroups"`
	BindRequests           []*schedulingv1alpha2.BindRequest `json:"bindRequests"`
	PriorityClasses        []*v14.PriorityClass              `json:"priorityClasses"`
	ConfigMaps             []*v1.ConfigMap                   `json:"configMaps"`
	PersistentVolumeClaims []*v1.PersistentVolumeClaim       `json:"persistentVolumeClaims"`
	CSIStorageCapacities   []*storage.CSIStorageCapacity     `json:"csiStorageCapacities"`
	StorageClasses         []*storage.StorageClass           `json:"storageClasses"`
	CSIDrivers             []*storage.CSIDriver              `json:"csiDrivers"`
	ResourceClaims         []*resourceapi.ResourceClaim      `json:"resourceClaims"`
	ResourceSlices         []*resourceapi.ResourceSlice      `json:"resourceSlices"`
	DeviceClasses          []*resourceapi.DeviceClass        `json:"deviceClasses"`
	Topologies             []*kaiv1alpha1.Topology           `json:"topologies"`
}

type DiscoverySnapshot struct {
	ServerVersion *version.Info             `json:"serverVersion"`
	Resources     []*metav1.APIResourceList `json:"resources"`
}

type Snapshot struct {
	Config          *conf.SchedulerConfiguration `json:"config"`
	SchedulerParams *conf.SchedulerParams        `json:"schedulerParams"`
	RawObjects      *RawKubernetesObjects        `json:"rawObjects"`
	Discovery       *DiscoverySnapshot           `json:"discovery,omitempty"`
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

	rawObjects.Topologies, err = dataLister.ListTopologies()
	if err != nil {
		log.InfraLogger.Errorf("Error getting raw topologies: %v", err)
		rawObjects.Topologies = []*kaiv1alpha1.Topology{}
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

	discoverySnapshot := &DiscoverySnapshot{}
	discoveryClient := sp.session.Cache.KubeClient().Discovery()
	discoverySnapshot.ServerVersion, err = getServerVersion(request.Context(), discoveryClient)
	if err != nil {
		log.InfraLogger.V(2).Warnf("Failed to snapshot server version: %v", err)
		discoverySnapshot.ServerVersion = nil
	}

	_, discoverySnapshot.Resources, err = discoveryClient.ServerGroupsAndResources()
	if err != nil {
		log.InfraLogger.V(2).Warnf("Failed to snapshot server resources: %v", err)
		discoverySnapshot.Resources = nil
	}

	snapshotAndConfig := Snapshot{
		Config:          sp.session.Config,
		SchedulerParams: &sp.session.SchedulerParams,
		RawObjects:      rawObjects,
		Discovery:       discoverySnapshot,
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

func New(_ framework.PluginArguments) framework.Plugin {
	return &snapshotPlugin{}
}

func getServerVersion(ctx context.Context, discoveryClient discovery.DiscoveryInterface) (*version.Info, error) {
	serverVersion, err := discoveryClient.ServerVersion()
	if err == nil {
		return serverVersion, nil
	}
	if !apierrors.IsNotAcceptable(err) {
		return nil, err
	}

	// Fallback for clusters where /version rejects the negotiated content-type (e.g. protobuf).
	versionResponse, fallbackErr := discoveryClient.RESTClient().
		Get().
		AbsPath("/version").
		SetHeader("Accept", "application/json").
		DoRaw(ctx)
	if fallbackErr != nil {
		return nil, fallbackErr
	}

	fallbackServerVersion := &version.Info{}
	if unmarshalErr := json.Unmarshal(versionResponse, fallbackServerVersion); unmarshalErr != nil {
		return nil, unmarshalErr
	}

	return fallbackServerVersion, nil
}

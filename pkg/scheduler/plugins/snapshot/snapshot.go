// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"net/http"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	v14 "k8s.io/api/scheduling/v1"
	storage "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	version "k8s.io/apimachinery/pkg/version"
	discovery "k8s.io/client-go/discovery"

	kaiv1alpha1 "github.com/kai-scheduler/api/kai/v1alpha1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
	enginev2 "github.com/kai-scheduler/api/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

const (
	SnapshotFileName = "snapshot.json"
)

// RawKubernetesObjects contains the raw Kubernetes objects from the cluster
type RawKubernetesObjects struct {
	Pods                   []*v1.Pod                           `json:"pods"`
	Nodes                  []*v1.Node                          `json:"nodes"`
	Queues                 []*enginev2.Queue                   `json:"queues"`
	PodGroups              []*enginev2alpha2.PodGroup          `json:"podGroups"`
	BindRequests           []*schedulingv1alpha2.BindRequest   `json:"bindRequests"`
	PriorityClasses        []*v14.PriorityClass                `json:"priorityClasses"`
	ConfigMaps             []*v1.ConfigMap                     `json:"configMaps"`
	PersistentVolumes      []*v1.PersistentVolume              `json:"persistentVolumes"`
	PersistentVolumeClaims []*v1.PersistentVolumeClaim         `json:"persistentVolumeClaims"`
	CSIStorageCapacities   []*storage.CSIStorageCapacity       `json:"csiStorageCapacities"`
	StorageClasses         []*storage.StorageClass             `json:"storageClasses"`
	CSIDrivers             []*storage.CSIDriver                `json:"csiDrivers"`
	ResourceClaims         []*resourceapi.ResourceClaim        `json:"resourceClaims"`
	ResourceSlices         []*resourceapi.ResourceSlice        `json:"resourceSlices"`
	DeviceClasses          []*resourceapi.DeviceClass          `json:"deviceClasses"`
	Topologies             []*kaiv1alpha1.Topology             `json:"topologies"`
	NodeResourceTopologies []*nrtv1alpha2.NodeResourceTopology `json:"nodeResourceTopologies"`
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
	config  *conf.SchedulerConfiguration
}

type jsonStream struct {
	writer  io.Writer
	encoder *json.Encoder
}

type jsonObjectWriter struct {
	stream     *jsonStream
	wroteField bool
}

type jsonFieldWriter func(*jsonObjectWriter) error

func (sp *snapshotPlugin) Name() string {
	return "snapshot"
}

func (sp *snapshotPlugin) OnSessionOpen(ssn *framework.Session) {
	sp.session = ssn
	sp.config = ssn.Config
	log.InfraLogger.V(3).Info("Snapshot plugin registering get-snapshot")
	ssn.AddHttpHandler("/get-snapshot", sp.serveSnapshot)
}

func (sp *snapshotPlugin) OnSessionClose(ssn *framework.Session) {}

func (sp *snapshotPlugin) serveSnapshot(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Disposition", "attachment; filename=snapshot.zip")
	writer.Header().Set("Content-Type", "application/zip")

	zipWriter := zip.NewWriter(writer)
	defer func() {
		if err := zipWriter.Close(); err != nil {
			log.InfraLogger.Errorf("Error closing snapshot zip: %v", err)
		}
	}()

	jsonWriter, err := zipWriter.Create(SnapshotFileName)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := sp.writeSnapshot(request.Context(), jsonWriter); err != nil {
		log.InfraLogger.Errorf("Error writing snapshot: %v", err)
	}
}

func (sp *snapshotPlugin) writeSnapshot(ctx context.Context, writer io.Writer) error {
	stream := newJSONStream(writer)

	return stream.writeObject(func(object *jsonObjectWriter) error {
		return writeFields(
			object,
			valueField("config", sp.config),
			valueField("schedulerParams", &sp.session.SchedulerParams),
			streamedField("rawObjects", sp.writeRawObjects),
			streamedField("discovery", func(stream *jsonStream) error {
				return sp.writeDiscoverySnapshot(ctx, stream)
			}),
		)
	})
}

func (sp *snapshotPlugin) writeRawObjects(stream *jsonStream) error {
	raw := CollectRawObjects(sp.session.Cache.GetDataLister())
	if raw == nil {
		raw = &RawKubernetesObjects{}
	}

	return stream.writeObject(func(object *jsonObjectWriter) error {
		return writeFields(
			object,
			valueField("pods", raw.Pods),
			valueField("nodes", raw.Nodes),
			valueField("queues", raw.Queues),
			valueField("podGroups", raw.PodGroups),
			valueField("bindRequests", raw.BindRequests),
			valueField("priorityClasses", raw.PriorityClasses),
			valueField("configMaps", raw.ConfigMaps),
			valueField("persistentVolumes", raw.PersistentVolumes),
			valueField("persistentVolumeClaims", raw.PersistentVolumeClaims),
			valueField("csiStorageCapacities", raw.CSIStorageCapacities),
			valueField("storageClasses", raw.StorageClasses),
			valueField("csiDrivers", raw.CSIDrivers),
			valueField("resourceClaims", raw.ResourceClaims),
			valueField("resourceSlices", raw.ResourceSlices),
			valueField("deviceClasses", raw.DeviceClasses),
			valueField("topologies", raw.Topologies),
			valueField("nodeResourceTopologies", raw.NodeResourceTopologies),
		)
	})
}

func (sp *snapshotPlugin) writeDiscoverySnapshot(ctx context.Context, stream *jsonStream) error {
	discoverySnapshot := sp.getDiscoverySnapshot(ctx)

	return stream.writeObject(func(object *jsonObjectWriter) error {
		return writeFields(
			object,
			valueField("serverVersion", discoverySnapshot.ServerVersion),
			sliceField("resources", discoverySnapshot.Resources),
		)
	})
}

func (sp *snapshotPlugin) getDiscoverySnapshot(ctx context.Context) *DiscoverySnapshot {
	return CollectDiscovery(ctx, sp.session.Cache.KubeClient().Discovery())
}

func newJSONStream(writer io.Writer) *jsonStream {
	return &jsonStream{
		writer:  writer,
		encoder: json.NewEncoder(writer),
	}
}

func (js *jsonStream) writeRaw(value string) error {
	_, err := io.WriteString(js.writer, value)
	return err
}

func (js *jsonStream) writeValue(value any) error {
	return js.encoder.Encode(value)
}

func (js *jsonStream) writeObject(fn func(*jsonObjectWriter) error) error {
	object := &jsonObjectWriter{stream: js}
	if err := js.writeRaw("{"); err != nil {
		return err
	}
	if err := fn(object); err != nil {
		return err
	}
	return js.writeRaw("}")
}

func (object *jsonObjectWriter) writeFieldName(fieldName string) error {
	if object.wroteField {
		if err := object.stream.writeRaw(","); err != nil {
			return err
		}
	}
	object.wroteField = true

	quotedFieldName, err := json.Marshal(fieldName)
	if err != nil {
		return err
	}
	if _, err := object.stream.writer.Write(quotedFieldName); err != nil {
		return err
	}
	return object.stream.writeRaw(":")
}

func (object *jsonObjectWriter) writeField(fieldName string, value any) error {
	if err := object.writeFieldName(fieldName); err != nil {
		return err
	}
	return object.stream.writeValue(value)
}

func writeFields(object *jsonObjectWriter, fields ...jsonFieldWriter) error {
	for _, field := range fields {
		if err := field(object); err != nil {
			return err
		}
	}
	return nil
}

func valueField(fieldName string, value any) jsonFieldWriter {
	return func(object *jsonObjectWriter) error {
		return object.writeField(fieldName, value)
	}
}

func streamedField(fieldName string, writeValue func(*jsonStream) error) jsonFieldWriter {
	return func(object *jsonObjectWriter) error {
		if err := object.writeFieldName(fieldName); err != nil {
			return err
		}
		return writeValue(object.stream)
	}
}

func sliceField[T any](fieldName string, values []T) jsonFieldWriter {
	return func(object *jsonObjectWriter) error {
		return writeSliceField(object, fieldName, values)
	}
}

func writeSliceField[T any](object *jsonObjectWriter, fieldName string, values []T) error {
	if err := object.writeFieldName(fieldName); err != nil {
		return err
	}
	return writeSlice(object.stream, values)
}

func writeSlice[T any](stream *jsonStream, values []T) error {
	if values == nil {
		return stream.writeValue(values)
	}

	if err := stream.writeRaw("["); err != nil {
		return err
	}
	for index, value := range values {
		if index > 0 {
			if err := stream.writeRaw(","); err != nil {
				return err
			}
		}
		if err := stream.writeValue(value); err != nil {
			return err
		}
	}
	return stream.writeRaw("]")
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

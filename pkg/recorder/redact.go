// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"encoding/json"
	"strings"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	commonconstants "github.com/kai-scheduler/api/constants"
	kaiv1alpha1 "github.com/kai-scheduler/api/kai/v1alpha1"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
	enginev2 "github.com/kai-scheduler/api/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

// RedactEnvelope rewrites identifying strings in a record. Fields that are not on the
// scheduling allowlist are dropped. Custom label keys and values are hashed so selectors
// still match; well-known domains are left as-is.
func RedactEnvelope(env *Envelope, h Hasher) error {
	if env == nil || len(env.Record) == 0 {
		return nil
	}
	var redacted any
	switch env.Kind {
	case KindState:
		var record StateRecord
		if err := json.Unmarshal(env.Record, &record); err != nil {
			return err
		}
		redacted = redactState(record, h)
	case KindDecision:
		var record DecisionRecord
		if err := json.Unmarshal(env.Record, &record); err != nil {
			return err
		}
		redacted = redactDecision(record, h)
	case KindSessionEnd:
		var record SessionEndRecord
		if err := json.Unmarshal(env.Record, &record); err != nil {
			return err
		}
		redacted = redactSessionEnd(record, h)
	case KindLifecycle:
		var record LifecycleRecord
		if err := json.Unmarshal(env.Record, &record); err != nil {
			return err
		}
		redacted = redactLifecycle(record, h)
	default:
		env.Record = nil
		return nil
	}
	raw, err := json.Marshal(redacted)
	if err != nil {
		return err
	}
	env.Record = raw
	return nil
}

func redactState(in StateRecord, h Hasher) StateRecord {
	in.ClusterName = h.Hash(in.ClusterName)
	in.ShardName = h.Hash(in.ShardName)
	in.Config = redactConfig(in.Config)
	in.SchedulerParams = cloneParams(in.SchedulerParams)
	if in.RawObjects != nil {
		in.RawObjects = redactRaw(in.RawObjects, h)
	}
	return in
}

func redactDecision(in DecisionRecord, h Hasher) DecisionRecord {
	ops := make([]DecisionOp, len(in.Ops))
	for i, op := range in.Ops {
		ops[i] = DecisionOp{
			Kind:         op.Kind,
			UID:          h.Hash(op.UID),
			Namespace:    h.Hash(op.Namespace),
			Name:         h.Hash(op.Name),
			Node:         h.Hash(op.Node),
			NotAttempted: op.NotAttempted,
			Eviction:     op.Eviction,
		}
		if !FreeTextIsName(op.Error) {
			ops[i].Error = op.Error
		}
		if op.Eviction.Preemptor != nil {
			preemptor := *op.Eviction.Preemptor
			preemptor.Namespace = h.Hash(preemptor.Namespace)
			preemptor.Name = h.Hash(preemptor.Name)
			ops[i].Eviction.Preemptor = &preemptor
		}
	}
	in.Ops = ops
	return in
}

func redactSessionEnd(in SessionEndRecord, h Hasher) SessionEndRecord {
	for i := range in.FailReasons {
		in.FailReasons[i].PodUID = h.Hash(in.FailReasons[i].PodUID)
		in.FailReasons[i].Namespace = h.Hash(in.FailReasons[i].Namespace)
		in.FailReasons[i].Name = h.Hash(in.FailReasons[i].Name)
	}
	return in
}

func redactLifecycle(in LifecycleRecord, h Hasher) LifecycleRecord {
	in.Namespace = h.Hash(in.Namespace)
	in.Name = h.Hash(in.Name)
	in.UID = h.Hash(in.UID)
	in.NodeName = h.Hash(in.NodeName)
	in.GPUGroups = h.HashEach(in.GPUGroups)
	if FreeTextIsName(in.Reason) {
		in.Reason = ""
	}
	return in
}

func redactRaw(in *snapshot.RawKubernetesObjects, h Hasher) *snapshot.RawKubernetesObjects {
	out := &snapshot.RawKubernetesObjects{
		Pods:      mapSlice(in.Pods, func(pod *v1.Pod) *v1.Pod { return redactPod(pod, h) }),
		Nodes:     mapSlice(in.Nodes, func(node *v1.Node) *v1.Node { return redactNode(node, h) }),
		Queues:    mapSlice(in.Queues, func(queue *enginev2.Queue) *enginev2.Queue { return redactQueue(queue, h) }),
		PodGroups: mapSlice(in.PodGroups, func(pg *enginev2alpha2.PodGroup) *enginev2alpha2.PodGroup { return redactPodGroup(pg, h) }),
		BindRequests: mapSlice(in.BindRequests, func(br *schedulingv1alpha2.BindRequest) *schedulingv1alpha2.BindRequest {
			return redactBindRequest(br, h)
		}),
		PriorityClasses: mapSlice(in.PriorityClasses, func(pc *schedulingv1.PriorityClass) *schedulingv1.PriorityClass {
			return redactPriorityClass(pc, h)
		}),
		ConfigMaps:             mapSlice(in.ConfigMaps, func(cm *v1.ConfigMap) *v1.ConfigMap { return redactConfigMap(cm, h) }),
		PersistentVolumes:      mapSlice(in.PersistentVolumes, func(pv *v1.PersistentVolume) *v1.PersistentVolume { return redactPV(pv, h) }),
		PersistentVolumeClaims: mapSlice(in.PersistentVolumeClaims, func(pvc *v1.PersistentVolumeClaim) *v1.PersistentVolumeClaim { return redactPVC(pvc, h) }),
		CSIStorageCapacities: mapSlice(in.CSIStorageCapacities, func(cap *storagev1.CSIStorageCapacity) *storagev1.CSIStorageCapacity {
			return redactCSICapacity(cap, h)
		}),
		StorageClasses: mapSlice(in.StorageClasses, func(sc *storagev1.StorageClass) *storagev1.StorageClass { return redactStorageClass(sc, h) }),
		CSIDrivers:     mapSlice(in.CSIDrivers, func(d *storagev1.CSIDriver) *storagev1.CSIDriver { return redactCSIDriver(d, h) }),
		ResourceClaims: mapSlice(in.ResourceClaims, func(c *resourceapi.ResourceClaim) *resourceapi.ResourceClaim {
			return hashNodeNames(redactClaimMeta(c, h), h)
		}),
		ResourceSlices: mapSlice(in.ResourceSlices, func(s *resourceapi.ResourceSlice) *resourceapi.ResourceSlice {
			return hashNodeNames(redactSliceMeta(s, h), h)
		}),
		DeviceClasses: mapSlice(in.DeviceClasses, func(d *resourceapi.DeviceClass) *resourceapi.DeviceClass { return redactDeviceClass(d, h) }),
		Topologies:    mapSlice(in.Topologies, func(t *kaiv1alpha1.Topology) *kaiv1alpha1.Topology { return redactTopology(t, h) }),
		NodeResourceTopologies: mapSlice(in.NodeResourceTopologies, func(n *nrtv1alpha2.NodeResourceTopology) *nrtv1alpha2.NodeResourceTopology {
			return hashNodeNames(redactNRTMeta(n, h), h)
		}),
	}
	return out
}

func redactPod(in *v1.Pod, h Hasher) *v1.Pod {
	if in == nil {
		return nil
	}
	out := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            h.Hash(in.Name),
			Namespace:       h.Hash(in.Namespace),
			UID:             types.UID(h.Hash(string(in.UID))),
			Labels:          redactLabels(in.Labels, h),
			Annotations:     redactPodAnnotations(in.Annotations, h),
			OwnerReferences: redactOwners(in.OwnerReferences, h),
		},
		Spec: v1.PodSpec{
			SchedulerName:             in.Spec.SchedulerName,
			Priority:                  in.Spec.Priority,
			PriorityClassName:         h.Hash(in.Spec.PriorityClassName),
			NodeName:                  h.Hash(in.Spec.NodeName),
			NodeSelector:              redactSelector(in.Spec.NodeSelector, h),
			Affinity:                  redactAffinity(in.Spec.Affinity, h),
			Tolerations:               redactTolerations(in.Spec.Tolerations, h),
			TopologySpreadConstraints: redactTopologySpread(in.Spec.TopologySpreadConstraints, h),
			Containers:                redactContainers(in.Spec.Containers),
			InitContainers:            redactContainers(in.Spec.InitContainers),
			Volumes:                   redactVolumes(in.Spec.Volumes, h),
			Overhead:                  in.Spec.Overhead.DeepCopy(),
		},
		Status: v1.PodStatus{
			Phase:             in.Status.Phase,
			StartTime:         in.Status.StartTime.DeepCopy(),
			Conditions:        redactPodConditions(in.Status.Conditions),
			NominatedNodeName: h.Hash(in.Status.NominatedNodeName),
		},
	}
	return out
}

func redactNode(in *v1.Node, h Hasher) *v1.Node {
	if in == nil {
		return nil
	}
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   h.Hash(in.Name),
			Labels: redactLabels(in.Labels, h),
		},
		Spec: v1.NodeSpec{
			Unschedulable: in.Spec.Unschedulable,
			Taints:        redactTaints(in.Spec.Taints, h),
		},
		Status: v1.NodeStatus{
			Capacity:    in.Status.Capacity.DeepCopy(),
			Allocatable: in.Status.Allocatable.DeepCopy(),
			Conditions:  redactNodeConditions(in.Status.Conditions),
		},
	}
}

func redactQueue(in *enginev2.Queue, h Hasher) *enginev2.Queue {
	if in == nil {
		return nil
	}
	spec := in.DeepCopy().Spec
	spec.DisplayName = ""
	spec.ParentQueue = h.Hash(spec.ParentQueue)
	return &enginev2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      h.Hash(in.Name),
			Namespace: h.Hash(in.Namespace),
			Labels:    redactLabels(in.Labels, h),
		},
		Spec: spec,
	}
}

func redactPodGroup(in *enginev2alpha2.PodGroup, h Hasher) *enginev2alpha2.PodGroup {
	if in == nil {
		return nil
	}
	spec := in.DeepCopy().Spec
	spec.Queue = h.Hash(spec.Queue)
	spec.PriorityClassName = h.Hash(spec.PriorityClassName)
	spec.TopologyConstraint.Topology = h.Hash(spec.TopologyConstraint.Topology)
	spec.SubGroups = redactSubGroups(spec.SubGroups, h)
	return &enginev2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            h.Hash(in.Name),
			Namespace:       h.Hash(in.Namespace),
			UID:             types.UID(h.Hash(string(in.UID))),
			Labels:          redactLabels(in.Labels, h),
			Annotations:     redactPodGroupAnnotations(in.Annotations),
			OwnerReferences: redactOwners(in.OwnerReferences, h),
		},
		Spec:   spec,
		Status: redactPodGroupStatus(in.Status, h),
	}
}

func redactSubGroups(in []enginev2alpha2.SubGroup, h Hasher) []enginev2alpha2.SubGroup {
	if len(in) == 0 {
		return nil
	}
	out := make([]enginev2alpha2.SubGroup, len(in))
	for i, group := range in {
		out[i] = group
		out[i].Name = h.Hash(group.Name)
		if group.Parent != nil {
			parent := h.Hash(*group.Parent)
			out[i].Parent = &parent
		}
		if group.TopologyConstraint != nil {
			constraint := *group.TopologyConstraint
			constraint.Topology = h.Hash(constraint.Topology)
			out[i].TopologyConstraint = &constraint
		}
	}
	return out
}

func redactPodGroupStatus(in enginev2alpha2.PodGroupStatus, h Hasher) enginev2alpha2.PodGroupStatus {
	copied := in.DeepCopy()
	if copied == nil {
		return enginev2alpha2.PodGroupStatus{}
	}
	out := copied
	for i := range out.Conditions {
		out.Conditions[i].Message = ""
		if FreeTextIsName(out.Conditions[i].Reason) {
			out.Conditions[i].Reason = ""
		}
	}
	for i := range out.SchedulingConditions {
		out.SchedulingConditions[i].Message = ""
		for j := range out.SchedulingConditions[i].Reasons {
			out.SchedulingConditions[i].Reasons[j].Message = ""
			details := out.SchedulingConditions[i].Reasons[j].Details
			if details != nil && details.QueueDetails != nil {
				details.QueueDetails.Name = h.Hash(details.QueueDetails.Name)
			}
		}
	}
	if out.SchedulingState != nil {
		out.SchedulingState.CorePods = h.HashEach(out.SchedulingState.CorePods)
	}
	return *out
}

func redactBindRequest(in *schedulingv1alpha2.BindRequest, h Hasher) *schedulingv1alpha2.BindRequest {
	if in == nil {
		return nil
	}
	groups := in.Spec.SelectedFractionalGpuGroupsOrDefault()
	for i := range groups {
		groups[i].ID = h.Hash(groups[i].ID)
	}
	reason := in.Status.Reason
	if FreeTextIsName(reason) {
		reason = ""
	}
	return &schedulingv1alpha2.BindRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      h.Hash(in.Name),
			Namespace: h.Hash(in.Namespace),
			UID:       types.UID(h.Hash(string(in.UID))),
			Labels:    redactLabels(in.Labels, h),
		},
		Spec: schedulingv1alpha2.BindRequestSpec{
			PodName:                     h.Hash(in.Spec.PodName),
			SelectedNode:                h.Hash(in.Spec.SelectedNode),
			ReceivedResourceType:        in.Spec.ReceivedResourceType,
			ReceivedGPU:                 in.Spec.ReceivedGPU.DeepCopy(),
			SelectedFractionalGpuGroups: groups,
			BackoffLimit:                in.Spec.BackoffLimit,
			PredictedNUMAZones:          append([]schedulingv1alpha2.NUMAZonePlacement{}, in.Spec.PredictedNUMAZones...),
			PredictedNUMAMemoryGroups:   append([]schedulingv1alpha2.NUMAMemoryGroupPlacement{}, in.Spec.PredictedNUMAMemoryGroups...),
		},
		Status: schedulingv1alpha2.BindRequestStatus{
			Phase:          in.Status.Phase,
			Reason:         reason,
			FailedAttempts: in.Status.FailedAttempts,
		},
	}
}

func redactPriorityClass(in *schedulingv1.PriorityClass, h Hasher) *schedulingv1.PriorityClass {
	if in == nil {
		return nil
	}
	return &schedulingv1.PriorityClass{
		ObjectMeta:       metav1.ObjectMeta{Name: h.Hash(in.Name)},
		Value:            in.Value,
		GlobalDefault:    in.GlobalDefault,
		PreemptionPolicy: in.PreemptionPolicy,
	}
}

func redactConfigMap(in *v1.ConfigMap, h Hasher) *v1.ConfigMap {
	if in == nil {
		return nil
	}
	return &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      h.Hash(in.Name),
		Namespace: h.Hash(in.Namespace),
	}}
}

func redactPV(in *v1.PersistentVolume, h Hasher) *v1.PersistentVolume {
	if in == nil {
		return nil
	}
	out := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: h.Hash(in.Name)},
		Spec: v1.PersistentVolumeSpec{
			Capacity:                      in.Spec.Capacity.DeepCopy(),
			AccessModes:                   append([]v1.PersistentVolumeAccessMode{}, in.Spec.AccessModes...),
			StorageClassName:              h.Hash(in.Spec.StorageClassName),
			VolumeMode:                    in.Spec.VolumeMode,
			PersistentVolumeReclaimPolicy: in.Spec.PersistentVolumeReclaimPolicy,
		},
	}
	if in.Spec.ClaimRef != nil {
		out.Spec.ClaimRef = &v1.ObjectReference{
			Kind:      in.Spec.ClaimRef.Kind,
			Namespace: h.Hash(in.Spec.ClaimRef.Namespace),
			Name:      h.Hash(in.Spec.ClaimRef.Name),
		}
	}
	if in.Spec.CSI != nil {
		out.Spec.CSI = &v1.CSIPersistentVolumeSource{
			Driver:       in.Spec.CSI.Driver,
			VolumeHandle: h.Hash(in.Spec.CSI.VolumeHandle),
		}
	}
	return out
}

func redactPVC(in *v1.PersistentVolumeClaim, h Hasher) *v1.PersistentVolumeClaim {
	if in == nil {
		return nil
	}
	var storageClass *string
	if in.Spec.StorageClassName != nil {
		hashed := h.Hash(*in.Spec.StorageClassName)
		storageClass = &hashed
	}
	return &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      h.Hash(in.Name),
			Namespace: h.Hash(in.Namespace),
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes:      append([]v1.PersistentVolumeAccessMode{}, in.Spec.AccessModes...),
			Resources:        in.Spec.Resources,
			StorageClassName: storageClass,
			VolumeMode:       in.Spec.VolumeMode,
			VolumeName:       h.Hash(in.Spec.VolumeName),
		},
		Status: v1.PersistentVolumeClaimStatus{
			Phase:       in.Status.Phase,
			Capacity:    in.Status.Capacity.DeepCopy(),
			AccessModes: append([]v1.PersistentVolumeAccessMode{}, in.Status.AccessModes...),
		},
	}
}

func redactCSICapacity(in *storagev1.CSIStorageCapacity, h Hasher) *storagev1.CSIStorageCapacity {
	if in == nil {
		return nil
	}
	out := &storagev1.CSIStorageCapacity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      h.Hash(in.Name),
			Namespace: h.Hash(in.Namespace),
		},
		NodeTopology:     redactLabelSelector(in.NodeTopology, h),
		StorageClassName: h.Hash(in.StorageClassName),
	}
	if in.Capacity != nil {
		copied := in.Capacity.DeepCopy()
		out.Capacity = &copied
	}
	if in.MaximumVolumeSize != nil {
		copied := in.MaximumVolumeSize.DeepCopy()
		out.MaximumVolumeSize = &copied
	}
	return out
}

func redactStorageClass(in *storagev1.StorageClass, h Hasher) *storagev1.StorageClass {
	if in == nil {
		return nil
	}
	return &storagev1.StorageClass{
		ObjectMeta:        metav1.ObjectMeta{Name: h.Hash(in.Name)},
		Provisioner:       in.Provisioner,
		ReclaimPolicy:     in.ReclaimPolicy,
		VolumeBindingMode: in.VolumeBindingMode,
		AllowedTopologies: redactAllowedTopologies(in.AllowedTopologies, h),
	}
}

func redactCSIDriver(in *storagev1.CSIDriver, h Hasher) *storagev1.CSIDriver {
	if in == nil {
		return nil
	}
	return &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: h.Hash(in.Name)},
		Spec:       *in.Spec.DeepCopy(),
	}
}

func redactClaimMeta(in *resourceapi.ResourceClaim, h Hasher) *resourceapi.ResourceClaim {
	if in == nil {
		return nil
	}
	out := in.DeepCopy()
	out.ObjectMeta = metav1.ObjectMeta{
		Name:      h.Hash(in.Name),
		Namespace: h.Hash(in.Namespace),
		UID:       types.UID(h.Hash(string(in.UID))),
		Labels:    redactLabels(in.Labels, h),
	}
	return out
}

func redactSliceMeta(in *resourceapi.ResourceSlice, h Hasher) *resourceapi.ResourceSlice {
	if in == nil {
		return nil
	}
	out := in.DeepCopy()
	out.ObjectMeta = metav1.ObjectMeta{
		Name:   h.Hash(in.Name),
		UID:    types.UID(h.Hash(string(in.UID))),
		Labels: redactLabels(in.Labels, h),
	}
	hashDeniedDRAAttributes(out, h)
	return out
}

// draAttributeValueDenylist names DRA device attributes whose string values are hashed.
// It is empty, so attribute strings stay readable.
var draAttributeValueDenylist = map[string]struct{}{}

func hashDeniedDRAAttributes(slice *resourceapi.ResourceSlice, h Hasher) {
	if slice == nil || len(draAttributeValueDenylist) == 0 {
		return
	}
	for i := range slice.Spec.Devices {
		attrs := slice.Spec.Devices[i].Attributes
		for name, attr := range attrs {
			if _, deny := draAttributeValueDenylist[string(name)]; !deny || attr.StringValue == nil {
				continue
			}
			hashed := h.Hash(*attr.StringValue)
			attr.StringValue = &hashed
			attrs[name] = attr
		}
	}
}

func redactDeviceClass(in *resourceapi.DeviceClass, h Hasher) *resourceapi.DeviceClass {
	if in == nil {
		return nil
	}
	out := in.DeepCopy()
	out.ObjectMeta = metav1.ObjectMeta{Name: h.Hash(in.Name)}
	return out
}

func redactTopology(in *kaiv1alpha1.Topology, h Hasher) *kaiv1alpha1.Topology {
	if in == nil {
		return nil
	}
	out := in.DeepCopy()
	out.ObjectMeta = metav1.ObjectMeta{
		Name:   h.Hash(in.Name),
		Labels: redactLabels(in.Labels, h),
	}
	for i := range out.Spec.Levels {
		out.Spec.Levels[i].NodeLabel = h.LabelKey(out.Spec.Levels[i].NodeLabel)
	}
	return out
}

func redactNRTMeta(in *nrtv1alpha2.NodeResourceTopology, h Hasher) *nrtv1alpha2.NodeResourceTopology {
	if in == nil {
		return nil
	}
	out := in.DeepCopy()
	out.ObjectMeta = metav1.ObjectMeta{
		Name:   h.Hash(in.Name),
		Labels: redactLabels(in.Labels, h),
	}
	return out
}

func redactConfig(in *conf.SchedulerConfiguration) *conf.SchedulerConfiguration {
	if in == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out conf.SchedulerConfiguration
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	out.UsageDBConfig = nil
	for i := range out.Tiers {
		for j := range out.Tiers[i].Plugins {
			out.Tiers[i].Plugins[j].Arguments = scrubArguments(out.Tiers[i].Plugins[j].Arguments)
		}
	}
	return &out
}

func cloneParams(in *conf.SchedulerParams) *conf.SchedulerParams {
	if in == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out conf.SchedulerParams
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return &out
}

func scrubArguments(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		if credentialLike(key) || credentialLike(value) {
			continue
		}
		out[key] = value
	}
	return out
}

func credentialLike(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "://") ||
		strings.Contains(lower, "password") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "token")
}

func redactLabels(in map[string]string, h Hasher) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[h.LabelKey(key)] = h.LabelValue(key, value)
	}
	return out
}

func redactSelector(in map[string]string, h Hasher) map[string]string {
	return redactLabels(in, h)
}

func redactPodAnnotations(in map[string]string, h Hasher) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range in {
		switch {
		case key == commonconstants.PodGroupAnnotationForPod:
			out[key] = h.Hash(value)
		case gpuAnnotation(key):
			out[key] = value
		case WellKnownKey(key) && !isIdentityKey(key):
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func redactPodGroupAnnotations(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	keep := []string{
		commonconstants.StalePodgroupTimeStamp,
		commonconstants.LastStartTimeStamp,
		commonconstants.LastEvictionTimeStamp,
	}
	out := map[string]string{}
	for _, key := range keep {
		if value, found := in[key]; found {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func gpuAnnotation(key string) bool {
	switch key {
	case commonconstants.GpuFraction, commonconstants.GpuFractionLimit, commonconstants.GpuMemory,
		commonconstants.GpuMemoryLimit, commonconstants.GpuFractionsNumDevices, commonconstants.ReceivedResourceType,
		commonconstants.GpuFractionContainerName:
		return true
	}
	return strings.HasPrefix(key, commonconstants.NvFractionsAnnotationPrefix) ||
		strings.HasPrefix(key, commonconstants.KaiFractionContainerAnnotationPrefix)
}

func redactOwners(in []metav1.OwnerReference, h Hasher) []metav1.OwnerReference {
	if len(in) == 0 {
		return nil
	}
	out := make([]metav1.OwnerReference, len(in))
	for i, ref := range in {
		out[i] = metav1.OwnerReference{
			APIVersion:         ref.APIVersion,
			Kind:               ref.Kind,
			Name:               h.Hash(ref.Name),
			UID:                types.UID(h.Hash(string(ref.UID))),
			Controller:         ref.Controller,
			BlockOwnerDeletion: ref.BlockOwnerDeletion,
		}
	}
	return out
}

func redactContainers(in []v1.Container) []v1.Container {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.Container, 0, len(in))
	for _, container := range in {
		out = append(out, v1.Container{
			Resources: v1.ResourceRequirements{
				Requests: container.Resources.Requests.DeepCopy(),
				Limits:   container.Resources.Limits.DeepCopy(),
			},
		})
	}
	return out
}

func redactVolumes(in []v1.Volume, h Hasher) []v1.Volume {
	var out []v1.Volume
	for _, volume := range in {
		switch {
		case volume.PersistentVolumeClaim != nil:
			out = append(out, v1.Volume{
				Name: h.Hash(volume.Name),
				VolumeSource: v1.VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
						ClaimName: h.Hash(volume.PersistentVolumeClaim.ClaimName),
						ReadOnly:  volume.PersistentVolumeClaim.ReadOnly,
					},
				},
			})
		case volume.ConfigMap != nil:
			out = append(out, v1.Volume{
				Name: h.Hash(volume.Name),
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{Name: h.Hash(volume.ConfigMap.Name)},
					},
				},
			})
		}
	}
	return out
}

func redactTolerations(in []v1.Toleration, h Hasher) []v1.Toleration {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.Toleration, len(in))
	for i, toleration := range in {
		out[i] = toleration
		out[i].Key = h.LabelKey(toleration.Key)
		out[i].Value = h.LabelValue(toleration.Key, toleration.Value)
	}
	return out
}

func redactTaints(in []v1.Taint, h Hasher) []v1.Taint {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.Taint, len(in))
	for i, taint := range in {
		out[i] = taint
		out[i].Key = h.LabelKey(taint.Key)
		out[i].Value = h.LabelValue(taint.Key, taint.Value)
	}
	return out
}

func redactPodConditions(in []v1.PodCondition) []v1.PodCondition {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.PodCondition, len(in))
	for i, condition := range in {
		out[i] = v1.PodCondition{
			Type:               condition.Type,
			Status:             condition.Status,
			Reason:             condition.Reason,
			LastTransitionTime: condition.LastTransitionTime,
		}
		if FreeTextIsName(out[i].Reason) {
			out[i].Reason = ""
		}
	}
	return out
}

func redactNodeConditions(in []v1.NodeCondition) []v1.NodeCondition {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.NodeCondition, 0, len(in))
	for _, condition := range in {
		if condition.Type != v1.NodeReady {
			continue
		}
		out = append(out, v1.NodeCondition{
			Type:               condition.Type,
			Status:             condition.Status,
			Reason:             condition.Reason,
			LastTransitionTime: condition.LastTransitionTime,
		})
	}
	return out
}

func redactAffinity(in *v1.Affinity, h Hasher) *v1.Affinity {
	if in == nil {
		return nil
	}
	out := &v1.Affinity{}
	if in.NodeAffinity != nil {
		out.NodeAffinity = &v1.NodeAffinity{}
		if required := in.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution; required != nil {
			out.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = redactNodeSelectorTerms(required, h)
		}
		for _, preferred := range in.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
			out.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
				out.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
				v1.PreferredSchedulingTerm{
					Weight:     preferred.Weight,
					Preference: redactNodeSelectorTerm(preferred.Preference, h),
				},
			)
		}
	}
	if in.PodAffinity != nil {
		out.PodAffinity = &v1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution:  redactPodAffinityTerms(in.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution, h),
			PreferredDuringSchedulingIgnoredDuringExecution: redactWeightedPodAffinity(in.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution, h),
		}
	}
	if in.PodAntiAffinity != nil {
		out.PodAntiAffinity = &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution:  redactPodAffinityTerms(in.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution, h),
			PreferredDuringSchedulingIgnoredDuringExecution: redactWeightedPodAffinity(in.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution, h),
		}
	}
	return out
}

func redactNodeSelectorTerms(in *v1.NodeSelector, h Hasher) *v1.NodeSelector {
	if in == nil {
		return nil
	}
	out := &v1.NodeSelector{}
	for _, term := range in.NodeSelectorTerms {
		out.NodeSelectorTerms = append(out.NodeSelectorTerms, redactNodeSelectorTerm(term, h))
	}
	return out
}

func redactNodeSelectorTerm(in v1.NodeSelectorTerm, h Hasher) v1.NodeSelectorTerm {
	return v1.NodeSelectorTerm{
		MatchExpressions: redactNodeSelectorRequirements(in.MatchExpressions, h),
		MatchFields:      redactNodeSelectorRequirements(in.MatchFields, h),
	}
}

func redactNodeSelectorRequirements(in []v1.NodeSelectorRequirement, h Hasher) []v1.NodeSelectorRequirement {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.NodeSelectorRequirement, len(in))
	for i, req := range in {
		values := make([]string, len(req.Values))
		for j, value := range req.Values {
			if req.Key == "metadata.name" {
				values[j] = h.Hash(value)
				continue
			}
			values[j] = h.LabelValue(req.Key, value)
		}
		out[i] = v1.NodeSelectorRequirement{
			Key:      h.LabelKey(req.Key),
			Operator: req.Operator,
			Values:   values,
		}
	}
	return out
}

func redactPodAffinityTerms(in []v1.PodAffinityTerm, h Hasher) []v1.PodAffinityTerm {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.PodAffinityTerm, len(in))
	for i, term := range in {
		out[i] = v1.PodAffinityTerm{
			LabelSelector:     redactLabelSelector(term.LabelSelector, h),
			Namespaces:        h.HashEach(term.Namespaces),
			NamespaceSelector: redactLabelSelector(term.NamespaceSelector, h),
			TopologyKey:       h.LabelKey(term.TopologyKey),
		}
	}
	return out
}

func redactWeightedPodAffinity(in []v1.WeightedPodAffinityTerm, h Hasher) []v1.WeightedPodAffinityTerm {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.WeightedPodAffinityTerm, len(in))
	for i, term := range in {
		out[i] = v1.WeightedPodAffinityTerm{
			Weight:          term.Weight,
			PodAffinityTerm: redactPodAffinityTerms([]v1.PodAffinityTerm{term.PodAffinityTerm}, h)[0],
		}
	}
	return out
}

func redactLabelSelector(in *metav1.LabelSelector, h Hasher) *metav1.LabelSelector {
	if in == nil {
		return nil
	}
	out := &metav1.LabelSelector{}
	if len(in.MatchLabels) > 0 {
		out.MatchLabels = redactLabels(in.MatchLabels, h)
	}
	for _, req := range in.MatchExpressions {
		values := make([]string, len(req.Values))
		for i, value := range req.Values {
			values[i] = h.LabelValue(req.Key, value)
		}
		out.MatchExpressions = append(out.MatchExpressions, metav1.LabelSelectorRequirement{
			Key:      h.LabelKey(req.Key),
			Operator: req.Operator,
			Values:   values,
		})
	}
	return out
}

func redactTopologySpread(in []v1.TopologySpreadConstraint, h Hasher) []v1.TopologySpreadConstraint {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.TopologySpreadConstraint, len(in))
	for i, constraint := range in {
		out[i] = v1.TopologySpreadConstraint{
			MaxSkew:            constraint.MaxSkew,
			TopologyKey:        h.LabelKey(constraint.TopologyKey),
			WhenUnsatisfiable:  constraint.WhenUnsatisfiable,
			LabelSelector:      redactLabelSelector(constraint.LabelSelector, h),
			MinDomains:         constraint.MinDomains,
			NodeAffinityPolicy: constraint.NodeAffinityPolicy,
			NodeTaintsPolicy:   constraint.NodeTaintsPolicy,
			MatchLabelKeys:     labelKeys(constraint.MatchLabelKeys, h),
		}
	}
	return out
}

func redactAllowedTopologies(in []v1.TopologySelectorTerm, h Hasher) []v1.TopologySelectorTerm {
	if len(in) == 0 {
		return nil
	}
	out := make([]v1.TopologySelectorTerm, len(in))
	for i, term := range in {
		for _, expr := range term.MatchLabelExpressions {
			values := make([]string, len(expr.Values))
			for j, value := range expr.Values {
				values[j] = h.LabelValue(expr.Key, value)
			}
			out[i].MatchLabelExpressions = append(out[i].MatchLabelExpressions, v1.TopologySelectorLabelRequirement{
				Key:    h.LabelKey(expr.Key),
				Values: values,
			})
		}
	}
	return out
}

func labelKeys(in []string, h Hasher) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, key := range in {
		out[i] = h.LabelKey(key)
	}
	return out
}

func hashNodeNames[T any](obj T, h Hasher) T {
	raw, err := json.Marshal(obj)
	if err != nil {
		return obj
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return obj
	}
	walked := walkHashKeys(decoded, h)
	raw, err = json.Marshal(walked)
	if err != nil {
		return obj
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return obj
	}
	return out
}

func walkHashKeys(value any, h Hasher) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if key == "nodeName" {
				if text, ok := child.(string); ok {
					out[key] = h.Hash(text)
					continue
				}
			}
			out[key] = walkHashKeys(child, h)
		}
		return out
	case []any:
		for i := range typed {
			typed[i] = walkHashKeys(typed[i], h)
		}
		return typed
	default:
		return value
	}
}

func mapSlice[T any](in []T, fn func(T) T) []T {
	if in == nil {
		return []T{}
	}
	out := make([]T, 0, len(in))
	for _, item := range in {
		out = append(out, fn(item))
	}
	return out
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package mpi

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow"
)

const (
	ReplicaSpecName = "mpiReplicaSpecs"
	MasterName      = "Launcher"
	WorkerName      = "Worker"

	delayedLauncherCreationPolicy = "WaitForWorkersReady"
	jobNameLabel                  = "training.kubeflow.org/job-name"
	podRoleNameLabel              = "training.kubeflow.org/job-role"
	launcherPodRoleName           = "launcher"
)

type MpiGrouper struct {
	client client.Client
	*kubeflow.KubeflowDistributedGrouper
}

func NewMpiGrouper(client client.Client, kubeflowGrouper *kubeflow.KubeflowDistributedGrouper) *MpiGrouper {
	return &MpiGrouper{
		client:                     client,
		KubeflowDistributedGrouper: kubeflowGrouper,
	}
}

func (mg *MpiGrouper) Name() string {
	return "MPI Grouper"
}

func (mg *MpiGrouper) GetPodGroupMetadata(topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	gp, err := mg.KubeflowDistributedGrouper.GetPodGroupMetadata(topOwner, pod, ReplicaSpecName, []string{MasterName, WorkerName})
	if err != nil {
		return nil, err
	}

	launcherCreationPolicy, found, err := unstructured.NestedString(topOwner.Object, "spec", "launcherCreationPolicy")
	if err != nil {
		return nil, err
	}
	if found && launcherCreationPolicy == delayedLauncherCreationPolicy {
		if err := mg.handleDelayedLauncherPolicy(topOwner, gp); err != nil {
			return gp, err
		}
	}
	return gp, nil
}

func (mg *MpiGrouper) handleDelayedLauncherPolicy(topOwner *unstructured.Unstructured, gp *podgroup.Metadata) error {
	hasLauncherPod, err := mg.launcherPodExists(topOwner)
	if err != nil {
		return err
	}
	if !hasLauncherPod {
		replicas, found, err := unstructured.NestedInt64(topOwner.Object, "spec", ReplicaSpecName, MasterName, "replicas")
		if err != nil || !found || replicas == 0 { //  This if should always be false - the same thing is tested in GetPodGroupMetadata
			return fmt.Errorf("the type of the job %s doesn't allow for 0 mpiReplicaSpecs pods. Please fix the replica field", topOwner.GetName())
		}
		// If the delayedLauncherCreationPolicy is set and the launcher pod hasn't been created yet,
		// the minAvailable should not contain the launcher as part of the replicas
		gp.MinAvailable = gp.MinAvailable - int32(replicas)
	}
	return nil
}

func (mg *MpiGrouper) launcherPodExists(topOwner *unstructured.Unstructured) (bool, error) {
	var pods v1.PodList
	err := mg.client.List(context.Background(), &pods, client.InNamespace(
		topOwner.GetNamespace()),
		client.MatchingLabels{
			jobNameLabel:     topOwner.GetName(),
			podRoleNameLabel: launcherPodRoleName,
		},
	)
	if err != nil {
		return false, err
	}

	// filter for non terminating launcher pods
	hasLauncherPod := false
	for _, pod := range pods.Items {
		if pod.GetObjectMeta().GetDeletionTimestamp() == nil {
			hasLauncherPod = true
			break
		}
	}
	return hasLauncherPod, nil
}

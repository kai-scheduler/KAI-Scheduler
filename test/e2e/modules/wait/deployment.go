/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package wait

import (
	"context"
	"fmt"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait/watcher"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

type ArgsUpdater func(args []string) []string

// WaitForDeploymentPodsRunning waits until all pods from a deployment are in the running state
func WaitForDeploymentPodsRunning(ctx context.Context, client runtimeClient.WithWatch, name, namespace string) {
	deployment := &appsv1.Deployment{}
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, deployment)
	if err != nil {
		Fail(fmt.Sprintf("failed to get deployment %s/%s: %v", namespace, name, err))
	}

	// Get the deployment's selector
	selector := deployment.Spec.Selector
	if selector == nil {
		Fail(fmt.Sprintf("deployment %s/%s has no selector", namespace, name))
	}

	// Get the desired replica count
	var replicas int32 = 1
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}

	isTheDeploymentReady := func(event watch.Event) bool {
		podListObj, ok := event.Object.(*v1.PodList)
		if !ok {
			return false
		}

		// Return false if no pods found
		if len(podListObj.Items) == 0 {
			return false
		}

		// Count running pods and check for non-running pods
		runningPods := 0
		for _, pod := range podListObj.Items {
			if pod.Status.Phase == v1.PodRunning {
				runningPods++
			} else {
				// If any pod is not running, return false
				return false
			}
		}

		return int32(runningPods) == replicas
	}

	// Create a watcher for the pods
	pw := watcher.NewGenericWatcher[v1.PodList](client, isTheDeploymentReady,
		runtimeClient.InNamespace(namespace),
		runtimeClient.MatchingLabels(selector.MatchLabels))

	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("timed out waiting for pods of deployment %s/%s to be running", namespace, name))
	}
}

func ForRolloutRestartDeployment(ctx context.Context, client runtimeClient.WithWatch, namespace, name string) error {
	// Get the orgDeployment
	orgDeployment := &appsv1.Deployment{}
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, orgDeployment)
	if err != nil {
		Fail(fmt.Sprintf("failed to get deployment %s/%s: %v", namespace, name, err))
	}
	orgPods := &v1.PodList{}
	err = client.List(ctx, orgPods, runtimeClient.InNamespace(namespace), runtimeClient.MatchingLabels(orgDeployment.Spec.Selector.MatchLabels))
	if err != nil {
		Fail(fmt.Sprintf("failed to get pods of deployment %s/%s: %v", namespace, name, err))
	}

	// Create a copy for patching
	deployment := orgDeployment.DeepCopy()

	// Add or update the restartedAt annotation
	if deployment.Spec.Template.ObjectMeta.Annotations == nil {
		deployment.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.ObjectMeta.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	// Create and apply the patch
	patch := runtimeClient.MergeFrom(orgDeployment)
	err = client.Patch(ctx, deployment, patch)
	if err != nil {
		return fmt.Errorf("failed to patch deployment %s: %w", name, err)
	}

	waitForDeploymentPodsToBeDeleted(ctx, client, orgPods, deployment)
	WaitForDeploymentPodsRunning(ctx, client, name, namespace)

	return nil
}

func waitForDeploymentPodsToBeDeleted(ctx context.Context, client runtimeClient.WithWatch, orgPods *v1.PodList,
	deployment *appsv1.Deployment) {
	orgPodUIDs := []types.UID{}
	for _, pod := range orgPods.Items {
		orgPodUIDs = append(orgPodUIDs, pod.UID)
	}

	ForPodsWithCondition(ctx, client, func(event watch.Event) bool {
		podList, ok := event.Object.(*v1.PodList)
		if !ok {
			return false
		}
		for _, pod := range podList.Items {
			if slices.Contains(orgPodUIDs, pod.UID) {
				return false
			}
		}
		return true
	}, runtimeClient.InNamespace(deployment.Namespace),
		runtimeClient.MatchingLabels(deployment.Spec.Selector.MatchLabels))
}

func patchDeploymentArgs(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
	deploymentName string,
	containerName string,
	argsUpdater ArgsUpdater,
) error {
	// Get the deployment
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(
		ctx,
		deploymentName,
		metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %v", deploymentName, err)
	}

	// Create a copy to use as the base for our patch
	deploymentCopy := deployment.DeepCopy()

	// Find and update the container args in our copy
	containerFound := false
	for i, container := range deploymentCopy.Spec.Template.Spec.Containers {
		if container.Name == containerName {
			deploymentCopy.Spec.Template.Spec.Containers[i].Args = argsUpdater(container.Args)
			containerFound = true
			break
		}
	}

	if !containerFound {
		return fmt.Errorf("container %s not found in deployment %s", containerName, deploymentName)
	}

	// Create the patch data
	patchBytes, err := client.MergeFrom(deployment).Data(deploymentCopy)
	if err != nil {
		return fmt.Errorf("failed to create patch data: %v", err)
	}

	// Apply the patch
	_, err = clientset.AppsV1().Deployments(namespace).Patch(
		ctx,
		deploymentName,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to patch deployment %s: %w", deploymentName, err)
	}

	return nil
}

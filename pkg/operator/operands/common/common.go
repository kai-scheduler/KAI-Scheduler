// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/exp/slices"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiv1common "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	kaiConfigUtils "github.com/kai-scheduler/KAI-scheduler/pkg/operator/config"
)

var controllerTypes = []string{"Deployment", "DaemonSet"}

func AllControllersAvailable(
	ctx context.Context, readerClient client.Reader, objects []client.Object,
) (bool, error) {
	availableControllers := 0
	errorMessages := []string{}

	for _, obj := range objects {
		err := readerClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		if err != nil {
			errorMessages = append(errorMessages, err.Error())
			continue
		}

		if slices.Contains(controllerTypes, obj.GetObjectKind().GroupVersionKind().Kind) {
			available, err := isControllerAvailable(obj)
			if err != nil {
				errorMessages = append(errorMessages, err.Error())
				continue
			}
			if !available {
				errorMessages = append(errorMessages, fmt.Sprintf(
					"%s [%s] is not available",
					obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(),
				))
				continue
			}
			availableControllers++
		}
	}

	if len(errorMessages) > 0 {
		return false, fmt.Errorf("%s", strings.Join(errorMessages, "\n"))
	}

	return true, nil
}

func AllObjectsExists(
	ctx context.Context, runtimeClient client.Reader, objects []client.Object,
) (bool, error) {
	for _, obj := range objects {
		err := runtimeClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
	}

	return true, nil
}

func ObjectForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, object client.Object,
	resourceName string, resourceNamespace string,
) (client.Object, error) {
	err := runtimeClient.Get(ctx, client.ObjectKey{
		Name:      resourceName,
		Namespace: resourceNamespace,
	}, object)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}

	object.SetName(resourceName)
	object.SetNamespace(resourceNamespace)
	if object.GetLabels() == nil {
		object.SetLabels(map[string]string{})
	}
	object.GetLabels()["app"] = resourceName

	return object, nil
}

func DeploymentForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config, service *kaiv1common.Service, deploymentName string,
) (*appsv1.Deployment, error) {

	deploymentObj, err := ObjectForKAIConfig(
		ctx, runtimeClient, &appsv1.Deployment{}, deploymentName, kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	deployment := deploymentObj.(*appsv1.Deployment)
	deployment.TypeMeta = metav1.TypeMeta{
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}

	deployment.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app": deploymentName,
		},
	}

	if deployment.Spec.Template.Labels == nil {
		deployment.Spec.Template.Labels = map[string]string{}
	}
	deployment.Spec.Template.Labels["app"] = deploymentName

	deployment.Spec.Template.Spec.ServiceAccountName = deploymentName
	deployment.Spec.Template.Spec.Affinity = kaiConfig.Spec.Global.Affinity
	deployment.Spec.Template.Spec.Tolerations = kaiConfig.Spec.Global.Tolerations

	deployment.Spec.Template.Spec.Containers = []v1.Container{
		{
			Name:            deploymentName,
			Image:           service.Image.Url(),
			ImagePullPolicy: *service.Image.PullPolicy,
			Resources:       v1.ResourceRequirements(*service.Resources),
			SecurityContext: kaiConfig.Spec.Global.GetSecurityContext(),
		},
	}

	deployment.Spec.Template.Spec.ImagePullSecrets = kaiConfigUtils.GetGlobalImagePullSecrets(kaiConfig.Spec.Global)

	return deployment, nil
}

func PtrFrom[T any](v T) *T {
	return &v
}

func isControllerAvailable(obj client.Object) (bool, error) {
	switch obj.GetObjectKind().GroupVersionKind().Kind {
	case "Deployment":
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok {
			return false, fmt.Errorf(
				"Failed to process deployment %s/%s", obj.GetNamespace(), obj.GetName())
		}

		if deployment.Spec.Replicas == nil {
			return false, nil
		}

		if deployment.Status.UpdatedReplicas != *deployment.Spec.Replicas {
			return false, nil
		}

		for _, condition := range deployment.Status.Conditions {
			if condition.Type == appsv1.DeploymentAvailable && condition.Status == v1.ConditionTrue {
				return true, nil
			}
		}
	case "DaemonSet":
		daemonSet, ok := obj.(*appsv1.DaemonSet)
		if !ok {
			return false, fmt.Errorf(
				"failed to process daemonSet %s/%s", obj.GetNamespace(), obj.GetName())
		}

		return daemonSet.Status.DesiredNumberScheduled == daemonSet.Status.NumberAvailable, nil
	}

	return false, nil
}

func AddK8sClientConfigToArgs(k8sClientConfig *kaiv1common.K8sClientConfig, args []string) {
	if k8sClientConfig != nil {
		if k8sClientConfig.QPS != nil {
			args = append(args, "--qps", strconv.Itoa(*k8sClientConfig.QPS))
		}
		if k8sClientConfig.Burst != nil {
			args = append(args, "--burst", strconv.Itoa(*k8sClientConfig.Burst))
		}
	}
}

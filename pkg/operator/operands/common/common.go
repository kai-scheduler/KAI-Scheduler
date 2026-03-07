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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	kaiv1common "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1/common"
	kaiConfigUtils "github.com/kai-scheduler/kai-scheduler/pkg/operator/config"
)

var controllerTypes = []string{"Deployment", "DaemonSet"}

// KAI services that should be monitored via ServiceMonitor
// For now, we only monitor the queue controller. Add more services here if needed.
var KaiServicesForServiceMonitor = []struct {
	Name     string
	Port     string
	JobLabel string

	LabelSelector map[string]string // optional, if not provided, "app": NAME will be used
	Namespaces    []string          // optional, if not provided, the service will be monitored in the namespace of the KAI config
}{
	{"queue-controller", "metrics", "queue-controller", nil, nil},
	{"kube-state-metrics", "http", "kube-state-metrics",
		map[string]string{"app.kubernetes.io/name": "kube-state-metrics"}, []string{"monitoring", "default"}},
}

func AllControllersAvailable(
	ctx context.Context, readerClient client.Reader, objects []client.Object,
) (bool, error) {
	availableControllers := 0
	errorMessages := []string{}

	for _, obj := range objects {
		objKind := obj.GetObjectKind().GroupVersionKind().Kind
		err := readerClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		if err != nil {
			errorMessages = append(errorMessages, err.Error())
			continue
		}

		if slices.Contains(controllerTypes, objKind) {
			available, err := isControllerAvailable(obj, objKind)
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
	deployment.Spec.Template.Spec.NodeSelector = kaiConfig.Spec.Global.NodeSelector
	deployment.Spec.Template.Spec.Tolerations = kaiConfig.Spec.Global.Tolerations

	deployment.Spec.Template.Spec.Affinity = MergeAffinities(service.Affinity,
		kaiConfig.Spec.Global.Affinity,
		deployment.Spec.Selector.MatchLabels,
		*kaiConfig.Spec.Global.RequireDefaultPodAntiAffinityTerm)

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

func isControllerAvailable(obj client.Object, objKind string) (bool, error) {
	switch objKind {
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

func CheckCRDsAvailable(ctx context.Context, client client.Reader, crdNames ...string) (bool, error) {
	for _, name := range crdNames {
		crd := &metav1.PartialObjectMetadata{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CustomResourceDefinition",
				APIVersion: "apiextensions.k8s.io/v1",
			},
		}
		err := client.Get(ctx, types.NamespacedName{Name: name}, crd)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("failed to check CRD %s: %w", name, err)
		}
	}
	return true, nil
}

func CheckPrometheusCRDsAvailable(ctx context.Context, client client.Reader, targetCRDs ...string) (bool, error) {
	var names []string
	for _, targetCRD := range targetCRDs {
		switch targetCRD {
		case "prometheus":
			names = append(names, "prometheuses.monitoring.coreos.com")
		case "serviceMonitor":
			names = append(names, "servicemonitors.monitoring.coreos.com")
		default:
			names = append(names, targetCRD)
		}
	}
	return CheckCRDsAvailable(ctx, client, names...)
}

func MergeAffinities(localAffinity *v1.Affinity,
	globalAffinity *v1.Affinity,
	podAntiAffinityLabel map[string]string,
	requireDefaultPodAntiAffinityTerm bool) *v1.Affinity {
	if localAffinity == nil {
		return globalAffinity
	}

	if globalAffinity == nil {
		return localAffinity
	}

	affinity := &v1.Affinity{}

	// If NodeAffinity is defined in localAffinity, use it; otherwise use from globalAffinity
	if localAffinity.NodeAffinity != nil {
		affinity.NodeAffinity = localAffinity.NodeAffinity
	} else if globalAffinity.NodeAffinity != nil {
		affinity.NodeAffinity = globalAffinity.NodeAffinity
	}

	// If PodAffinity is defined in localAffinity, use it; otherwise use from globalAffinity
	if localAffinity.PodAffinity != nil {
		affinity.PodAffinity = localAffinity.PodAffinity
	} else if globalAffinity.PodAffinity != nil {
		affinity.PodAffinity = globalAffinity.PodAffinity
	}

	podAntiAffinity := &v1.PodAntiAffinity{}

	if localAffinity.PodAntiAffinity != nil {
		podAntiAffinity = localAffinity.PodAntiAffinity
	} else if globalAffinity.PodAntiAffinity != nil {
		podAntiAffinity = globalAffinity.PodAntiAffinity
	} else if len(podAntiAffinityLabel) > 0 {
		podAffinityTerm := v1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: podAntiAffinityLabel,
			},
			TopologyKey: "kubernetes.io/hostname",
		}

		podAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
			podAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
			v1.WeightedPodAffinityTerm{
				Weight:          100,
				PodAffinityTerm: podAffinityTerm,
			},
		)

		if requireDefaultPodAntiAffinityTerm {
			podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(
				podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
				podAffinityTerm,
			)
		}
	}

	affinity.PodAntiAffinity = podAntiAffinity

	return affinity
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group_controller

import (
	"context"
	"fmt"
	"strconv"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/pod_group_controller"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	generate "github.com/NVIDIA/KAI-scheduler/pkg/operator/cert-utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/operator/operands/common"
)

const (
	defaultResourceName = "podgroup-controller"
	appName             = defaultResourceName
	serviceName         = defaultResourceName

	podGroupWebhookName = "podgroup-validation.kai.scheduler"

	secretName = "podgroup-webhook-tls-secret"
	certKey    = "tls.crt"
	keyKey     = "tls.key"
)

func (p *PodGroupController) deploymentForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {

	config := kaiConfig.Spec.PodGroupController

	deployment, err := common.DeploymentForKAIConfig(ctx, runtimeClient, kaiConfig, config.Service, p.BaseResourceName)
	if err != nil {
		return nil, err
	}

	deployment.Spec.Replicas = config.Replicas
	deployment.Spec.Template.Spec.Containers[0].Args = buildArgsList(config, *kaiConfig.Spec.Global.SchedulerName,
		kaiConfig.Spec.Global.JSONLog != nil && *kaiConfig.Spec.Global.JSONLog)
	deployment.Spec.Template.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
		{
			Name:      "cert",
			ReadOnly:  true,
			MountPath: "/tmp/k8s-webhook-server/serving-certs",
		},
	}

	deployment.Spec.Template.Spec.Volumes = []v1.Volume{
		{
			Name: "cert",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName:  secretName,
					DefaultMode: ptr.To(int32(420)),
				},
			},
		},
	}

	return []client.Object{deployment}, nil
}

func (p *PodGroupController) serviceAccountForKAIConfig(
	ctx context.Context, k8sReader client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	sa, err := common.ObjectForKAIConfig(ctx, k8sReader, &v1.ServiceAccount{}, p.BaseResourceName,
		kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	sa.(*v1.ServiceAccount).TypeMeta = metav1.TypeMeta{
		Kind:       "ServiceAccount",
		APIVersion: "v1",
	}
	return []client.Object{sa}, err
}

func (p *PodGroupController) serviceForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	obj, err := common.ObjectForKAIConfig(ctx, runtimeClient, &v1.Service{}, p.BaseResourceName,
		kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}

	service := obj.(*v1.Service)
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}

	service.Spec.Ports = []v1.ServicePort{
		{
			Name:       *kaiConfig.Spec.PodGroupController.ControllerService.Webhook.Name,
			Port:       int32(*kaiConfig.Spec.PodGroupController.ControllerService.Webhook.Port),
			Protocol:   v1.ProtocolTCP,
			TargetPort: intstr.FromInt(*kaiConfig.Spec.PodGroupController.ControllerService.Webhook.TargetPort),
		},
	}
	service.Spec.Selector = map[string]string{
		"app": p.BaseResourceName,
	}

	return []client.Object{service}, nil
}

func (p *PodGroupController) secretForKAIConfig(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	obj, err := common.ObjectForKAIConfig(ctx, runtimeClient, &v1.Secret{}, secretName, kaiConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}

	secret := obj.(*v1.Secret)
	secret.TypeMeta = metav1.TypeMeta{
		Kind:       "Secret",
		APIVersion: "v1",
	}

	if _, found := secret.Data[certKey]; !found {
		serviceUrl := fmt.Sprintf("%s.%s.svc", p.BaseResourceName, kaiConfig.Spec.Namespace)
		cert, key, err := generate.GenerateSelfSignedCert(serviceUrl, []string{serviceUrl})
		if err != nil {
			return nil, err
		}
		secret.Data = map[string][]byte{
			certKey: cert,
			keyKey:  key,
		}
	}
	return []client.Object{secret}, nil
}

func (p *PodGroupController) validatingWCForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
	secret *v1.Secret,
) ([]client.Object, error) {
	var err error
	if !*kaiConfig.Spec.PodGroupController.Webhooks.EnableValidation {
		return nil, nil
	}

	crt, found := secret.Data[certKey]
	if !found {
		return nil, fmt.Errorf("unable to create validating webhooks for podgroup controller: "+
			"missing key %s in secret %s/%s", certKey, kaiConfig.Spec.Namespace, secretName)
	}

	validatingWebhookConfigurations := []client.Object{}
	for _, version := range constants.PodGroupValidatedVersions() {
		validatingWebhookConfiguration := &admissionv1.ValidatingWebhookConfiguration{}

		webhookName := fmt.Sprintf("%s%s", *kaiConfig.Spec.PodGroupController.Webhooks.WebhookConfigurationNamePrefix, version)
		err = runtimeClient.Get(ctx, types.NamespacedName{Name: webhookName}, validatingWebhookConfiguration)
		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
		validatingWebhookConfiguration.Name = webhookName

		if validatingWebhookConfiguration.Labels == nil {
			validatingWebhookConfiguration.Labels = map[string]string{}
		}
		validatingWebhookConfiguration.Labels["app"] = p.BaseResourceName
		validatingWebhookConfiguration.Webhooks = []admissionv1.ValidatingWebhook{
			{
				Name:                    podGroupWebhookName,
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             ptr.To(admissionv1.SideEffectClassNone),
				FailurePolicy:           ptr.To(admissionv1.Fail),
				ClientConfig: p.webhookClientConfig(kaiConfig.Spec.Namespace,
					fmt.Sprintf("/validate-scheduling-run-ai-%s-podgroup", version), crt,
					*kaiConfig.Spec.PodGroupController.ControllerService.Webhook.Port),
				Rules: []admissionv1.RuleWithOperations{
					{
						Operations: []admissionv1.OperationType{
							admissionv1.Create,
							admissionv1.Update,
						},
						Rule: admissionv1.Rule{
							APIGroups:   []string{"scheduling.run.ai"},
							APIVersions: []string{version},
							Resources: []string{
								"podgroups",
							},
							Scope: ptr.To(admissionv1.NamespacedScope),
						},
					},
				},
			},
		}

		validatingWebhookConfigurations = append(validatingWebhookConfigurations, validatingWebhookConfiguration)
	}

	return validatingWebhookConfigurations, nil
}

func (p *PodGroupController) webhookClientConfig(namespace, path string, cabundle []byte, port int) admissionv1.WebhookClientConfig {
	return admissionv1.WebhookClientConfig{
		Service: &admissionv1.ServiceReference{
			Namespace: namespace,
			Name:      p.BaseResourceName,
			Path:      ptr.To(path),
			Port:      ptr.To(int32(port)),
		},
		CABundle: cabundle,
	}
}

func buildArgsList(config *pod_group_controller.PodGroupController, schedulerName string, jsonLog bool) []string {
	args := []string{
		"--scheduler-name", schedulerName,
	}

	common.AddK8sClientConfigToArgs(config.Service.K8sClientConfig, args)

	if config.MaxConcurrentReconciles != nil {
		args = append(args, "--max-concurrent-reconciles", strconv.Itoa(*config.MaxConcurrentReconciles))
	}

	if config.Replicas != nil && *config.Replicas > 1 {
		args = append(args, "--leader-elect")
	}

	if jsonLog {
		args = append(args, "--zap-devel=false")
	}

	return args
}

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_controller

import (
	"context"
	"fmt"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	generate "github.com/NVIDIA/KAI-scheduler/pkg/operator/cert-utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/operator/operands/common"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	QueueCrdName = "queues.scheduling.run.ai"

	defaultResourceName = "queue-controller"
	serviceName         = defaultResourceName
	appName             = defaultResourceName
	queueWebhookName    = "queue-validation.kai.scheduler"

	secretName = "queue-webhook-tls-secret"
	certKey    = "tls.crt"
	keyKey     = "tls.key"
)

func (q *QueueController) deploymentForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {

	config := kaiConfig.Spec.QueueController
	deployment, err := common.DeploymentForKAIConfig(ctx, runtimeClient, kaiConfig, config.Service, q.BaseResourceName)
	if err != nil {
		return nil, err
	}

	deployment.Spec.Replicas = config.Replicas
	deployment.Spec.Template.Spec.Containers[0].Args = buildArgsList(kaiConfig)
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

func (q *QueueController) serviceAccountForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	sa, err := common.ObjectForKAIConfig(ctx, runtimeClient, &v1.ServiceAccount{}, q.BaseResourceName,
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

func (q *QueueController) serviceForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	obj, err := common.ObjectForKAIConfig(ctx, runtimeClient, &v1.Service{}, q.BaseResourceName,
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
			Name:       *kaiConfig.Spec.QueueController.ControllerService.Webhook.Name,
			Port:       int32(*kaiConfig.Spec.QueueController.ControllerService.Webhook.Port),
			Protocol:   v1.ProtocolTCP,
			TargetPort: intstr.FromInt(*kaiConfig.Spec.QueueController.ControllerService.Webhook.TargetPort),
		},
		{
			Name:       *kaiConfig.Spec.QueueController.ControllerService.Metrics.Name,
			Port:       int32(*kaiConfig.Spec.QueueController.ControllerService.Metrics.Port),
			Protocol:   v1.ProtocolTCP,
			TargetPort: intstr.FromInt(*kaiConfig.Spec.QueueController.ControllerService.Metrics.TargetPort),
		},
	}
	service.Spec.Selector = map[string]string{
		"app": q.BaseResourceName,
	}

	return []client.Object{service}, nil
}

func crdForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
) ([]client.Object, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := runtimeClient.Get(ctx, types.NamespacedName{Name: QueueCrdName}, crd)
	if err != nil {
		return nil, client.IgnoreNotFound(err)
	}

	if crd.Spec.Conversion != nil {
		crd.Spec.Conversion = nil
		return []client.Object{crd}, nil
	}
	return []client.Object{}, nil
}

func (q *QueueController) secretForKAIConfig(ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
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
		serviceUrl := fmt.Sprintf("%s.%s.svc", q.BaseResourceName, kaiConfig.Spec.Namespace)
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

func (q *QueueController) validatingWCForKAIConfig(
	ctx context.Context, runtimeClient client.Reader, kaiConfig *kaiv1.Config,
	secret *v1.Secret,
) ([]client.Object, error) {
	var err error
	if *kaiConfig.Spec.QueueController.Webhooks.EnableValidation == false {
		return nil, nil
	}

	crt, found := secret.Data[certKey]
	if !found {
		return nil, fmt.Errorf("unable to create validating webhooks for queue controller: missing key %s in"+
			" secret %s/%s", certKey, kaiConfig.Spec.Namespace, secretName)
	}

	validatingWebhookConfigurations := []client.Object{}
	for _, version := range constants.QueueValidatedVersions() {
		validatingWebhookConfiguration := &admissionv1.ValidatingWebhookConfiguration{}

		webhookName := fmt.Sprintf("%s%s", *kaiConfig.Spec.QueueController.Webhooks.WebhookConfigurationNamePrefix, version)
		err = runtimeClient.Get(ctx, types.NamespacedName{Name: webhookName}, validatingWebhookConfiguration)
		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
		validatingWebhookConfiguration.Name = webhookName

		if validatingWebhookConfiguration.Labels == nil {
			validatingWebhookConfiguration.Labels = map[string]string{}
		}
		validatingWebhookConfiguration.Labels["app"] = q.BaseResourceName
		validatingWebhookConfiguration.Webhooks = []admissionv1.ValidatingWebhook{
			{
				Name:                    queueWebhookName,
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             sideEffectsNone(),
				FailurePolicy:           failurePolicyTypeFail(),
				ClientConfig: q.webhookClientConfig(kaiConfig.Spec.Namespace,
					fmt.Sprintf("/validate-scheduling-run-ai-%s-queue", version), crt, int(*kaiConfig.Spec.QueueController.ControllerService.Webhook.Port)),
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
								"queues",
							},
							Scope: scopeTypeCluster(),
						},
					},
				},
			},
		}

		validatingWebhookConfigurations = append(validatingWebhookConfigurations, validatingWebhookConfiguration)
	}

	return validatingWebhookConfigurations, nil
}

func sideEffectsNone() *admissionv1.SideEffectClass {
	se := admissionv1.SideEffectClassNone
	return &se
}

func failurePolicyTypeFail() *admissionv1.FailurePolicyType {
	fp := admissionv1.Fail
	return &fp
}

func scopeTypeCluster() *admissionv1.ScopeType {
	s := admissionv1.ClusterScope
	return &s
}

func (q *QueueController) webhookClientConfig(namespace, path string, cabundle []byte, port int) admissionv1.WebhookClientConfig {
	return admissionv1.WebhookClientConfig{
		Service: &admissionv1.ServiceReference{
			Namespace: namespace,
			Name:      q.BaseResourceName,
			Path:      ptr.To(path),
			Port:      ptr.To(int32(port)),
		},
		CABundle: cabundle,
	}
}

func buildArgsList(kaiConfig *kaiv1.Config) []string {
	config := kaiConfig.Spec.QueueController
	args := []string{
		"--metrics-listen-address", fmt.Sprintf(":%d", *config.ControllerService.Metrics.Port),
	}
	if config.Replicas != nil && *config.Replicas > 1 {
		args = append(args, "--leader-elect")
	}
	if config.MetricsNamespace != nil {
		args = append(args, "--metrics-namespace", *config.MetricsNamespace)
	}

	if config.QueueLabelToMetricLabel != nil {
		args = append(args, "--queue-label-to-metric-label", *config.QueueLabelToMetricLabel)
	}

	if config.QueueLabelToDefaultMetricValue != nil {
		args = append(args, "--queue-label-to-default-metric-value", *config.QueueLabelToDefaultMetricValue)
	}

	common.AddK8sClientConfigToArgs(config.Service.K8sClientConfig, args)

	if kaiConfig.Spec.Global.JSONLog != nil && *kaiConfig.Spec.Global.JSONLog {
		args = append(args, "--zap-devel=false")
	}

	return args
}

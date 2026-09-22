// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	nvidiav1 "github.com/kai-scheduler/KAI-scheduler/third_party/nvidia/gpu-operator/api/nvidia/v1"
)

func TestAdmissionServiceIdentity(t *testing.T) {
	for _, tc := range []struct {
		name         string
		serviceName  *string
		baseName     string
		expectedName string
		upgrade      bool
	}{
		{name: "upstream default", expectedName: "admission"},
		{name: "custom operand default", baseName: "custom-admission", expectedName: "custom-admission"},
		{name: "fresh install with legacy identity", serviceName: ptr.To("engine-admission"), expectedName: "engine-admission"},
		{name: "upgrade preserves legacy certificate and service", serviceName: ptr.To("engine-admission"), expectedName: "engine-admission", upgrade: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			testScheme := runtime.NewScheme()
			require.NoError(t, scheme.AddToScheme(testScheme))
			require.NoError(t, kaiv1.AddToScheme(testScheme))
			require.NoError(t, nvidiav1.AddToScheme(testScheme))
			config := kaiConfigForAdmission()
			config.Spec.Namespace = "runai"
			config.Spec.Admission.ServiceName = tc.serviceName
			config.Spec.Admission.MutatingWebhookConfigurationName = ptr.To("mutating-engine-admission")
			config.Spec.Admission.ValidatingWebhookConfigurationName = ptr.To("validating-engine-admission")
			kubeClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
			expectedDNS := tc.expectedName + ".runai.svc"
			var oldSecret *v1.Secret
			if tc.upgrade {
				oldSecret = &v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: kaiAdmissionWebhookSecretName, Namespace: "runai"}}
				require.NoError(t, updateSelfSigned(oldSecret, expectedDNS))
				require.NoError(t, kubeClient.Create(ctx, oldSecret))
				require.NoError(t, kubeClient.Create(ctx, &v1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: tc.expectedName, Namespace: "runai"},
					Spec:       v1.ServiceSpec{ClusterIP: "10.0.0.42", Selector: map[string]string{"app": "engine-admission"}},
				}))
			}
			a := &Admission{BaseResourceName: tc.baseName}
			objects, err := a.DesiredState(ctx, kubeClient, config)
			require.NoError(t, err)
			var secret *v1.Secret
			var service *v1.Service
			var deployment *appsv1.Deployment
			var webhooks []admissionv1.WebhookClientConfig
			for _, object := range objects {
				switch obj := object.(type) {
				case *v1.Secret:
					secret = obj
				case *v1.Service:
					service = obj
				case *appsv1.Deployment:
					deployment = obj
				case *admissionv1.MutatingWebhookConfiguration:
					require.Equal(t, "mutating-engine-admission", obj.Name)
					for _, webhook := range obj.Webhooks {
						webhooks = append(webhooks, webhook.ClientConfig)
					}
				case *admissionv1.ValidatingWebhookConfiguration:
					require.Equal(t, "validating-engine-admission", obj.Name)
					for _, webhook := range obj.Webhooks {
						webhooks = append(webhooks, webhook.ClientConfig)
					}
				}
			}
			require.NotNil(t, secret)
			require.NotNil(t, service)
			require.NotNil(t, deployment)
			require.Equal(t, tc.expectedName, service.Name)
			for key, value := range service.Spec.Selector {
				require.Equal(t, value, deployment.Spec.Template.Labels[key])
			}
			pair, err := tls.X509KeyPair(secret.Data[certKey], secret.Data[keyKey])
			require.NoError(t, err)
			cert, err := x509.ParseCertificate(pair.Certificate[0])
			require.NoError(t, err)
			require.NoError(t, cert.VerifyHostname(expectedDNS))
			require.Len(t, webhooks, 4)
			for _, webhook := range webhooks {
				require.Equal(t, service.Name, webhook.Service.Name)
				require.Equal(t, service.Namespace, webhook.Service.Namespace)
				require.Equal(t, secret.Data[certKey], webhook.CABundle)
			}
			if tc.upgrade {
				require.Equal(t, oldSecret.Data, secret.Data)
				require.Equal(t, "10.0.0.42", service.Spec.ClusterIP)
			} else {
				require.NoError(t, kubeClient.Create(ctx, secret))
			}
			// A second reconciliation must not rotate a certificate with the preserved identity.
			objects, err = a.DesiredState(ctx, kubeClient, config)
			require.NoError(t, err)
			for _, object := range objects {
				if next, ok := object.(*v1.Secret); ok {
					require.Equal(t, secret.Data, next.Data)
				}
			}
		})
	}
}

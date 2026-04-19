// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package webhookmanager

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/webhookmanager/cert"
)

const (
	certKey = "tls.crt"
	keyKey  = "tls.key"
)

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;create;update;patch

func UpsertSecret(ctx context.Context, kubeClient client.Client, namespace, name, serviceName string) error {
	logger := log.FromContext(ctx)
	logger.Info("Looking for secret",
		"namespace", namespace, "name", name)
	secret := &v1.Secret{}
	err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get secret %s/%s, err: %v", namespace, name, err)
		}

		logger.Info("Secret was not found, creating",
			"namespace", namespace, "namea", name)
		secret, err = generateSecret(namespace, name, serviceName)
		if err != nil {
			return fmt.Errorf("failed to generate secret. err: %v", err)
		}

		logger.Info("Creating secret",
			"namespace", namespace, "name", name)
		return kubeClient.Create(ctx, secret)
	}

	serviceUrl := generateServiceUrl(serviceName, namespace)
	cert, key, err := cert.GenerateSelfSignedCert(serviceUrl, []string{serviceUrl})
	if err != nil {
		return fmt.Errorf("failed to generate cert. err: %v", err)
	}

	origSecret := secret.DeepCopy()
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[certKey] = cert
	secret.Data[keyKey] = key

	logger.Info("Updating secret",
		"namespace", namespace, "name", name)
	return kubeClient.Patch(ctx, secret, client.MergeFrom(origSecret))
}

func GetCertFromSecret(ctx context.Context, kubeClient client.Client, namespace, name string) ([]byte, error) {
	secret := &v1.Secret{}
	err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
	if err != nil {
		return nil, err
	}
	certBytes, found := secret.Data[certKey]
	if !found {
		return nil, fmt.Errorf("secret %s/%s does not contain a certificate", namespace, name)
	}
	return certBytes, nil
}

func generateServiceUrl(name, namespace string) string {
	return fmt.Sprintf("%s.%s.svc", name, namespace)
}

func generateSecret(namespace, name, serviceName string) (*v1.Secret, error) {
	serviceUrl := generateServiceUrl(serviceName, namespace)
	cert, key, err := cert.GenerateSelfSignedCert(serviceUrl, []string{serviceUrl})
	if err != nil {
		return nil, err
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string][]byte{
			certKey: cert,
			keyKey:  key,
		},
		Type: v1.SecretTypeOpaque,
	}
	return secret, nil
}

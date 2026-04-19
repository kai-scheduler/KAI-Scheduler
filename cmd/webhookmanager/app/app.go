// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"flag"

	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kai-scheduler/KAI-scheduler/pkg/webhookmanager"
)

var log = ctrl.Log.WithName("webhookmanager")

func Run(ctx context.Context) error {
	var options Options
	options.AddFlags(flag.CommandLine)

	initLogger()

	log.Info("Starting webhookmanager", "options", options)
	clientConfig := ctrl.GetConfigOrDie()
	kubeClient, err := client.New(clientConfig, client.Options{})
	if err != nil {
		return err
	}

	if options.UpsertSecret {
		err = webhookmanager.UpsertSecret(ctx, kubeClient, options.Namespace, options.SecretName, options.ServiceName)
		if err != nil {
			log.Error(err, "failed to upsert secret")
			return err
		}
		log.Info("Successfully created/updated secret",
			"namespace", options.Namespace, "name", options.SecretName)
	}

	if options.PatchWebhooks {
		err = patchWebhooks(ctx, kubeClient, &options)
		if err != nil {
			log.Error(err, "failed to patch webhooks")
			return err
		}
		log.Info("Successfully patched webhook configurations")
	}

	return nil
}

func patchWebhooks(ctx context.Context, kubeClient client.Client, options *Options) error {
	var err error
	var certBytes []byte
	certBytes, err = webhookmanager.GetCertFromSecret(ctx, kubeClient, options.Namespace, options.SecretName)
	if err != nil {
		log.Error(err, "failed to get cert bytes from secret",
			"name", options.SecretName, "namespace", options.Namespace)
		return err
	}

	if len(options.ValidatingWebhookName) > 0 {
		err = webhookmanager.PatchValidatingWebhookCA(ctx, kubeClient, options.ValidatingWebhookName, certBytes)
		if err != nil {
			log.Error(err, "failed to patch validating webhook",
				"webhook", options.ValidatingWebhookName)
			return err
		}
	}

	if len(options.MutatingWebhookName) > 0 {
		err = webhookmanager.PatchMutatingWebhookCA(ctx, kubeClient, options.MutatingWebhookName, certBytes)
		if err != nil {
			log.Error(err, "failed to patch mutating webhook",
				"webhook", options.MutatingWebhookName)
			return err
		}
	}

	return nil
}

func initLogger() {
	logOptions := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	logOptions.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOptions)))
}

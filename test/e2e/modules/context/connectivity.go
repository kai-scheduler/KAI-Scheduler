/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package context

import (
	"fmt"
	"os"
	"strings"

	lws "sigs.k8s.io/lws/api/leaderworkerset/v1"

	"k8s.io/api/node/v1alpha1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	kubeAiSchedulerV2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"

	kwokopv1beta1 "github.com/run-ai/kwok-operator/api/v1beta1"

	kubeAiSchedClient "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned"
	kwokv1alpha1 "sigs.k8s.io/kwok/pkg/apis/v1alpha1"
)

var kubeConfig *rest.Config
var kubeClientset *kubernetes.Clientset
var kubeAiSchedClientset *kubeAiSchedClient.Clientset
var controllerClient runtimeClient.WithWatch = nil

func initConnectivity() error {
	if kubeConfig == nil {
		err := initKubeConfig()
		if err != nil {
			return err
		}
	}
	if kubeClientset == nil {
		kubeClientset = kubernetes.NewForConfigOrDie(kubeConfig)
	}
	if kubeAiSchedClientset == nil {
		kubeAiSchedClientset = kubeAiSchedClient.NewForConfigOrDie(kubeConfig)
	}
	if controllerClient == nil {
		var err error
		controllerClient, err = runtimeClient.NewWithWatch(kubeConfig, runtimeClient.Options{})
		if err != nil {
			panic(err)
		}
		if err = kaiv1.AddToScheme(controllerClient.Scheme()); err != nil {
			return fmt.Errorf("failed to add kai v1 to scheme: %w", err)
		}
		if err = kaiv1alpha1.AddToScheme(controllerClient.Scheme()); err != nil {
			return fmt.Errorf("failed to add kai v1alpha1 to scheme: %w", err)
		}
		if err = v1alpha1.AddToScheme(controllerClient.Scheme()); err != nil {
			return fmt.Errorf("failed to add engine v1alpha1 to scheme: %w", err)
		}
		if err = v2alpha2.AddToScheme(controllerClient.Scheme()); err != nil {
			return fmt.Errorf("failed to add scheduling v2alpha2 to scheme: %w", err)
		}
		if err = v2.AddToScheme(controllerClient.Scheme()); err != nil {
			return fmt.Errorf("failed to add scheduling v2 to scheme: %w", err)
		}
		if err = kubeAiSchedulerV2alpha2.AddToScheme(controllerClient.Scheme()); err != nil {
			return fmt.Errorf("failed to add KubeAiScheduler scheduling v2alpha2 to scheme: %w", err)
		}
		if err = kwokopv1beta1.AddToScheme(controllerClient.Scheme()); err != nil {
			return fmt.Errorf("failed to add scheduling v1beta1 to scheme: %w", err)
		}
		if err = lws.AddToScheme(controllerClient.Scheme()); err != nil {
			return fmt.Errorf("failed to add lws to scheme: %w", err)
		}
		if err = kwokv1alpha1.AddToScheme(controllerClient.Scheme()); err != nil {
			return fmt.Errorf("failed to add kwok v1alpha1 to scheme: %w", err)
		}
	}

	return nil
}

func initKubeConfig() error {
	config, err := clientcmd.BuildConfigFromFlags("", getKubeConfigPath())
	if err != nil {
		return err
	}

	// Update throttling parameters matching to the current (v1.25) kubectl
	config.QPS = 50
	config.Burst = 300

	kubeConfig = config
	return nil
}

func validateClientSets() error {
	_, err := kubeClientset.ServerVersion()
	if err != nil {
		return fmt.Errorf("failed connectivity check on kube client-set. Inner error: %w", err)
	}

	_, err = kubeAiSchedClientset.ServerVersion()
	if err != nil {
		return fmt.Errorf("failed connectivity check on KubeAiScheduler client-set. Inner error: %w", err)
	}

	return nil
}

func getKubeConfigPath() string {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = fmt.Sprintf("%s/.kube/config", os.Getenv("HOME"))
	}
	kubeconfig = strings.Split(kubeconfig, ":")[0]

	return kubeconfig
}

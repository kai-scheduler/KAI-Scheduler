/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package rd

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/testconfig"
)

func CreateNamespaceObject(name, queueName string) *corev1.Namespace {
	cfg := testconfig.GetConfig()
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"project":              queueName,
				cfg.QueueLabelKey:      queueName,
				constants.AppLabelName: "engine-e2e",
			},
		},
	}
}

func GetE2ENamespaces(ctx context.Context, kubeClient *kubernetes.Clientset) (*corev1.NamespaceList, error) {
	return kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=engine-e2e", constants.AppLabelName),
	})
}

func DeleteNamespace(ctx context.Context, kubeClient *kubernetes.Clientset, namespace string) error {
	err := kubeClient.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	return client.IgnoreNotFound(err)
}

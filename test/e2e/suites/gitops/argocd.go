/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package gitops

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	argoNamespace = "argocd"
	appName       = "kai-scheduler"
	kaiNamespace  = "kai-scheduler"
	// Cascades resource deletion (and the chart's PostDelete cleanup hook)
	// when the Application is deleted.
	argoResourcesFinalizer = "resources-finalizer.argocd.argoproj.io"
	// In-cluster static helm repo deployed by hack/setup-gitops-e2e.sh.
	chartRepoURL = "http://chart-repo.chart-repo.svc.cluster.local"

	statusPollInterval = 5 * time.Second
)

var applicationGVK = schema.GroupVersionKind{
	Group:   "argoproj.io",
	Version: "v1alpha1",
	Kind:    "Application",
}

// newRawClient builds a controller-runtime client independent of
// testcontext.GetConnectivity, whose preflight lists Queues and therefore
// fails before the Application has installed the KAI CRDs.
func newRawClient() runtimeClient.Client {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = fmt.Sprintf("%s/.kube/config", os.Getenv("HOME"))
	}
	kubeconfig = strings.Split(kubeconfig, ":")[0]

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).NotTo(HaveOccurred(), "failed to load kubeconfig")

	c, err := runtimeClient.New(config, runtimeClient.Options{})
	Expect(err).NotTo(HaveOccurred(), "failed to create client")
	return c
}

// kaiApplication mirrors the Application example in docs/gitops/README.md,
// plus e2e-cluster values (local registry, gpu sharing, prometheus).
func kaiApplication(chartVersion string) *unstructured.Unstructured {
	app := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":       appName,
				"namespace":  argoNamespace,
				"finalizers": []interface{}{argoResourcesFinalizer},
			},
			"spec": map[string]interface{}{
				"project": "default",
				"source": map[string]interface{}{
					"repoURL":        chartRepoURL,
					"chart":          "kai-scheduler",
					"targetRevision": chartVersion,
					"helm": map[string]interface{}{
						"valuesObject": map[string]interface{}{
							"kaiConfigDeployer": map[string]interface{}{"enabled": false},
							"kaiConfig":         map[string]interface{}{"render": true},
							"global": map[string]interface{}{
								"registry":   "localhost:30100",
								"gpuSharing": true,
							},
							"prometheus": map[string]interface{}{"enabled": true},
						},
					},
				},
				"destination": map[string]interface{}{
					"server":    "https://kubernetes.default.svc",
					"namespace": kaiNamespace,
				},
				"syncPolicy": map[string]interface{}{
					"automated": map[string]interface{}{
						"prune":    true,
						"selfHeal": true,
					},
					"syncOptions": []interface{}{
						"CreateNamespace=true",
						"ServerSideApply=true",
					},
					"retry": map[string]interface{}{
						"limit": int64(3),
						"backoff": map[string]interface{}{
							"duration": "10s",
							"factor":   int64(2),
						},
					},
				},
			},
		},
	}
	app.SetGroupVersionKind(applicationGVK)
	return app
}

func getApplication(ctx context.Context, c runtimeClient.Client) (*unstructured.Unstructured, error) {
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(applicationGVK)
	err := c.Get(ctx, runtimeClient.ObjectKey{Namespace: argoNamespace, Name: appName}, app)
	return app, err
}

func waitForAppSyncedHealthy(ctx context.Context, c runtimeClient.Client, timeout time.Duration) {
	EventuallyWithOffset(1, func(g Gomega) {
		app, err := getApplication(ctx, c)
		g.Expect(err).NotTo(HaveOccurred())

		syncStatus, _, err := unstructured.NestedString(app.Object, "status", "sync", "status")
		g.Expect(err).NotTo(HaveOccurred())
		healthStatus, _, err := unstructured.NestedString(app.Object, "status", "health", "status")
		g.Expect(err).NotTo(HaveOccurred())

		opPhase, _, _ := unstructured.NestedString(app.Object, "status", "operationState", "phase")
		opMessage, _, _ := unstructured.NestedString(app.Object, "status", "operationState", "message")
		conditions, _, _ := unstructured.NestedSlice(app.Object, "status", "conditions")

		g.Expect(syncStatus).To(Equal("Synced"),
			"Expected Application to be Synced. operation=%s: %s conditions=%v", opPhase, opMessage, conditions)
		g.Expect(healthStatus).To(Equal("Healthy"),
			"Expected Application to be Healthy. operation=%s: %s conditions=%v", opPhase, opMessage, conditions)
	}, timeout, statusPollInterval).Should(Succeed())
}

func waitForAppGone(ctx context.Context, c runtimeClient.Client, timeout time.Duration) {
	EventuallyWithOffset(1, func(g Gomega) {
		_, err := getApplication(ctx, c)
		g.Expect(errors.IsNotFound(err)).To(BeTrue(),
			"Expected Application to be deleted (finalizer removal implies the PostDelete hook succeeded)")
	}, timeout, statusPollInterval).Should(Succeed())
}

// stripApplicationFinalizer force-unblocks Application deletion so a failed
// spec cannot wedge the CI job on ArgoCD's finalizers (the resources
// finalizer, or the post-delete hook finalizers ArgoCD adds on deletion).
func stripApplicationFinalizer(ctx context.Context, c runtimeClient.Client) {
	app, err := getApplication(ctx, c)
	if err != nil {
		return
	}
	patch := []byte(`{"metadata":{"finalizers":null}}`)
	_ = c.Patch(ctx, app, runtimeClient.RawPatch(types.MergePatchType, patch))
}

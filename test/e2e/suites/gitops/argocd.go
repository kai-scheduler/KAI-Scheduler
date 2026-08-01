/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package gitops

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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

// kaiApplication mirrors the Application example in docs/gitops/README.md,
// plus e2e-cluster values (gpu sharing, prometheus). GITOPS_KAI_REGISTRY
// overrides the chart's image registry when the images were loaded into the
// in-cluster registry (CI, local --local-images-build runs); when unset the
// chart default (the release registry) is used.
func kaiApplication(chartVersion string) *unstructured.Unstructured {
	global := map[string]interface{}{
		"gpuSharing": true,
	}
	if registry := os.Getenv("GITOPS_KAI_REGISTRY"); registry != "" {
		global["registry"] = registry
	}
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
							"global":            global,
							"prometheus":        map[string]interface{}{"enabled": true},
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

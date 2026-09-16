// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
)

const configManifest = `apiVersion: kai.scheduler/v1
kind: Config
metadata:
  name: kai-config
  labels:
    applied-by: test
`

func configScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, kaiv1.AddToScheme(scheme))
	return scheme
}

func writeManifest(t *testing.T, content string) string {
	path := filepath.Join(t.TempDir(), "kai-config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestApplyConfig_CreatesConfig(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(configScheme(t)).Build()

	require.NoError(t, ApplyConfig(ctx, c, writeManifest(t, configManifest)))

	got := &kaiv1.Config{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "kai-config"}, got))
	assert.Equal(t, "test", got.Labels["applied-by"])
}

func TestApplyConfig_StripsHelmHookAnnotations(t *testing.T) {
	ctx := context.Background()
	existing := &kaiv1.Config{ObjectMeta: metav1.ObjectMeta{
		Name: "kai-config",
		Annotations: map[string]string{
			"helm.sh/hook":        "pre-install,pre-upgrade",
			"helm.sh/hook-weight": "1",
			"other":               "kept",
		},
	}}
	c := fake.NewClientBuilder().WithScheme(configScheme(t)).WithObjects(existing).Build()

	require.NoError(t, ApplyConfig(ctx, c, writeManifest(t, configManifest)))

	got := &kaiv1.Config{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "kai-config"}, got))
	assert.NotContains(t, got.Annotations, "helm.sh/hook")
	assert.NotContains(t, got.Annotations, "helm.sh/hook-weight")
	assert.Equal(t, "kept", got.Annotations["other"])
	assert.Equal(t, "test", got.Labels["applied-by"])
}

func TestApplyConfig_RejectsWrongKind(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(configScheme(t)).Build()
	manifest := writeManifest(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: kai-config\n")

	err := ApplyConfig(context.Background(), c, manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `kind "ConfigMap"`)
}

func TestApplyConfig_RejectsMissingName(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(configScheme(t)).Build()
	manifest := writeManifest(t, "apiVersion: kai.scheduler/v1\nkind: Config\nmetadata: {}\n")

	err := ApplyConfig(context.Background(), c, manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metadata.name")
}

func TestApplyConfig_MissingFile(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(configScheme(t)).Build()
	require.Error(t, ApplyConfig(context.Background(), c, filepath.Join(t.TempDir(), "missing.yaml")))
}

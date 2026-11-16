// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	usagedbapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaiv1scheduler "github.com/kai-scheduler/api/kai/v1/scheduler"
)

const usageDBAuthTestNamespace = "kai-scheduler"

func usageDBSecretKey(name, key string) *corev1.SecretKeySelector {
	return &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  key,
	}
}

func usageDBSecret(name string, keys ...string) *corev1.Secret {
	data := map[string][]byte{}
	for _, key := range keys {
		data[key] = []byte("value")
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: usageDBAuthTestNamespace},
		Data:       data,
	}
}

func usageDBAuthTestConfig() *kaiv1.Config {
	config := &kaiv1.Config{Spec: kaiv1.ConfigSpec{
		Global:    &kaiv1.GlobalConfig{},
		Namespace: usageDBAuthTestNamespace,
		Scheduler: &kaiv1scheduler.Scheduler{},
	}}
	config.Spec.SetDefaultsWhereNeeded()
	return config
}

func usageDBAuthTestShard(auth *usagedbapi.Auth) *kaiv1.SchedulingShard {
	shard := &kaiv1.SchedulingShard{Spec: kaiv1.SchedulingShardSpec{
		UsageDBConfig: &usagedbapi.UsageDBConfig{
			ClientType:       "prometheus",
			ConnectionString: "https://metrics.example.com/prometheus",
			Auth:             auth,
		},
	}}
	shard.Spec.SetDefaultsWhereNeeded()
	return shard
}

func TestUsageDBAuthVolume(t *testing.T) {
	t.Run("no volume without Secret references", func(t *testing.T) {
		for name, auth := range map[string]*usagedbapi.Auth{
			"nil auth":   nil,
			"empty auth": {},
			"files only": {BasicAuth: &usagedbapi.BasicAuth{UsernameFile: "/vault/username", PasswordFile: "/vault/password"}},
		} {
			volume, mount := usageDBAuthVolume(auth)
			assert.Nil(t, volume, name)
			assert.Nil(t, mount, name)
		}
	})

	t.Run("basic auth projects each Secret key to its file", func(t *testing.T) {
		volume, mount := usageDBAuthVolume(&usagedbapi.Auth{BasicAuth: &usagedbapi.BasicAuth{
			Username: usageDBSecretKey("usage-db-user", "user"),
			Password: usageDBSecretKey("usage-db-password", "pass"),
		}})
		require.NotNil(t, volume)
		require.NotNil(t, mount)

		assert.Equal(t, corev1.VolumeMount{
			Name: usageDBAuthVolumeName, MountPath: usageDBAuthMountPath, ReadOnly: true,
		}, *mount)
		assert.Equal(t, usageDBAuthVolumeName, volume.Name)
		require.NotNil(t, volume.Projected)
		assert.Equal(t, []corev1.VolumeProjection{
			{Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: "usage-db-user"},
				Items:                []corev1.KeyToPath{{Key: "user", Path: usageDBAuthUsernameFile}},
			}},
			{Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: "usage-db-password"},
				Items:                []corev1.KeyToPath{{Key: "pass", Path: usageDBAuthPasswordFile}},
			}},
		}, volume.Projected.Sources)
	})

	t.Run("authorization projects the credentials and keeps optional", func(t *testing.T) {
		credentials := usageDBSecretKey("usage-db", "token")
		credentials.Optional = ptr.To(true)

		volume, _ := usageDBAuthVolume(&usagedbapi.Auth{
			Authorization: &usagedbapi.Authorization{Credentials: credentials},
		})
		require.NotNil(t, volume)
		assert.Equal(t, []corev1.VolumeProjection{
			{Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: "usage-db"},
				Items:                []corev1.KeyToPath{{Key: "token", Path: usageDBAuthCredentialsFile}},
				Optional:             ptr.To(true),
			}},
		}, volume.Projected.Sources)
	})
}

func TestUsageDBAuthWithMountedFiles(t *testing.T) {
	assert.Nil(t, usageDBAuthWithMountedFiles(nil))

	t.Run("Secret references become mounted file paths", func(t *testing.T) {
		original := &usagedbapi.Auth{BasicAuth: &usagedbapi.BasicAuth{
			Username: usageDBSecretKey("usage-db", "username"),
			Password: usageDBSecretKey("usage-db", "password"),
		}}

		mounted := usageDBAuthWithMountedFiles(original)
		assert.Equal(t, &usagedbapi.Auth{BasicAuth: &usagedbapi.BasicAuth{
			UsernameFile: "/etc/usage-db-auth/username",
			PasswordFile: "/etc/usage-db-auth/password",
		}}, mounted)
		assert.NotNil(t, original.BasicAuth.Username, "the shard's auth must not be modified")
		assert.NoError(t, mounted.Validate())
	})

	t.Run("authorization keeps its type", func(t *testing.T) {
		mounted := usageDBAuthWithMountedFiles(&usagedbapi.Auth{Authorization: &usagedbapi.Authorization{
			Type:        "Token",
			Credentials: usageDBSecretKey("usage-db", "token"),
		}})
		assert.Equal(t, &usagedbapi.Auth{Authorization: &usagedbapi.Authorization{
			Type:            "Token",
			CredentialsFile: "/etc/usage-db-auth/credentials",
		}}, mounted)
	})

	t.Run("file paths set directly are kept", func(t *testing.T) {
		auth := &usagedbapi.Auth{BasicAuth: &usagedbapi.BasicAuth{
			UsernameFile: "/vault/username",
			Password:     usageDBSecretKey("usage-db", "password"),
		}}
		assert.Equal(t, &usagedbapi.Auth{BasicAuth: &usagedbapi.BasicAuth{
			UsernameFile: "/vault/username",
			PasswordFile: "/etc/usage-db-auth/password",
		}}, usageDBAuthWithMountedFiles(auth))
	})
}

func TestValidateUsageDBAuthSecrets(t *testing.T) {
	basicAuth := &usagedbapi.Auth{BasicAuth: &usagedbapi.BasicAuth{
		Username: usageDBSecretKey("usage-db", "username"),
		Password: usageDBSecretKey("usage-db", "password"),
	}}
	optionalCredentials := usageDBSecretKey("missing", "token")
	optionalCredentials.Optional = ptr.To(true)

	tests := []struct {
		name    string
		auth    *usagedbapi.Auth
		secrets []*corev1.Secret
		wantErr string
	}{
		{name: "no auth", auth: nil},
		{
			name:    "all keys exist",
			auth:    basicAuth,
			secrets: []*corev1.Secret{usageDBSecret("usage-db", "username", "password")},
		},
		{
			name:    "secret is missing",
			auth:    basicAuth,
			wantErr: "usageDBConfig.auth.basicAuth.username: failed to get secret kai-scheduler/usage-db",
		},
		{
			name:    "key is missing",
			auth:    basicAuth,
			secrets: []*corev1.Secret{usageDBSecret("usage-db", "username")},
			wantErr: `usageDBConfig.auth.basicAuth.password: secret kai-scheduler/usage-db has no key "password"`,
		},
		{
			name: "an optional reference is not checked",
			auth: &usagedbapi.Auth{Authorization: &usagedbapi.Authorization{Credentials: optionalCredentials}},
		},
		{
			name: "files need no Secret",
			auth: &usagedbapi.Auth{Authorization: &usagedbapi.Authorization{CredentialsFile: "/vault/token"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder()
			for _, secret := range tt.secrets {
				builder = builder.WithObjects(secret)
			}

			err := validateUsageDBAuthSecrets(context.Background(), builder.Build(), usageDBAuthTestNamespace, tt.auth)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestDeploymentForShardUsageDBAuth(t *testing.T) {
	ctx := context.Background()
	config := usageDBAuthTestConfig()
	auth := &usagedbapi.Auth{BasicAuth: &usagedbapi.BasicAuth{
		Username: usageDBSecretKey("usage-db", "username"),
		Password: usageDBSecretKey("usage-db", "password"),
	}}

	t.Run("mounts the referenced Secret keys", func(t *testing.T) {
		shard := usageDBAuthTestShard(auth)
		client := fake.NewClientBuilder().WithObjects(usageDBSecret("usage-db", "username", "password")).Build()

		deployment, err := NewSchedulerForShard(shard).deploymentForShard(ctx, client, config, shard)
		require.NoError(t, err)
		podSpec := deployment.(*appsv1.Deployment).Spec.Template.Spec

		require.Len(t, podSpec.Volumes, 2)
		assert.Equal(t, "config", podSpec.Volumes[0].Name)
		assert.Equal(t, usageDBAuthVolumeName, podSpec.Volumes[1].Name)
		require.NotNil(t, podSpec.Volumes[1].Projected)
		assert.Len(t, podSpec.Volumes[1].Projected.Sources, 2)

		mounts := podSpec.Containers[0].VolumeMounts
		require.Len(t, mounts, 2)
		assert.Equal(t, corev1.VolumeMount{
			Name: usageDBAuthVolumeName, MountPath: usageDBAuthMountPath, ReadOnly: true,
		}, mounts[1])
	})

	t.Run("a missing Secret fails instead of producing a pod that cannot start", func(t *testing.T) {
		shard := usageDBAuthTestShard(auth)
		client := fake.NewClientBuilder().Build()

		_, err := NewSchedulerForShard(shard).deploymentForShard(ctx, client, config, shard)
		assert.ErrorContains(t, err, "failed to get secret kai-scheduler/usage-db")
	})

	t.Run("an invalid auth fails", func(t *testing.T) {
		shard := usageDBAuthTestShard(&usagedbapi.Auth{
			BasicAuth:     &usagedbapi.BasicAuth{Username: usageDBSecretKey("usage-db", "username")},
			Authorization: &usagedbapi.Authorization{Credentials: usageDBSecretKey("usage-db", "token")},
		})
		client := fake.NewClientBuilder().Build()

		_, err := NewSchedulerForShard(shard).deploymentForShard(ctx, client, config, shard)
		assert.ErrorContains(t, err, "invalid usageDBConfig.auth: basicAuth and authorization are mutually exclusive")
	})

	t.Run("no auth adds no volume", func(t *testing.T) {
		shard := usageDBAuthTestShard(nil)
		client := fake.NewClientBuilder().Build()

		deployment, err := NewSchedulerForShard(shard).deploymentForShard(ctx, client, config, shard)
		require.NoError(t, err)
		podSpec := deployment.(*appsv1.Deployment).Spec.Template.Spec
		assert.Len(t, podSpec.Volumes, 1)
		assert.Len(t, podSpec.Containers[0].VolumeMounts, 1)
	})
}

func TestConfigMapForShardUsageDBAuth(t *testing.T) {
	ctx := context.Background()
	config := usageDBAuthTestConfig()
	client := fake.NewClientBuilder().Build()

	t.Run("the scheduler config names files, not Secrets", func(t *testing.T) {
		shard := usageDBAuthTestShard(&usagedbapi.Auth{Authorization: &usagedbapi.Authorization{
			Credentials: usageDBSecretKey("usage-db", "token"),
		}})

		configMap, err := NewSchedulerForShard(shard).configMapForShard(ctx, client, config, shard)
		require.NoError(t, err)
		data := configMap.(*corev1.ConfigMap).Data["config.yaml"]
		assert.NotContains(t, data, "usage-db\n", "the Secret name must not reach the ConfigMap")

		var schedulerConfig conf.SchedulerConfiguration
		require.NoError(t, yaml.Unmarshal([]byte(data), &schedulerConfig))
		require.NotNil(t, schedulerConfig.UsageDBConfig)
		assert.Equal(t, &usagedbapi.Auth{Authorization: &usagedbapi.Authorization{
			CredentialsFile: "/etc/usage-db-auth/credentials",
		}}, schedulerConfig.UsageDBConfig.Auth)
		assert.NotNil(t, shard.Spec.UsageDBConfig.Auth.Authorization.Credentials,
			"the shard's own auth must keep its Secret reference")
	})

	t.Run("an invalid auth fails", func(t *testing.T) {
		shard := usageDBAuthTestShard(&usagedbapi.Auth{Authorization: &usagedbapi.Authorization{Type: "Bearer"}})

		_, err := NewSchedulerForShard(shard).configMapForShard(ctx, client, config, shard)
		assert.ErrorContains(t, err,
			"invalid usageDBConfig.auth: authorization: one of credentials and credentialsFile is required")
	})
}

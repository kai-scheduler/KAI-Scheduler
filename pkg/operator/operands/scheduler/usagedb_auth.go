// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"
	"fmt"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	usagedbapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
)

const (
	usageDBAuthVolumeName = "usage-db-auth"
	usageDBAuthMountPath  = "/etc/usage-db-auth"

	usageDBAuthUsernameFile    = "username"
	usageDBAuthPasswordFile    = "password"
	usageDBAuthCredentialsFile = "credentials"
)

// usageDBAuthSecretSource is a credential of the usage db client that comes
// from a Secret key, with the name of the file that key is mounted as.
type usageDBAuthSecretSource struct {
	field    string
	selector *corev1.SecretKeySelector
	fileName string
}

func usageDBAuthSecretSources(auth *usagedbapi.Auth) []usageDBAuthSecretSource {
	if auth == nil {
		return nil
	}

	var sources []usageDBAuthSecretSource
	if basicAuth := auth.BasicAuth; basicAuth != nil {
		if basicAuth.Username != nil {
			sources = append(sources, usageDBAuthSecretSource{
				field: "basicAuth.username", selector: basicAuth.Username, fileName: usageDBAuthUsernameFile,
			})
		}
		if basicAuth.Password != nil {
			sources = append(sources, usageDBAuthSecretSource{
				field: "basicAuth.password", selector: basicAuth.Password, fileName: usageDBAuthPasswordFile,
			})
		}
	}
	if authorization := auth.Authorization; authorization != nil && authorization.Credentials != nil {
		sources = append(sources, usageDBAuthSecretSource{
			field: "authorization.credentials", selector: authorization.Credentials,
			fileName: usageDBAuthCredentialsFile,
		})
	}
	return sources
}

// usageDBAuthVolume projects the Secret keys the usage db client authenticates
// with into one read-only directory of the scheduler container. It returns nil
// when no credential comes from a Secret.
//
// The keys are mounted as files, not passed as environment variables, so the
// kubelet updates them when a Secret is rotated and the scheduler, which reads
// them on every request, needs no restart.
func usageDBAuthVolume(auth *usagedbapi.Auth) (*corev1.Volume, *corev1.VolumeMount) {
	sources := usageDBAuthSecretSources(auth)
	if len(sources) == 0 {
		return nil, nil
	}

	projections := make([]corev1.VolumeProjection, 0, len(sources))
	for _, source := range sources {
		projections = append(projections, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: source.selector.Name},
				Items:                []corev1.KeyToPath{{Key: source.selector.Key, Path: source.fileName}},
				Optional:             source.selector.Optional,
			},
		})
	}

	volume := &corev1.Volume{
		Name: usageDBAuthVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: projections,
			},
		},
	}
	mount := &corev1.VolumeMount{
		Name:      usageDBAuthVolumeName,
		MountPath: usageDBAuthMountPath,
		ReadOnly:  true,
	}
	return volume, mount
}

// usageDBAuthWithMountedFiles returns a copy of auth for the scheduler
// configuration: every Secret reference is replaced by the path its key is
// mounted at, so the ConfigMap names files and holds no reference to a Secret.
// A file path that was set directly is kept.
func usageDBAuthWithMountedFiles(auth *usagedbapi.Auth) *usagedbapi.Auth {
	mounted := auth.DeepCopy()
	if mounted == nil {
		return nil
	}

	if basicAuth := mounted.BasicAuth; basicAuth != nil {
		if basicAuth.Username != nil {
			basicAuth.Username = nil
			basicAuth.UsernameFile = path.Join(usageDBAuthMountPath, usageDBAuthUsernameFile)
		}
		if basicAuth.Password != nil {
			basicAuth.Password = nil
			basicAuth.PasswordFile = path.Join(usageDBAuthMountPath, usageDBAuthPasswordFile)
		}
	}
	if authorization := mounted.Authorization; authorization != nil && authorization.Credentials != nil {
		authorization.Credentials = nil
		authorization.CredentialsFile = path.Join(usageDBAuthMountPath, usageDBAuthCredentialsFile)
	}
	return mounted
}

// validateUsageDBAuthSecrets fails when a Secret key the usage db client
// authenticates with does not exist. The scheduler Deployment is recreated on
// every change, and a pod that cannot mount its volume never starts, so a
// mistyped Secret name would leave the shard without a scheduler. Failing the
// reconcile instead keeps the running scheduler until the reference is fixed.
// A reference marked optional is not checked.
func validateUsageDBAuthSecrets(
	ctx context.Context, readerClient client.Reader, namespace string, auth *usagedbapi.Auth,
) error {
	for _, source := range usageDBAuthSecretSources(auth) {
		if ptr.Deref(source.selector.Optional, false) {
			continue
		}

		secret := &corev1.Secret{}
		key := types.NamespacedName{Namespace: namespace, Name: source.selector.Name}
		if err := readerClient.Get(ctx, key, secret); err != nil {
			return fmt.Errorf("usageDBConfig.auth.%s: failed to get secret %s: %w", source.field, key, err)
		}
		if _, found := secret.Data[source.selector.Key]; !found {
			return fmt.Errorf("usageDBConfig.auth.%s: secret %s has no key %q",
				source.field, key, source.selector.Key)
		}
	}
	return nil
}

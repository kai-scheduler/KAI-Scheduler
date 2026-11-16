// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package prometheus

import (
	"errors"
	"net/http"

	promconfig "github.com/prometheus/common/config"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
)

// newAuthRoundTripper wraps next so that every request carries the credentials
// auth names. With no auth it returns next unchanged.
//
// The credentials are read from files on every request, so a rotated Secret
// takes effect without a restart. The scheduler reads files only: a Secret
// reference is the operator's to resolve, by mounting the key and writing the
// file path into the scheduler configuration.
func newAuthRoundTripper(auth *api.Auth, next http.RoundTripper) (http.RoundTripper, error) {
	if auth == nil {
		return next, nil
	}
	if err := auth.Validate(); err != nil {
		return nil, err
	}

	if basicAuth := auth.BasicAuth; basicAuth != nil {
		if basicAuth.Username != nil || basicAuth.Password != nil {
			return nil, errors.New("basicAuth names a Secret, but the scheduler reads credentials from files: " +
				"set usernameFile and passwordFile, or let the operator mount the Secret")
		}
		// A nil reader, not an empty file path: the password is optional.
		var password promconfig.SecretReader
		if basicAuth.PasswordFile != "" {
			password = promconfig.NewFileSecret(basicAuth.PasswordFile)
		}
		return promconfig.NewBasicAuthRoundTripper(
			promconfig.NewFileSecret(basicAuth.UsernameFile), password, next), nil
	}

	if authorization := auth.Authorization; authorization != nil {
		if authorization.Credentials != nil {
			return nil, errors.New("authorization names a Secret, but the scheduler reads credentials from files: " +
				"set credentialsFile, or let the operator mount the Secret")
		}
		return promconfig.NewAuthorizationCredentialsRoundTripper(
			authorization.GetType(), promconfig.NewFileSecret(authorization.CredentialsFile), next), nil
	}

	return next, nil
}

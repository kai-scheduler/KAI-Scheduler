// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package prometheus

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
)

// authRecorder is an HTTP server that answers every Prometheus query with one
// sample and records the Authorization header of each request.
type authRecorder struct {
	server *httptest.Server
	mutex  sync.Mutex
	seen   []string
}

func newAuthRecorder(t *testing.T) *authRecorder {
	recorder := &authRecorder{}
	recorder.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.mutex.Lock()
		recorder.seen = append(recorder.seen, r.Header.Get("Authorization"))
		recorder.mutex.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[` +
			`{"metric":{"queue_name":"team-a"},"value":[1700000000,"1"]}]}}`))
	}))
	t.Cleanup(recorder.server.Close)
	return recorder
}

func (r *authRecorder) headers() []string {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return append([]string(nil), r.seen...)
}

func (r *authRecorder) get(t *testing.T, roundTripper http.RoundTripper) {
	response, err := (&http.Client{Transport: roundTripper}).Get(r.server.URL)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
}

func writeSecretFile(t *testing.T, name, content string) string {
	file := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	return file
}

func TestNewAuthRoundTripper(t *testing.T) {
	tests := []struct {
		name     string
		auth     func(t *testing.T) *api.Auth
		expected string
	}{
		{
			name:     "no auth sends no header",
			auth:     func(*testing.T) *api.Auth { return nil },
			expected: "",
		},
		{
			name:     "empty auth sends no header",
			auth:     func(*testing.T) *api.Auth { return &api.Auth{} },
			expected: "",
		},
		{
			name: "basic auth",
			auth: func(t *testing.T) *api.Auth {
				return &api.Auth{BasicAuth: &api.BasicAuth{
					UsernameFile: writeSecretFile(t, "username", "reader\n"),
					PasswordFile: writeSecretFile(t, "password", "s3cret\n"),
				}}
			},
			expected: "Basic cmVhZGVyOnMzY3JldA==", // reader:s3cret
		},
		{
			name: "basic auth without a password",
			auth: func(t *testing.T) *api.Auth {
				return &api.Auth{BasicAuth: &api.BasicAuth{UsernameFile: writeSecretFile(t, "username", "reader")}}
			},
			expected: "Basic cmVhZGVyOg==", // reader:
		},
		{
			name: "bearer token is the default authorization type",
			auth: func(t *testing.T) *api.Auth {
				return &api.Auth{Authorization: &api.Authorization{
					CredentialsFile: writeSecretFile(t, "token", "abc123\n"),
				}}
			},
			expected: "Bearer abc123",
		},
		{
			name: "custom authorization type",
			auth: func(t *testing.T) *api.Auth {
				return &api.Auth{Authorization: &api.Authorization{
					Type:            "Token",
					CredentialsFile: writeSecretFile(t, "token", "abc123"),
				}}
			},
			expected: "Token abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := newAuthRecorder(t)

			roundTripper, err := newAuthRoundTripper(tt.auth(t), http.DefaultTransport)
			require.NoError(t, err)
			recorder.get(t, roundTripper)

			assert.Equal(t, []string{tt.expected}, recorder.headers())
		})
	}
}

func TestNewAuthRoundTripper_RotatedCredentials(t *testing.T) {
	recorder := newAuthRecorder(t)
	tokenFile := writeSecretFile(t, "token", "first")

	roundTripper, err := newAuthRoundTripper(
		&api.Auth{Authorization: &api.Authorization{CredentialsFile: tokenFile}}, http.DefaultTransport)
	require.NoError(t, err)

	recorder.get(t, roundTripper)
	require.NoError(t, os.WriteFile(tokenFile, []byte("second"), 0o600))
	recorder.get(t, roundTripper)

	assert.Equal(t, []string{"Bearer first", "Bearer second"}, recorder.headers())
}

func TestNewAuthRoundTripper_Rejected(t *testing.T) {
	secretKey := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "usage-db"},
		Key:                  "value",
	}

	tests := []struct {
		name    string
		auth    *api.Auth
		wantErr string
	}{
		{
			name:    "basic auth that still names a Secret",
			auth:    &api.Auth{BasicAuth: &api.BasicAuth{Username: secretKey}},
			wantErr: "basicAuth names a Secret",
		},
		{
			name:    "authorization that still names a Secret",
			auth:    &api.Auth{Authorization: &api.Authorization{Credentials: secretKey}},
			wantErr: "authorization names a Secret",
		},
		{
			name: "both forms",
			auth: &api.Auth{
				BasicAuth:     &api.BasicAuth{UsernameFile: "/auth/username"},
				Authorization: &api.Authorization{CredentialsFile: "/auth/credentials"},
			},
			wantErr: "basicAuth and authorization are mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newAuthRoundTripper(tt.auth, http.DefaultTransport)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestNewPrometheusClient_Auth(t *testing.T) {
	params := &api.UsageParams{}
	params.SetDefaults()

	t.Run("every usage query carries the credentials", func(t *testing.T) {
		recorder := newAuthRecorder(t)
		auth := &api.Auth{Authorization: &api.Authorization{CredentialsFile: writeSecretFile(t, "token", "abc123")}}

		client, err := NewPrometheusClient(recorder.server.URL, params, auth)
		require.NoError(t, err)
		_, err = client.GetResourceUsage()
		require.NoError(t, err)

		headers := recorder.headers()
		require.NotEmpty(t, headers)
		for _, header := range headers {
			assert.Equal(t, "Bearer abc123", header)
		}
	})

	t.Run("an unreadable credentials file fails the fetch, not the client", func(t *testing.T) {
		recorder := newAuthRecorder(t)
		auth := &api.Auth{Authorization: &api.Authorization{
			CredentialsFile: filepath.Join(t.TempDir(), "missing"),
		}}

		client, err := NewPrometheusClient(recorder.server.URL, params, auth)
		require.NoError(t, err)
		_, err = client.GetResourceUsage()
		assert.ErrorContains(t, err, "unable to read authorization credentials")
		assert.Empty(t, recorder.headers(), "no request may be sent without its credentials")
	})

	t.Run("an auth the scheduler cannot use fails the client", func(t *testing.T) {
		_, err := NewPrometheusClient("http://localhost:9090", params, &api.Auth{
			BasicAuth: &api.BasicAuth{Username: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "usage-db"},
				Key:                  "username",
			}},
		})
		assert.ErrorContains(t, err, "error configuring prometheus client authentication")
	})
}

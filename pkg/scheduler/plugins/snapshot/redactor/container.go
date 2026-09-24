// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	corev1 "k8s.io/api/core/v1"
)

// redactContainer redacts a container's image, name, commands, arguments,
// environment variables, secret and configmap references, and all probes.
// The containerPrefix distinguishes regular containers from init containers.
func (r *Redactor) redactContainer(container *corev1.Container, containerPrefix string) {
	if container == nil {
		return
	}

	container.Image = r.Obfuscate(container.Image, "image")
	container.Name = r.Obfuscate(container.Name, containerPrefix)

	for i := range container.Command {
		container.Command[i] = r.Obfuscate(container.Command[i], "cmdarg")
	}
	for i := range container.Args {
		container.Args[i] = r.Obfuscate(container.Args[i], "cmdarg")
	}

	r.redactEnvVars(container)
	r.redactEnvFrom(container)

	r.redactContainerProbe(container.LivenessProbe)
	r.redactContainerProbe(container.ReadinessProbe)
	r.redactContainerProbe(container.StartupProbe)
}

// redactEnvVars redacts inline env var values and secret/configmap key references.
// Env var names (e.g. API_KEY, DATABASE_URL) are preserved because they are
// well-known application constants and not sensitive in themselves.
func (r *Redactor) redactEnvVars(container *corev1.Container) {
	for i := range container.Env {
		if container.Env[i].Value != "" {
			container.Env[i].Value = r.Obfuscate(container.Env[i].Value, "envval")
			r.mu.Lock()
			r.stats.EnvVarsRedacted++
			r.mu.Unlock()
		}

		if container.Env[i].ValueFrom == nil {
			continue
		}

		if ref := container.Env[i].ValueFrom.SecretKeyRef; ref != nil {
			ref.Name = r.Obfuscate(ref.Name, "secret")
			ref.Key = r.Obfuscate(ref.Key, "secretkey")
			r.mu.Lock()
			r.stats.SecretsRedacted++
			r.mu.Unlock()
		}

		if ref := container.Env[i].ValueFrom.ConfigMapKeyRef; ref != nil {
			ref.Name = r.Obfuscate(ref.Name, "configmap")
			ref.Key = r.Obfuscate(ref.Key, "configkey")
			r.mu.Lock()
			r.stats.ConfigMapsRedacted++
			r.mu.Unlock()
		}
	}
}

// redactEnvFrom redacts secret and configmap names referenced via envFrom,
// which bulk-loads all keys from a secret or configmap as env vars.
func (r *Redactor) redactEnvFrom(container *corev1.Container) {
	for i := range container.EnvFrom {
		if container.EnvFrom[i].SecretRef != nil {
			container.EnvFrom[i].SecretRef.Name = r.Obfuscate(
				container.EnvFrom[i].SecretRef.Name, "secret",
			)
			r.mu.Lock()
			r.stats.SecretsRedacted++
			r.mu.Unlock()
		}
		if container.EnvFrom[i].ConfigMapRef != nil {
			container.EnvFrom[i].ConfigMapRef.Name = r.Obfuscate(
				container.EnvFrom[i].ConfigMapRef.Name, "configmap",
			)
			r.mu.Lock()
			r.stats.ConfigMapsRedacted++
			r.mu.Unlock()
		}
	}
}

// redactContainerProbe redacts HTTP paths, hosts, TCP hosts, and exec commands
// from liveness, readiness, and startup probes. A nil probe is a no-op.
func (r *Redactor) redactContainerProbe(probe *corev1.Probe) {
	if probe == nil {
		return
	}

	if probe.HTTPGet != nil {
		probe.HTTPGet.Path = r.Obfuscate(probe.HTTPGet.Path, "probepath")
		probe.HTTPGet.Host = r.Obfuscate(probe.HTTPGet.Host, "probehost")
		r.mu.Lock()
		r.stats.ProbesRedacted++
		r.mu.Unlock()
	}

	if probe.TCPSocket != nil {
		probe.TCPSocket.Host = r.Obfuscate(probe.TCPSocket.Host, "tcphost")
		r.mu.Lock()
		r.stats.ProbesRedacted++
		r.mu.Unlock()
	}

	if probe.Exec != nil {
		for i := range probe.Exec.Command {
			probe.Exec.Command[i] = r.Obfuscate(probe.Exec.Command[i], "probecmd")
		}
		r.mu.Lock()
		r.stats.ProbesRedacted++
		r.mu.Unlock()
	}
}

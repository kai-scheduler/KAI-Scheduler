#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
#
# Installs kai-resource-isolator with kai-vgpu-monitor enabled so hamicore e2e
# can scrape hami_* per-container VRAM metrics on :9394.
#
# Overrides (optional):
#   ISOLATOR_CHART_REF       OCI ref or local chart path
#                           (default: oci://docker.io/projecthami/kai-resource-isolator)
#   ISOLATOR_CHART_VERSION   Chart version when using OCI (default: 1.1.0-chart)
#   ISOLATOR_NAMESPACE       Install namespace (default: kai-resource-isolator)
#   ISOLATOR_RELEASE         Helm release name (default: kai-resource-isolator)
#   ISOLATOR_HELM_EXTRA_ARGS Extra args appended to helm upgrade (word-split)
set -euo pipefail

ISOLATOR_CHART_REF="${ISOLATOR_CHART_REF:-oci://docker.io/projecthami/kai-resource-isolator}"
ISOLATOR_CHART_VERSION="${ISOLATOR_CHART_VERSION:-1.1.0-chart}"
ISOLATOR_NAMESPACE="${ISOLATOR_NAMESPACE:-kai-resource-isolator}"
ISOLATOR_RELEASE="${ISOLATOR_RELEASE:-kai-resource-isolator}"

HELM_ARGS=(
  upgrade --install "${ISOLATOR_RELEASE}" "${ISOLATOR_CHART_REF}"
  --namespace "${ISOLATOR_NAMESPACE}"
  --create-namespace
  --set monitor.enabled=true
  --wait
  --timeout 5m
)

# --version only applies to OCI/repo charts, not a local filesystem chart path.
if [[ "${ISOLATOR_CHART_REF}" == oci://* ]] || [[ "${ISOLATOR_CHART_REF}" == *://* ]]; then
  HELM_ARGS+=(--version "${ISOLATOR_CHART_VERSION}")
fi

# shellcheck disable=SC2206
if [[ -n "${ISOLATOR_HELM_EXTRA_ARGS:-}" ]]; then
  EXTRA=( ${ISOLATOR_HELM_EXTRA_ARGS} )
  HELM_ARGS+=("${EXTRA[@]}")
fi

echo "Installing kai-resource-isolator from ${ISOLATOR_CHART_REF} (monitor.enabled=true)..."
helm "${HELM_ARGS[@]}"

echo "Waiting for isolator webhook deployment..."
kubectl -n "${ISOLATOR_NAMESPACE}" rollout status \
  "deployment/${ISOLATOR_RELEASE}-webhook" --timeout=180s

echo "kai-resource-isolator installed (mutating webhook + monitor chart resources)."

#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

KARTA_CRD_MANIFEST="${KARTA_CRD_MANIFEST:-}"

# Install only the upstream Karta CRD. The Karta e2e suite owns its test-specific
# workload CRD, RBAC, and pod-grouper restart/cleanup.
if [[ -z "${KARTA_CRD_MANIFEST}" ]]; then
  # Prefer the Karta CRD that matches the module version pinned by this repo.
  # This keeps the e2e cluster aligned with the API types compiled into KAI.
  KARTA_VERSION="$(go list -m -f '{{.Version}}' github.com/run-ai/karta)"
  KARTA_MOD_DIR="$(go env GOMODCACHE)/github.com/run-ai/karta@${KARTA_VERSION}"
  if [[ -f "${KARTA_MOD_DIR}/charts/karta/crds/run.ai_kartas.yaml" ]]; then
    KARTA_CRD_MANIFEST="${KARTA_MOD_DIR}/charts/karta/crds/run.ai_kartas.yaml"
  else
    # Fallback for environments where the module cache has not been populated.
    KARTA_CRD_MANIFEST="https://raw.githubusercontent.com/run-ai/karta/main/charts/karta/crds/run.ai_kartas.yaml"
  fi
fi

# Set KARTA_CRD_MANIFEST to test a specific local or remote CRD manifest.
kubectl apply -f "${KARTA_CRD_MANIFEST}"

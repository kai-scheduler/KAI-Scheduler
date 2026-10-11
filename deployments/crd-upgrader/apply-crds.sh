#!/bin/bash
# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Dry-run every CRD first: the API server rejects a CRD whose status.storedVersions lists a version
# the new CRD no longer declares, and a real apply would fail midway with the CRDs half upgraded.
if ! kubectl apply --server-side=true --force-conflicts --dry-run=server -f /internal-crds; then
  echo "CRD preflight failed, no CRD was changed. If the error above is status.storedVersions: re-save the" \
    "objects of that CRD (kubectl get <resource> -A -o yaml | kubectl replace -f -) and then run:" \
    "kubectl patch crd <crd> --subresource=status --type=json" \
    "-p='[{\"op\":\"replace\",\"path\":\"/status/storedVersions\",\"value\":[\"<current-storage-version>\"]}]'" >&2
  exit 1
fi

# Using --force-conflicts to claim ownership of the CRDs from helm
kubectl apply --server-side=true --force-conflicts -f /internal-crds

#!/bin/bash
# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Migrate Kueue Topology CRs to KAI Topology CRs
# This script is idempotent - it skips topologies that already exist in KAI

set -e

echo "Checking for Kueue Topology CRD..."

# Check if Kueue Topology CRD exists
if ! kubectl get crd topologies.kueue.x-k8s.io > /dev/null 2>&1; then
  echo "Kueue Topology CRD not found, skipping migration."
  exit 0
fi

echo "Kueue Topology CRD found. Starting migration..."

# Get all Kueue Topology names
TOPO_NAMES=$(kubectl get topologies.kueue.x-k8s.io -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

if [ -z "$TOPO_NAMES" ]; then
  echo "No Kueue Topologies found to migrate."
  exit 0
fi

echo "Found Kueue Topologies to migrate: $TOPO_NAMES"

# Process each topology
for TOPO_NAME in $TOPO_NAMES; do
  echo "Migrating topology: $TOPO_NAME"

  # Check if KAI Topology already exists
  if kubectl get topologies.kai.scheduler "$TOPO_NAME" > /dev/null 2>&1; then
    echo "KAI Topology '$TOPO_NAME' already exists, skipping."
    continue
  fi

  # Extract the levels spec using go-template and create KAI Topology
  kubectl get topologies.kueue.x-k8s.io "$TOPO_NAME" \
    -o go-template='apiVersion: kai.scheduler/v1
kind: Topology
metadata:
  name: {{.metadata.name}}
  annotations:
    kai.scheduler/migrated-from: "kueue.x-k8s.io"
spec:
  levels:
{{- range .spec.levels}}
  - nodeLabel: "{{.nodeLabel}}"
{{- end}}
' | kubectl apply -f -

  echo "Successfully migrated topology: $TOPO_NAME"
done

echo "Topology migration completed."


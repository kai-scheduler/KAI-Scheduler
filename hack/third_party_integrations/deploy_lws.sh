#!/bin/bash
# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
set -e

CHART_VERSION=0.7.0
helm install lws oci://registry.k8s.io/lws/charts/lws --version=$CHART_VERSION --namespace lws-system --create-namespace --wait --timeout 300s

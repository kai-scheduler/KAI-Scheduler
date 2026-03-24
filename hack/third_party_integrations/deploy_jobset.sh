#!/bin/bash
# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
set -e

kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/v0.10.1/manifests.yaml


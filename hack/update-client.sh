#!/usr/bin/env bash
# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Creating an import file got code-generator

cat <<EOF > generate-dep.go
package main

import (
	_ "k8s.io/code-generator"
)
EOF

go mod tidy
go mod vendor

SDK_HACK_DIR="$(cd "$(dirname "$(readlink "$0" || echo "$0")")"; pwd)"
source ${SDK_HACK_DIR}/../vendor/k8s.io/code-generator/kube_codegen.sh
kube::codegen::gen_client \
  --boilerplate ${SDK_HACK_DIR}/boilerplate.go.txt --with-watch \
  --output-dir ${SDK_HACK_DIR}/../pkg/apis/client --output-pkg github.com/kai-scheduler/KAI-scheduler/pkg/apis/client \
  ${SDK_HACK_DIR}/../pkg/apis

rm -f generate-dep.go && rm -r vendor && go mod tidy

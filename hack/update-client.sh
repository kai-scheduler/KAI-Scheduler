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
go mod download

SDK_HACK_DIR="$(cd "$(dirname "$(readlink "$0" || echo "$0")")"; pwd)"
CODEGEN_PKG=$(go list -m -f '{{.Dir}}' k8s.io/code-generator)
source ${CODEGEN_PKG}/kube_codegen.sh
kube::codegen::gen_client \
  --boilerplate ${SDK_HACK_DIR}/boilerplate.go.kb.txt \
  --with-watch \
  --output-dir ${SDK_HACK_DIR}/../pkg/apis/client \
  --output-pkg github.com/kai-scheduler/kai-scheduler/pkg/apis/client \
  ${SDK_HACK_DIR}/../pkg/apis

rm -f generate-dep.go && go mod tidy

changed_files=$(git diff --name-only | grep pkg/apis/client | grep v1alpha2)
${SDK_HACK_DIR}/replace_headers.sh \
  ${SDK_HACK_DIR}/boilerplate.go.txt \
  ${changed_files}

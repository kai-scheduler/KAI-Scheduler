#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# This script runs the GitOps (ArgoCD) e2e tests for the kai-scheduler.
# It reuses setup-e2e-cluster.sh to create a kind cluster WITHOUT KAI
# installed, installs ArgoCD and an in-cluster chart repo via
# setup-gitops-e2e.sh, then runs tests that install KAI through an ArgoCD
# Application (see test/e2e/suites/gitops and docs/gitops/README.md).

set -e

CLUSTER_NAME=${CLUSTER_NAME:-e2e-kai-scheduler}

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
GOPATH=${HOME}/go
GOBIN=${GOPATH}/bin

LOCAL_IMAGES_BUILD="false"
PRESERVE_CLUSTER="false"

while [[ $# -gt 0 ]]; do
  case $1 in
    --local-images-build)
      LOCAL_IMAGES_BUILD="true"
      shift
      ;;
    --preserve-cluster)
      PRESERVE_CLUSTER="true"
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--local-images-build] [--preserve-cluster]"
      echo "  --local-images-build: Build and use local images instead of pulling from registry"
      echo "  --preserve-cluster: Keep the kind cluster after running the test suite"
      echo ""
      echo "Environment variables:"
      echo "  PACKAGE_VERSION: Override the chart version to install"
      echo "  ARGOCD_VERSION: ArgoCD version to install (see setup-gitops-e2e.sh)"
      exit 0
      ;;
    *)
      echo "Unknown option $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

# Resolve the chart version, mirroring setup-e2e-cluster.sh
if [ -z "$PACKAGE_VERSION" ]; then
    if [ "$LOCAL_IMAGES_BUILD" = "true" ]; then
        GIT_REV=$(git rev-parse --short HEAD | sed 's/^0*//')
        PACKAGE_VERSION=0.0.0-$GIT_REV
    else
        PACKAGE_VERSION=$(curl -s https://api.github.com/repos/kai-scheduler/KAI-scheduler/releases/latest | jq -r .tag_name)
        if [ -z "$PACKAGE_VERSION" ] || [ "$PACKAGE_VERSION" = "null" ]; then
            echo "Failed to resolve latest release."
            exit 1
        fi
    fi
fi
export PACKAGE_VERSION

# Set up the cluster (images + packaged chart, no KAI install)
SETUP_ARGS="--skip-kai-install"
if [ "$LOCAL_IMAGES_BUILD" = "true" ]; then
    SETUP_ARGS="$SETUP_ARGS --local-images-build"
fi
${REPO_ROOT}/hack/setup-e2e-cluster.sh $SETUP_ARGS

# Locate or fetch the chart tgz to serve in-cluster
CHART_TGZ=${REPO_ROOT}/charts/kai-scheduler-$PACKAGE_VERSION.tgz
if [ ! -f "$CHART_TGZ" ]; then
    echo "Pulling chart version $PACKAGE_VERSION..."
    mkdir -p ${REPO_ROOT}/charts
    helm pull oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler --version "$PACKAGE_VERSION" -d ${REPO_ROOT}/charts
fi

${REPO_ROOT}/hack/setup-gitops-e2e.sh --chart-tgz "$CHART_TGZ"

# Install ginkgo if it's not installed
if [ ! -f ${GOBIN}/ginkgo ]; then
    echo "Installing ginkgo"
    GOBIN=${GOBIN} go install github.com/onsi/ginkgo/v2/ginkgo@v2.25.3
fi

echo "Running gitops tests..."
export GITOPS_CHART_VERSION=$PACKAGE_VERSION
# Locally-built images live in the in-cluster registry; released images are
# pulled from the chart's default (release) registry.
if [ "$LOCAL_IMAGES_BUILD" = "true" ]; then
    export GITOPS_KAI_REGISTRY=localhost:30100
fi
${GOBIN}/ginkgo -r --keep-going --trace -vv --label-filter 'gitops' ${REPO_ROOT}/test/e2e/suites/gitops

# Cleanup
rm -rf "$CHART_TGZ"

if [ "$PRESERVE_CLUSTER" != "true" ]; then
    kind delete cluster --name $CLUSTER_NAME
fi

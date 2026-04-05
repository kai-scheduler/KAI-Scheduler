#!/bin/bash
# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0


CLUSTER_NAME=${CLUSTER_NAME:-e2e-kai-scheduler}

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
GOPATH=${HOME}/go
GOBIN=${GOPATH}/bin

# Parse named parameters
TEST_THIRD_PARTY_INTEGRATIONS="false"
LOCAL_IMAGES_BUILD="false"
PRESERVE_CLUSTER="false"

while [[ $# -gt 0 ]]; do
  case $1 in
    --test-third-party-integrations)
      TEST_THIRD_PARTY_INTEGRATIONS="true"
      shift
      ;;
    --local-images-build)
      LOCAL_IMAGES_BUILD="true"
      shift
      ;;
    --preserve-cluster)
      PRESERVE_CLUSTER="true"
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--test-third-party-integrations] [--local-images-build] [--preserve-cluster]"
      echo "  --test-third-party-integrations: Install third party operators for compatibility testing"
      echo "  --local-images-build: Build and use local images instead of pulling from registry"
      echo "  --preserve-cluster: Keep the kind cluster after running the test suite"
      exit 0
      ;;
    *)
      echo "Unknown option $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

# Build setup script arguments
SETUP_ARGS=""
if [ "$TEST_THIRD_PARTY_INTEGRATIONS" = "true" ]; then
    SETUP_ARGS="$SETUP_ARGS --test-third-party-integrations"
fi
if [ "$LOCAL_IMAGES_BUILD" = "true" ]; then
    SETUP_ARGS="$SETUP_ARGS --local-images-build"
fi

# Run the cluster setup script
${REPO_ROOT}/hack/setup-e2e-cluster.sh $SETUP_ARGS

# Install ginkgo if it's not installed
if [ ! -f ${GOBIN}/ginkgo ]; then
    echo "Installing ginkgo"
    GOBIN=${GOBIN} go install github.com/onsi/ginkgo/v2/ginkgo@v2.25.3
fi

${GOBIN}/ginkgo -r --keep-going --randomize-all --randomize-suites --label-filter '!autoscale && !scale && !upgrade' --trace -vv ${REPO_ROOT}/test/e2e/suites

if [ "$PRESERVE_CLUSTER" != "true" ]; then
    kind delete cluster --name $CLUSTER_NAME
fi

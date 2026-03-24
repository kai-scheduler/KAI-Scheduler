#!/bin/bash
# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0


CLUSTER_NAME=${CLUSTER_NAME:-e2e-kai-scheduler}

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
KIND_CONFIG=${REPO_ROOT}/hack/e2e-kind-config.yaml
GOPATH=${HOME}/go
GOBIN=${GOPATH}/bin

: ${KIND_K8S_TAG:="v1.34.0"}
: ${KIND_IMAGE:="kindest/node:${KIND_K8S_TAG}"}

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
      echo "Usage: $0 [--test-third-party-integrations] [--local-images-build]"
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

kind create cluster \
    --config ${KIND_CONFIG} \
    --image ${KIND_IMAGE}\
    --name $CLUSTER_NAME

# Install the fake-gpu-operator to provide a fake GPU resources for the e2e tests
helm upgrade -i gpu-operator oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator --namespace gpu-operator --create-namespace --version 0.0.62 \
    --values ${REPO_ROOT}/hack/fake-gpu-operator-values.yaml --wait

# install third party operators to check the compatibility with the kai-scheduler
if [ "$TEST_THIRD_PARTY_INTEGRATIONS" = "true" ]; then
    ${REPO_ROOT}/hack/third_party_integrations/deploy_ray.sh
    ${REPO_ROOT}/hack/third_party_integrations/deploy_kubeflow.sh
    ${REPO_ROOT}/hack/third_party_integrations/deploy_knative.sh
    ${REPO_ROOT}/hack/third_party_integrations/deploy_lws.sh
fi

if [ "$LOCAL_IMAGES_BUILD" = "true" ]; then
    cd ${REPO_ROOT}
    PACKAGE_VERSION=0.0.0-$(git rev-parse --short HEAD)
    make build VERSION=$PACKAGE_VERSION
    for image in $(docker images --format '{{.Repository}}:{{.Tag}}' | grep $PACKAGE_VERSION); do
        kind load docker-image $image --name $CLUSTER_NAME
    done
    helm package ./deployments/kai-scheduler -d ./charts --app-version $PACKAGE_VERSION --version $PACKAGE_VERSION
    helm upgrade -i kai-scheduler ./charts/kai-scheduler-$PACKAGE_VERSION.tgz  -n kai-scheduler --create-namespace --set "global.gpuSharing=true" --wait
    rm -rf ./charts/kai-scheduler-$PACKAGE_VERSION.tgz 
    cd ${REPO_ROOT}/hack
else
    PACKAGE_VERSION=0.0.0-$(git rev-parse --short origin/main)
    helm upgrade -i kai-scheduler oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler -n kai-scheduler --create-namespace --set "global.gpuSharing=true" --version "$PACKAGE_VERSION"
fi

# Allow all the pods in the fake-gpu-operator and kai-scheduler to start
sleep 30

# Install ginkgo if it's not installed
if [ ! -f ${GOBIN}/ginkgo ]; then
    echo "Installing ginkgo"
    GOBIN=${GOBIN} go install github.com/onsi/ginkgo/v2/ginkgo@v2.23.3
fi

${GOBIN}/ginkgo -r --keep-going --randomize-all --randomize-suites --label-filter '!autoscale && !scale' --trace -vv ${REPO_ROOT}/test/e2e/suites

if [ "$PRESERVE_CLUSTER" != "true" ]; then
    kind delete cluster --name $CLUSTER_NAME
fi

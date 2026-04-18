#!/bin/bash
# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# This script sets up a kind cluster for e2e testing with the kai-scheduler.
# It can be run independently or sourced from run-e2e-kind.sh.

set -e

CLUSTER_NAME=${CLUSTER_NAME:-e2e-kai-scheduler}

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..
: ${FEATURE_CONFIG:="default"}

: ${KIND_K8S_TAG:="v1.34.0"}
: ${KIND_IMAGE:="kindest/node:${KIND_K8S_TAG}"}

# Parse named parameters
TEST_THIRD_PARTY_INTEGRATIONS=${TEST_THIRD_PARTY_INTEGRATIONS:-"false"}
LOCAL_IMAGES_BUILD=${LOCAL_IMAGES_BUILD:-"false"}
INSTALL_VPA=${INSTALL_VPA:-"false"}

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
    --install-vpa)
      INSTALL_VPA="true"
      shift
      ;;
    --feature-config)
      FEATURE_CONFIG="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [--test-third-party-integrations] [--local-images-build] [--install-vpa] [--feature-config <config>]"
      echo "  --test-third-party-integrations: Install third party operators for compatibility testing"
      echo "  --local-images-build: Build and use local images instead of pulling from registry"
      echo "  --install-vpa: Install Vertical Pod Autoscaler and metrics-server"
      echo "  --feature-config: Feature configuration for kind cluster generation (default: \"default\")"
      exit 0
      ;;
    *)
      echo "Unknown option $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

GENERATED_KIND_CONFIG=$(mktemp "${TMPDIR:-/tmp}/kind-config-XXXXXX.yaml")
trap "rm -f \"$GENERATED_KIND_CONFIG\"" EXIT
${REPO_ROOT}/hack/generate-kind-config.sh \
    --feature-config "$FEATURE_CONFIG" \
    --k8s-version "$KIND_K8S_TAG" \
    --output "$GENERATED_KIND_CONFIG"

kind create cluster \
    --config "$GENERATED_KIND_CONFIG" \
    --image "${KIND_IMAGE}" \
    --name "$CLUSTER_NAME"

rm -f "$GENERATED_KIND_CONFIG"

# Deploy local image registry
echo "Deploying local image registry..."
kubectl apply -f ${REPO_ROOT}/hack/local_registry.yaml
kubectl wait --for=condition=available --timeout=60s deployment/registry -n kube-registry

# Install the fake-gpu-operator to provide fake GPU resources for the e2e tests
DRA_PLUGIN_ENABLED="false"
if [ "$FEATURE_CONFIG" = "dra-enabled" ]; then
  DRA_PLUGIN_ENABLED="true"
fi
helm upgrade -i gpu-operator oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator --namespace gpu-operator --create-namespace \
    --version 0.0.74 --values ${REPO_ROOT}/hack/fake-gpu-operator-values.yaml --set "draPlugin.enabled=$DRA_PLUGIN_ENABLED" --wait

# Deploy Prometheus Operator
echo "Deploying Prometheus Operator..."
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts --force-update
helm repo update prometheus-community
helm install prometheus prometheus-community/kube-prometheus-stack --namespace monitoring --create-namespace \
    --set "alertmanager.enabled=false" \
    --set "grafana.enabled=false" \
    --set "prometheus.enabled=false" \
    --wait

# Install VPA and its prerequisites
if [ "$INSTALL_VPA" = "true" ]; then
    echo "Installing metrics-server (required by VPA recommender)..."
    kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/download/v0.8.1/components.yaml
    # kind uses self-signed kubelet certs, so metrics-server needs --kubelet-insecure-tls
    kubectl patch deployment metrics-server -n kube-system --type=json \
        -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'
    kubectl wait --for=condition=available --timeout=120s deployment/metrics-server -n kube-system

    echo "Installing Vertical Pod Autoscaler..."
    VPA_TMPDIR=$(mktemp -d)
    git clone https://github.com/kubernetes/autoscaler.git "$VPA_TMPDIR/autoscaler"
    (cd "$VPA_TMPDIR/autoscaler/vertical-pod-autoscaler" && git checkout vertical-pod-autoscaler-1.5.1 && ./hack/vpa-up.sh)
    rm -rf "$VPA_TMPDIR"
    echo "VPA installation complete."
fi

# Install third party operators to check the compatibility with the kai-scheduler
if [ "$TEST_THIRD_PARTY_INTEGRATIONS" = "true" ]; then
    ${REPO_ROOT}/hack/third_party_integrations/deploy_ray.sh
    ${REPO_ROOT}/hack/third_party_integrations/deploy_kubeflow.sh
    ${REPO_ROOT}/hack/third_party_integrations/deploy_knative.sh
    ${REPO_ROOT}/hack/third_party_integrations/deploy_lws.sh
    ${REPO_ROOT}/hack/third_party_integrations/deploy_jobset.sh
fi

# Build and install kai-scheduler
if [ -z "$PACKAGE_VERSION" ]; then
    if [ "$LOCAL_IMAGES_BUILD" = "true" ]; then
        GIT_REV=$(git rev-parse --short HEAD | sed 's/^0*//')
        PACKAGE_VERSION=0.0.0-$GIT_REV
    else
        PACKAGE_VERSION=$(curl -s https://api.github.com/repos/kai-scheduler/KAI-scheduler/releases/latest | jq -r .tag_name)
        if [ -z "$PACKAGE_VERSION" ] || [ "$PACKAGE_VERSION" = "null" ]; then
            echo "Failed to resolve latest release. Falling back to commit-based version."
            GIT_REV=$(git rev-parse --short HEAD | sed 's/^0*//')
            PACKAGE_VERSION=0.0.0-$GIT_REV
        fi
    fi
fi

if [ "$LOCAL_IMAGES_BUILD" = "true" ]; then
    cd ${REPO_ROOT}
    echo "Building docker images with version $PACKAGE_VERSION..."
    make build DOCKER_REPO_BASE=localhost:30100 VERSION=$PACKAGE_VERSION

    # Start port-forward to local registry
    kubectl port-forward -n kube-registry deploy/registry 30100:5000 &
    PORT_FORWARD_PID=$!
    trap "kill $PORT_FORWARD_PID 2>/dev/null || true" EXIT
    sleep 2

    # Probe whether docker push can reach the registry (fails on Docker Desktop where the
    # daemon runs in a VM and cannot reach the host-side port-forward on localhost).
    PROBE_IMAGE=$(docker images --format '{{.Repository}}:{{.Tag}}' | grep "$PACKAGE_VERSION" | head -1)
    if docker push "$PROBE_IMAGE" > /dev/null 2>&1; then
        echo "Pushing images to local registry via port-forward..."
        for image in $(docker images --format '{{.Repository}}:{{.Tag}}' | grep $PACKAGE_VERSION); do
            docker push "$image"
        done
    else
        echo "docker push failed (likely Docker Desktop — daemon cannot reach host port-forward). Falling back to 'kind load docker-image'..."
        kill "$PORT_FORWARD_PID" 2>/dev/null || true
        for image in $(docker images --format '{{.Repository}}:{{.Tag}}' | grep "localhost:30100/.*${PACKAGE_VERSION}"); do
            echo "  loading: $image"
            kind load docker-image "$image" --name "$CLUSTER_NAME"
        done
    fi

    # Package and install helm chart
    helm package ./deployments/kai-scheduler -d ./charts --app-version $PACKAGE_VERSION --version $PACKAGE_VERSION
    helm upgrade -i kai-scheduler ./charts/kai-scheduler-$PACKAGE_VERSION.tgz -n kai-scheduler --create-namespace \
        --set "global.gpuSharing=true" --set "global.registry=localhost:30100" --debug --wait
    rm -rf ./charts/kai-scheduler-$PACKAGE_VERSION.tgz
    cd ${REPO_ROOT}/hack
else
    helm upgrade -i kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler -n kai-scheduler --create-namespace \
        --set "global.gpuSharing=true" --wait --version "$PACKAGE_VERSION"
fi

# Create RBAC for fake-gpu-operator status updates
kubectl create clusterrole pods-patcher --verb=patch --resource=pods
kubectl create rolebinding fake-status-updater --clusterrole=pods-patcher --serviceaccount=gpu-operator:status-updater -n kai-resource-reservation

echo "Cluster setup complete. Cluster name: $CLUSTER_NAME"

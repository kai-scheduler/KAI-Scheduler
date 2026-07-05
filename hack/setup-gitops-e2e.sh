#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Prepares a cluster for the GitOps e2e suite (test/e2e/suites/gitops):
#   1. Installs ArgoCD Core (no server/UI/dex) into the argocd namespace.
#   2. Deploys a static in-cluster Helm repo (nginx) serving the given chart
#      tgz, so the argocd-repo-server can fetch it at
#      http://chart-repo.chart-repo.svc.cluster.local
#
# Usage: setup-gitops-e2e.sh --chart-tgz <path-to-kai-scheduler-chart.tgz>

set -e

REPO_ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/..

# The chart's post-delete cleanup job relies on the PostDelete hook,
# supported since ArgoCD 2.10.
ARGOCD_VERSION=${ARGOCD_VERSION:-v3.4.4}

CHART_TGZ=""
while [[ $# -gt 0 ]]; do
  case $1 in
    --chart-tgz)
      CHART_TGZ="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 --chart-tgz <path>"
      echo "  --chart-tgz: Path to the packaged kai-scheduler chart tgz to serve in-cluster"
      echo ""
      echo "Environment variables:"
      echo "  ARGOCD_VERSION: ArgoCD version to install (default: $ARGOCD_VERSION, must be >= 2.10)"
      exit 0
      ;;
    *)
      echo "Unknown option $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

if [ -z "$CHART_TGZ" ] || [ ! -f "$CHART_TGZ" ]; then
  echo "Error: --chart-tgz must point to an existing chart tgz (got: '$CHART_TGZ')"
  exit 1
fi

echo "Installing ArgoCD Core $ARGOCD_VERSION..."
kubectl create namespace argocd --dry-run=client -o yaml | kubectl apply -f -
# Server-side: the applicationsets CRD exceeds the client-side
# last-applied-configuration annotation size limit.
kubectl apply --server-side --force-conflicts -n argocd -f \
  "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/core-install.yaml"
# Not needed by the suite; save resources on the kind node.
kubectl -n argocd scale deployment argocd-applicationset-controller --replicas=0 2>/dev/null || true
kubectl -n argocd scale deployment argocd-notifications-controller --replicas=0 2>/dev/null || true
kubectl -n argocd rollout status deployment/argocd-repo-server --timeout=180s
kubectl -n argocd rollout status statefulset/argocd-application-controller --timeout=180s

# ArgoCD Core has no argocd-server, which is the component that normally
# auto-creates the default AppProject.
kubectl apply -f - <<'EOF'
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: default
  namespace: argocd
spec:
  clusterResourceWhitelist:
    - group: '*'
      kind: '*'
  destinations:
    - namespace: '*'
      server: '*'
  sourceRepos:
    - '*'
EOF

# RBAC for fake-gpu-operator status updates, created right after the KAI
# install in the other e2e paths (see setup-e2e-cluster.sh). KAI is installed
# later here by the ArgoCD Application, so the reservation namespace is
# pre-created and adopted by ArgoCD on sync (ServerSideApply).
kubectl create namespace kai-resource-reservation --dry-run=client -o yaml | kubectl apply -f -
kubectl create clusterrole pods-patcher --verb=patch --resource=pods --dry-run=client -o yaml | kubectl apply -f -
kubectl create rolebinding fake-status-updater --clusterrole=pods-patcher \
  --serviceaccount=gpu-operator:status-updater -n kai-resource-reservation \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Deploying in-cluster chart repo serving $(basename "$CHART_TGZ")..."
STAGING=$(mktemp -d)
trap 'rm -rf "$STAGING"' EXIT
cp "$CHART_TGZ" "$STAGING/"
# No --url: relative chart URLs, resolved against the repo URL by clients.
helm repo index "$STAGING"

kubectl apply -f "${REPO_ROOT}/hack/gitops/chart-repo.yaml"
kubectl -n chart-repo create configmap kai-charts --from-file="$STAGING" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n chart-repo rollout status deployment/chart-repo --timeout=120s

echo "GitOps e2e setup complete: ArgoCD Core $ARGOCD_VERSION + chart repo at http://chart-repo.chart-repo.svc.cluster.local"

# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

ARG KUBECTL_VERSION=v1.35.0

FROM curlimages/curl:8.11.1 AS kubectl-fetch
ARG TARGETARCH
ARG KUBECTL_VERSION
RUN curl -fsSL -o /tmp/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" \
 && chmod 0755 /tmp/kubectl

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=kubectl-fetch /tmp/kubectl /usr/bin/kubectl
COPY deployments/kai-scheduler/crds /internal-crds
COPY NOTICE /NOTICE
USER 65532:65532
ENTRYPOINT ["/usr/bin/kubectl", "apply", "--server-side=true", "--force-conflicts", "-f", "/internal-crds"]

# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

ARG KUBECTL_VERSION=v1.35.0

FROM alpine:3.20 AS kubectl-fetch
ARG TARGETARCH
ARG KUBECTL_VERSION
RUN apk add --no-cache curl \
 && curl -fsSL -o /tmp/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" \
 && chmod 0755 /tmp/kubectl

FROM alpine:3.20
RUN apk add --no-cache bash ca-certificates
COPY --from=kubectl-fetch /tmp/kubectl /usr/bin/kubectl
COPY deployments/kai-scheduler/crds /internal-crds
COPY NOTICE /NOTICE
COPY deployments/crd-upgrader/apply-crds.sh /apply-crds.sh
RUN chmod 0755 /apply-crds.sh

USER 65532:65532
CMD ["/apply-crds.sh"]

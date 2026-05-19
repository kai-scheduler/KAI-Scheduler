# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

FROM alpine:3.20
ARG TARGETARCH
ARG KUBECTL_VERSION=v1.35.0

RUN apk add --no-cache bash ca-certificates
ADD --chmod=0755 "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" /usr/bin/kubectl

COPY deployments/kai-scheduler/crds /internal-crds
COPY NOTICE /NOTICE
COPY deployments/crd-upgrader/apply-crds.sh /apply-crds.sh
RUN chmod 0755 /apply-crds.sh

USER 65532:65532
CMD ["/apply-crds.sh"]

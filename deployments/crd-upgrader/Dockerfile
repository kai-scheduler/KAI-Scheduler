# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

FROM registry.access.redhat.com/ubi9/ubi-minimal
ARG TARGETARCH
ENV TARGETARCH=$TARGETARCH
RUN curl -o kubectl -L "https://dl.k8s.io/release/v1.35.0/bin/linux/$TARGETARCH/kubectl" \
&& chmod +x kubectl && mv kubectl /usr/bin/kubectl


COPY deployments/kai-scheduler/crds /internal-crds
COPY NOTICE .
COPY deployments/crd-upgrader/apply-crds.sh /apply-crds.sh
RUN chmod +x /apply-crds.sh

USER 65532:65532

CMD ["/apply-crds.sh"]

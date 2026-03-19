# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.25.6 AS debug
ARG TARGETARCH
ARG SERVICE_NAME
ENV TARGETARCH=$TARGETARCH
ENV SERVICE_NAME=$SERVICE_NAME

RUN go install github.com/go-delve/delve/cmd/dlv@latest

WORKDIR /workspace
ADD bin/$SERVICE_NAME-$TARGETARCH app

RUN chgrp -R 0 /workspace && chmod -R g=u /workspace
USER 65532:65532

ENTRYPOINT ["/go/bin/dlv", "exec", "--headless", "-l", ":10000", "--api-version=2", "/workspace/app", "--"]

FROM registry.access.redhat.com/ubi9/ubi-minimal AS prod
ARG TARGETARCH
ARG SERVICE_NAME
ENV TARGETARCH=$TARGETARCH
ENV SERVICE_NAME=$SERVICE_NAME

WORKDIR /workspace
ADD bin/$SERVICE_NAME-$TARGETARCH app
ADD NOTICE .

RUN chgrp -R 0 /workspace && chmod -R g=u /workspace

USER 65532:65532

ENTRYPOINT ["/workspace/app"]

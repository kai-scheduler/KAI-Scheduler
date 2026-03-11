# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.22 AS debug
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

FROM nvcr.io/nvidia/distroless/go:v3.2.1 AS prod
ARG TARGETARCH
ARG SERVICE_NAME
ENV TARGETARCH=$TARGETARCH
ENV SERVICE_NAME=$SERVICE_NAME

WORKDIR /workspace
COPY --chmod=0755 --chown=65532:0 bin/$SERVICE_NAME-$TARGETARCH app
COPY --chmod=0644 --chown=65532:0 NOTICE .

USER 65532:65532

ENTRYPOINT ["/workspace/app"]

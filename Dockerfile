# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.26.3 AS debug
ARG TARGETARCH
ARG SERVICE_NAME
ENV TARGETARCH=$TARGETARCH
ENV SERVICE_NAME=$SERVICE_NAME

RUN go install github.com/go-delve/delve/cmd/dlv@v1.27.2

WORKDIR /workspace
ADD bin/$SERVICE_NAME-$TARGETARCH app

RUN chgrp -R 0 /workspace && chmod -R g=u /workspace
USER 65532:65532

ENTRYPOINT ["/go/bin/dlv", "exec", "--headless", "-l", ":10000", "--api-version=2", "/workspace/app", "--"]

# Source for the CA bundle copied into the scratch image; same image as the debug stage, so no extra pull.
FROM golang:1.26.3 AS certs

# Statically linked (CGO_ENABLED=0) services.
FROM scratch AS prod
ARG TARGETARCH
ARG SERVICE_NAME
ENV TARGETARCH=$TARGETARCH
ENV SERVICE_NAME=$SERVICE_NAME

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

WORKDIR /workspace
COPY --chmod=0755 --chown=65532:0 bin/$SERVICE_NAME-$TARGETARCH app
COPY --chmod=0644 --chown=65532:0 NOTICE .

USER 65532:65532

ENTRYPOINT ["/workspace/app"]

# Services that need cgo at runtime (CGO_SERVICES in build/makefile/base.mk) require a libc, so they stay on distroless.
FROM nvcr.io/nvidia/distroless/go:v3.2.1 AS prod-cgo
ARG TARGETARCH
ARG SERVICE_NAME
ENV TARGETARCH=$TARGETARCH
ENV SERVICE_NAME=$SERVICE_NAME

WORKDIR /workspace
COPY --chmod=0755 --chown=65532:0 bin/$SERVICE_NAME-$TARGETARCH app
COPY --chmod=0644 --chown=65532:0 NOTICE .

USER 65532:65532

ENTRYPOINT ["/workspace/app"]

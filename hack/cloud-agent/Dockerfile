# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

FROM ubuntu:24.04

ARG GO_VERSION=1.26.8
ARG HELM_VERSION=3.17.2

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        fuse-overlayfs \
        iptables \
        docker.io \
        docker-buildx \
        build-essential \
        xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
        | tar -C /usr/local -xz \
    && curl -fsSL "https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz" \
        | tar -xz -C /tmp \
    && mv /tmp/linux-amd64/helm /usr/local/bin/helm \
    && rm -rf /tmp/linux-amd64 \
    && usermod -aG docker ubuntu

ENV PATH=/usr/local/go/bin:/usr/local/bin:${PATH}

USER ubuntu

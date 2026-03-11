# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.24.4-bullseye AS builder

RUN apt-get update && apt-get install -y \
    g++-x86-64-linux-gnu \
    g++-aarch64-linux-gnu \
    libc6-dev-amd64-cross \
    libc6-dev-arm64-cross
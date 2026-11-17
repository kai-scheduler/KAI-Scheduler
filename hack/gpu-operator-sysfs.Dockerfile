# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

ARG GPU_OPERATOR_VERSION=v26.7.2
FROM nvcr.io/nvidia/gpu-operator:${GPU_OPERATOR_VERSION}

ARG GPU_OPERATOR_REF
LABEL org.opencontainers.image.revision=${GPU_OPERATOR_REF}

# GPU Operator #2959 changes operand manifests, with no binary changes.
COPY assets /opt/gpu-operator/

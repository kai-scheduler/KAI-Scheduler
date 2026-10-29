# Builder image for make build-go / make test without pulling from Docker Hub.
# Matches build/builder/Dockerfile toolchains using Ubuntu + Go tarball.

FROM ubuntu:24.04

ARG GO_VERSION=1.26.8

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates curl \
        g++-x86-64-linux-gnu \
        g++-aarch64-linux-gnu \
        libc6-dev-amd64-cross \
        libc6-dev-arm64-cross \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
        | tar -C /usr/local -xz

ENV PATH="/usr/local/go/bin:${PATH}"
WORKDIR /local

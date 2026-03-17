# Uses base.mk file from build/makefile

## Paths
BUILD_DIR=bin
BUILD_DIR_ABS=$(shell pwd)/${BUILD_DIR}
GOLANG_LINTER_CONFIG_PATH=build/lint/.golangci.yaml
BUILD_OUT_PATH_AMD=${BUILD_DIR}/${SERVICE_NAME}-amd64
BUILD_OUT_PATH_ARM=${BUILD_DIR}/${SERVICE_NAME}-arm64
BUILD_IN_PATH=cmd/${SERVICE_NAME}/main.go

GOCACHE_DOCKER_DIR=/tmp/.cache
GOCACHE_HOST_DIR=${HOME}/.cache/go-build-docker-gocache
GOPATH_HOST_DIR=${HOME}/.cache/go-build-docker-gopath

DOCKER_GO_CACHING_VOLUME_AND_ENV := -v ${GOPATH_HOST_DIR}:/go:z -v ${GOCACHE_HOST_DIR}:${GOCACHE_DOCKER_DIR}:z -e GOPATH=/go -e GOCACHE=${GOCACHE_DOCKER_DIR} -e GOLANGCI_LINT_CACHE=${GOCACHE_DOCKER_DIR}
ifneq ($(GOPROXY),)
DOCKER_GO_CACHING_VOLUME_AND_ENV += -e GOPROXY=$(GOPROXY)
endif
ifneq ($(GOSUMDB),)
DOCKER_GO_CACHING_VOLUME_AND_ENV += -e GOSUMDB=$(GOSUMDB)
endif

## Version
GO_VERSION=1.26.1
GO_IMAGE_VERSION=${GO_VERSION}-bookworm
GOLANGCI_LINT_VERSION=v2.11.3

## Tool Versions
CGO_ENABLED?=1

## Version Variables
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
GIT_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "unknown")
GIT_TREE_STATE := $(shell if git diff-index --quiet HEAD -- 2>/dev/null; then echo "clean"; else echo "dirty"; fi)

## Go Build Flags
VERSION_PKG := github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/version
LDFLAGS := -X '$(VERSION_PKG).buildDate=$(BUILD_DATE)' \
           -X '$(VERSION_PKG).gitCommit=$(GIT_COMMIT)' \
           -X '$(VERSION_PKG).gitVersion=$(GIT_VERSION)' \
           -X '$(VERSION_PKG).gitTreeState=$(GIT_TREE_STATE)'

### GO
DOCKER_GO_BASE_COMMAND=${DOCKER_COMMAND} -e CGO_ENABLED=${CGO_ENABLED} -e GO111MODULE=on ${DOCKER_GO_CACHING_VOLUME_AND_ENV}

GO_ENV_ARCH_AMD=-e GOOS=linux -e GOARCH=amd64 -e CC=x86_64-linux-gnu-gcc -e CXX=x86_64-linux-gnu-g++
GO_ENV_ARCH_ARM=-e GOOS=linux -e GOARCH=arm64 -e CC=aarch64-linux-gnu-gcc -e CXX=aarch64-linux-gnu-g++
DOCKER_GO_COMMAND=${DOCKER_GO_BASE_COMMAND} builder:${GO_IMAGE_VERSION}
DOCKER_GO_COMMAND_AMD=${DOCKER_GO_BASE_COMMAND} ${GO_ENV_ARCH_AMD} builder:${GO_IMAGE_VERSION}
DOCKER_GO_COMMAND_ARM=${DOCKER_GO_BASE_COMMAND} ${GO_ENV_ARCH_ARM} builder:${GO_IMAGE_VERSION}

DOCKER_GO_LINTER_COMMAND=${DOCKER_GO_BASE_COMMAND} -e GOFLAGS="-buildvcs=false" golangci/golangci-lint:${GOLANGCI_LINT_VERSION}

ifeq ($(DEBUG), 1)
GO_BUILD_ADDITIONAL_FLAGS=-gcflags="all=-N -l"
else
GO_BUILD_ADDITIONAL_FLAGS=
endif

gocache:
	mkdir -p ${GOCACHE_HOST_DIR}
	mkdir -p ${GOPATH_HOST_DIR}
.PHONY: gocache

lint-go: gocache
	@ ${ECHO_COMMAND} ${GREEN_CONSOLE} "${CONSOLE_PREFIX} Running golangci linter" ${BASE_CONSOLE}
	${DOCKER_GO_LINTER_COMMAND} golangci-lint run -v -c ${GOLANG_LINTER_CONFIG_PATH} || ${FAILURE_MESSAGE_HANDLER}
	${SUCCESS_MESSAGE_HANDLER}
.PHONY: lint-go

fmt-go:
	go fmt ./...
.PHONY: fmt-go

vet-go:
	go vet ./...
.PHONY: vet-go

build-go: builder build-go-amd build-go-arm

build-go-amd: gocache
	@ ${ECHO_COMMAND} ${GREEN_CONSOLE} "${CONSOLE_PREFIX} Building ${SERVICE_NAME}, GOOS: ${OS}, GOARCH amd64" ${BASE_CONSOLE}
	${DOCKER_GO_COMMAND_AMD} go build -buildvcs=false -ldflags "$(LDFLAGS)" ${GO_BUILD_ADDITIONAL_FLAGS} -o ${BUILD_OUT_PATH_AMD} ${BUILD_IN_PATH} || ${FAILURE_MESSAGE_HANDLER}
	${SUCCESS_MESSAGE_HANDLER}
.PHONY: build-go-amd

build-go-arm: gocache
	@ ${ECHO_COMMAND} ${GREEN_CONSOLE} "${CONSOLE_PREFIX} Building ${SERVICE_NAME}, GOOS: ${OS}, GOARCH arm64" ${BASE_CONSOLE}
	${DOCKER_GO_COMMAND_ARM} go build -buildvcs=false -ldflags "$(LDFLAGS)" ${GO_BUILD_ADDITIONAL_FLAGS} -o ${BUILD_OUT_PATH_ARM} ${BUILD_IN_PATH} || ${FAILURE_MESSAGE_HANDLER}
	${SUCCESS_MESSAGE_HANDLER}
.PHONY: build-go-arm

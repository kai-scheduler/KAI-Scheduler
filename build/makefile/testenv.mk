ENVTEST_K8S_VERSION = 1.34.0
ENVTEST_VERSION=release-0.20

E2E_TESTS_DIR = "test/e2e/"
TEST_TARGETS = $(shell go list ./... | grep -v "${E2E_TESTS_DIR}")

envtest-docker-go: builder gocache
	@ ${ECHO_COMMAND} ${GREEN_CONSOLE} "${CONSOLE_PREFIX} Running unit-tests" ${BASE_CONSOLE}
	${DOCKER_GO_COMMAND} make envtest-go || ${FAILURE_MESSAGE_HANDLER}
	${SUCCESS_MESSAGE_HANDLER}

envtest-go: envtest
	@ ${ECHO_COMMAND} ${GREEN_CONSOLE} "${CONSOLE_PREFIX} Running unit-tests" ${BASE_CONSOLE}
	mkdir -p coverage
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path --bin-dir $(LOCALBIN))" \
    go test ${TEST_TARGETS} -timeout 30m -coverprofile=coverage/coverage.out || ${FAILURE_MESSAGE_HANDLER}
	${SUCCESS_MESSAGE_HANDLER}

ENVTEST = $(LOCALBIN)/setup-envtest
.PHONY: envtest
envtest: ## Download envtest locally if necessary.
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@${ENVTEST_VERSION}

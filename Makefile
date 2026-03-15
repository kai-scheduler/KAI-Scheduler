include build/makefile/index.mk

CONTROLLER_TOOLS_VERSION ?= v0.16.1
MOCKGEN_VERSION ?= v0.5.0
ADDLICENSE_VERSION ?= v1.1.1
KUSTOMIZE_VERSION ?= v5.0.0

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
MOCKGEN ?= $(LOCALBIN)/mockgen
ADDLICENSE ?= $(LOCALBIN)/addlicense
KUSTOMIZE ?= $(LOCALBIN)/kustomize

# Space seperated list of services to build by default
# SERVICE_NAMES := service1 service2 service3
SERVICE_NAMES := podgrouper scheduler binder resourcereservation snapshot-tool scalingpod nodescaleadjuster podgroupcontroller queuecontroller fairshare-simulator admission operator time-based-fairshare-simulator

# Kubernetes manifest files that require Kubernetes copyright header (space-separated)
K8S_COPYRIGHTED_MANIFEST_FILES := deployments/kai-scheduler/crds/kai.scheduler_topologies.yaml


lint: fmt-go vet-go lint-go
.PHONY: lint

.PHONY: test-chart
test-chart:
	@echo "Running tests for Helm chart: kai-scheduler"
	docker run -t --rm -v ./deployments/kai-scheduler:/apps helmunittest/helm-unittest:3.17.2-0.8.1 . -f 'tests/**/*_test.yaml'

.PHONY: test
test: test-chart envtest-docker-go

.PHONY: build
build: $(SERVICE_NAMES)
	$(MAKE) docker-build-crd-upgrader

$(SERVICE_NAMES):
	$(MAKE) build-go SERVICE_NAME=$@
	$(MAKE) docker-build-generic SERVICE_NAME=$@

.PHONY: push
push: $(SERVICE_NAMES)
	docker push $(DOCKER_REPO_BASE)/crd-upgrader:$(VERSION)

.PHONY: validate
validate: generate manifests clients gen-license generate-mocks lint
	git diff --exit-code

.PHONY: generate-mocks
generate-mocks: mockgen
	$(MOCKGEN) -source=pkg/binder/binding/interface.go -destination=pkg/binder/binding/mock/binder_mock.go -package=mock_binder
	$(MOCKGEN) -source=pkg/binder/binding/resourcereservation/resource_reservation.go -destination=pkg/binder/binding/resourcereservation/mock/resource_reservation_mock.go -package=mock_resourcereservation
	$(MOCKGEN) -source=pkg/binder/plugins/interface.go -destination=pkg/binder/plugins/mock/plugins_mock.go -package=mock_plugins
	$(MOCKGEN) -source=pkg/binder/plugins/k8s-plugins/common/interface.go -destination=pkg/binder/plugins/k8s-plugins/common/mock/mock_common_plugins.go -package=mock_common_plugins
	$(MOCKGEN) -source=pkg/scheduler/cache/interface.go -destination=pkg/scheduler/cache/cache_mock.go -package=cache
	$(MOCKGEN) -source=pkg/scheduler/api/pod_affinity/cluster_pod_affinity_info.go -destination=pkg/scheduler/api/pod_affinity/mock_cluster_pod_affinity_info.go -package=pod_affinity
	$(MOCKGEN) -source=pkg/scheduler/cache/cluster_info/data_lister/interface.go -destination=pkg/scheduler/cache/cluster_info/data_lister/data_lister_mock.go -package=data_lister
	$(MOCKGEN) -source=pkg/scheduler/k8s_utils/k8s_utils.go -destination=pkg/scheduler/k8s_utils/k8s_utils_mock.go -package=k8s_utils

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="./hack/boilerplate.go.txt" paths="./pkg/apis/..."

.PHONY: gen-license
gen-license: addlicense
	$(ADDLICENSE) -c "NVIDIA CORPORATION" -s=only -l apache -v .

.PHONY: manifests
manifests: controller-gen kustomize ## Generate ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd:allowDangerousTypes=true,generateEmbeddedObjectMeta=true,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/apis/..." output:crd:artifacts:config=deployments/kai-scheduler/crds
	$(CONTROLLER_GEN) rbac:roleName=kai-podgrouper,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/podgrouper/..." paths="./cmd/podgrouper/..." output:stdout > deployments/kai-scheduler/templates/rbac/podgrouper.yaml
	$(CONTROLLER_GEN) rbac:roleName=kai-binder,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/binder/..." paths="./cmd/binder/..." output:stdout > deployments/kai-scheduler/templates/rbac/binder.yaml
	$(CONTROLLER_GEN) rbac:roleName=kai-resource-reservation,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/resourcereservation/..." paths="./cmd/resourcereservation/..." output:stdout > deployments/kai-scheduler/templates/rbac/resourcereservation.yaml
	$(CONTROLLER_GEN) rbac:roleName=kai-scheduler,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/scheduler/..." paths="./cmd/scheduler/..." output:stdout > deployments/kai-scheduler/templates/rbac/scheduler.yaml
	$(CONTROLLER_GEN) rbac:roleName=kai-node-scale-adjuster,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/nodescaleadjuster/..." paths="./cmd/nodescaleadjuster/..." output:stdout > deployments/kai-scheduler/templates/rbac/nodescaleadjuster.yaml
	$(CONTROLLER_GEN) rbac:roleName=kai-podgroup-controller,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/podgroupcontroller/..." paths="./cmd/podgroupcontroller/..." output:stdout > deployments/kai-scheduler/templates/rbac/podgroupcontroller.yaml
	$(CONTROLLER_GEN) rbac:roleName=queuecontroller,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/queuecontroller/..." paths="./cmd/queuecontroller/..." output:stdout > deployments/kai-scheduler/templates/rbac/queuecontroller.yaml
	$(CONTROLLER_GEN) rbac:roleName=kai-admission,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/admission/..." paths="./cmd/admission/..." output:stdout > deployments/kai-scheduler/templates/rbac/admission.yaml
	$(CONTROLLER_GEN) rbac:roleName=kai-operator,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/operator/..." paths="./cmd/operator/..." output:stdout > deployments/kai-scheduler/templates/rbac/operator.yaml

	# Add Kubernetes copyright to files derived from Kubernetes projects
	@for f in $(K8S_COPYRIGHTED_MANIFEST_FILES); do \
		cat ./hack/boilerplate.yaml.kb.txt $$f > $$f.tmp && mv $$f.tmp $$f; \
	done

	$(MAKE) gen-license

.PHONY: clients
clients: ## Generate clients.
	hack/update-client.sh

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: mockgen
mockgen: $(MOCKGEN) ## Download mockgen locally if necessary.
$(MOCKGEN): $(LOCALBIN)
	test -s $(LOCALBIN)/mockgen || GOBIN=$(LOCALBIN) go install go.uber.org/mock/mockgen@$(MOCKGEN_VERSION)

.PHONY: addlicense
addlicense: $(ADDLICENSE) ## Download google-addlicense locally if necessary.
$(ADDLICENSE): $(LOCALBIN)
	test -s $(LOCALBIN)/addlicense || GOBIN=$(LOCALBIN) go install github.com/google/addlicense@$(ADDLICENSE_VERSION)


KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE)
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) --output install_kustomize.sh && bash install_kustomize.sh $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); rm install_kustomize.sh; }

# Benchmark targets
BENCHSTAT ?= $(LOCALBIN)/benchstat
BENCH_OUTPUT ?= benchmark-results.txt

.PHONY: benchstat
benchstat: $(BENCHSTAT)
$(BENCHSTAT): $(LOCALBIN)
	test -s $(LOCALBIN)/benchstat || GOBIN=$(LOCALBIN) go install golang.org/x/perf/cmd/benchstat@latest

.PHONY: benchmark
benchmark: envtest ## Run benchmarks and output results (use BENCH_OUTPUT=file.txt to customize output)
	@echo "Running benchmarks..."
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path --bin-dir $(LOCALBIN))" \
	go test -bench=. -benchmem -count=6 -run=^$$ ./pkg/scheduler/actions/... | tee $(BENCH_OUTPUT)

.PHONY: benchmark-docker
benchmark-docker: builder gocache ## Run benchmarks in Docker
	@echo "Running benchmarks in Docker..."
	${DOCKER_GO_COMMAND} make benchmark

.PHONY: benchmark-compare
benchmark-compare: benchstat ## Compare benchmark results (requires baseline.txt and benchmark-results.txt)
	@echo "Comparing benchmarks..."
	$(BENCHSTAT) baseline.txt benchmark-results.txt

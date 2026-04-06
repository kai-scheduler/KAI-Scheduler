# KAI Scheduler - Agent Development Guide

KAI Scheduler is a Kubernetes scheduler optimized for GPU resource allocation in AI/ML workloads, built on kube-batch with a modular plugin architecture.

## Build/Lint/Test Commands

### Building
```bash
make build                    # Build all services (Docker-based)
make build-go SERVICE_NAME=scheduler  # Build single service
```

### Linting
```bash
make lint                     # Run all linters (fmt, vet, golangci-lint)
make fmt-go                   # Format Go code
make vet-go                   # Run go vet
```

### Testing
#### unit and integration tests

- Test files MUST ALWAYS be in the same directory as code
- Test files names MUST ALWAYS end in `_test.go`. Example: `resolver_test.go`

```bash
make test                     # Run all tests (unit + helm chart tests)

# Run a single test file
ginkgo -v ./pkg/scheduler/actions/allocate

# Run a specific test function
ginkgo -v --focus "TestHandleAllocation" ./pkg/scheduler/actions/allocate

# Run tests with Ginkgo (for integration tests)
ginkgo -v --focus "test name pattern" ./pkg/binder/controllers/integration_tests

# Run tests with envtest (requires setup-envtest)
make envtest
KUBEBUILDER_ASSETS="$(bin/setup-envtest use 1.34.0 -p path --bin-dir bin)" go test ./pkg/... -timeout 30m
```
#### E2E tests

E2E tests run against a real Kubernetes using [`kind`](https://kind.sigs.k8s.io/) cluster and are located in `test/e2e/suites/`.

```bash
# Run locally with Kind (recommended for development)
./hack/run-e2e-kind.sh                          # Full e2e suite
./hack/run-e2e-kind.sh --preserve-cluster       # Keep cluster after tests
./hack/run-e2e-kind.sh --local-images-build     # Build images locally

# Run specific test suites (requires cluster with KAI installed)
ginkgo -r --randomize-all ./test/e2e/suites/allocate
ginkgo -r --randomize-all --focus "quota" ./test/e2e/suites

# Run with verbose output and trace
ginkgo -r --randomize-all --trace -vv ./test/e2e/suites/preempt
```

### Code Generation
```bash
make generate                 # Generate DeepCopy methods
make manifests                # Generate CRDs and RBAC
make clients                  # Generate client code
make generate-mocks           # Generate mock implementations
make validate                 # Verify generated code is up to date. Also format and vet codebase
```

## Repository Structure

### Core Services (`/cmd/` and `/pkg/`)
- `scheduler` - Core GPU-aware batch scheduler with plugins, actions, and cache
- `binder` - Pod binding execution with GPU sharing support
- `operator` - Lifecycle management of all KAI components
- `podgrouper` - PodGroup creation from workloads (Kubeflow, Spark, Ray, etc.)
- `admission` - Validating/mutating webhooks for KAI resources
- `queuecontroller` - Queue resource management and status updates
- `podgroupcontroller` - PodGroup lifecycle and status management
- `resourcereservation` - GPU resource reservation for pending pods
- `nodescaleadjuster` - Node scaling integration for autoscalers

### Supporting Packages (`/pkg/`)
- `apis` - Custom resource definitions (Queue, PodGroup, BindRequest)
- `common` - Shared utilities and constants

### Tools (`/cmd/`)
- `fairshare-simulator` - Simulate fairshare scheduling decisions
- `time-based-fairshare-simulator` - Time-based scheduling simulation
- `snapshot-tool` - Cluster state snapshot utilities
- `scalingpod` - Helper for scaling pod operations

## Code Style Guidelines

### Import Organization
Organize imports in three groups separated by blank lines:
```go
import (
    // 1. Standard library
    "context"
    "fmt"

    // 2. External dependencies
    v1 "k8s.io/api/core/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"

    // 3. Internal packages
    "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
)
```

### Naming Conventions
- **Files**: snake_case (`bind_request_controller.go`, `node_info.go`)
- **Types**: PascalCase (`SchedulerCache`, `NodeInfo`)
- **Interfaces**: `-er` suffix or `Interface` (`Plugin`, `SchedulerLogger`)
- **Functions**: PascalCase exported, camelCase unexported
- **Boolean functions**: `is`/`has`/`should` prefix (`IsTaskAllocatable`)
- **Constants**: PascalCase exported, camelCase unexported

### Logging
```go
log.InfraLogger.V(6).Infof("Task <%s/%s> allocatable on node <%s>", ...)  // V(2-3) operational, V(5-6) debug
logger := log.FromContext(ctx)  // Controller logging
logger.Info("Binding pod", "namespace", pod.Namespace, "name", pod.Name)
```

### Comments
- Apache 2.0 + NVIDIA copyright headers on all files
- GoDoc-style for exported functions/types
- kubebuilder RBAC markers: `// +kubebuilder:rbac:groups=core,resources=pods,verbs=get`
- Avoid obvious comments; explain "why" not "what"

### General Patterns
- Context as first parameter: `func Foo(ctx context.Context, ...)`
- Pointer receivers for methods that modify state
- Interfaces in `interface.go` files
- Constructor functions return interface types: `func New(...) Cache`
- Use `defer` for cleanup and state restoration

## Linter Configuration

Enabled linters (see `build/lint/.golangci.yaml`):
- `gofmt` `unused` `goconst` `errcheck` `govet`

Test files (`*_test.go`) have relaxed rules for `goconst`, `errcheck`, `govet`.

## Pull Request Requirements

### PR Title Format (Conventional Commits)
PR titles must follow semantic format: `<type>(<scope>): <description>`

**Types**: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`

**Scopes** (optional): `scheduler`, `binder`, `podgrouper`, `admission`, `operator`, `queue-controller`, `pod-group-controller`, `resource-reservation`, `chart`, `api`, `node-scale-adjuster`, `ci`, `release`, `docs`, `deps`

### PR Description format

When opening a PR, use the template in .github/pull_request_template.md for the PR description

### Changelog Requirements
- You must update `CHANGELOG.md` for PRs to `main` or version branches (`v*.*`) for behavior changes: ones that add functionality, fix bugs, change APIs, or introduce significant performance improvements. Not needed for refactors, documentations, tests, and CI changes.
- Add `skip-changelog` or `dependencies` label to skip this check

### CI Checks (on-pr.yaml)
PRs trigger: `make validate` → `make test` → `make build` → E2E tests
- Docs-only changes (`.md` files, `docs/`) skip build/test
- E2E tests run with Ginkgo: `ginkgo -r --randomize-all ./test/e2e/suites`

## General Rules
- Use `git mv` when moving files to preserve history
- DO NOT add obvious comments that duplicate the code.
    - Write comments ONLY when ABSOLUTELY nessessary
    - Keep Comments short, explicit and concise
    - DO NOT use `I`, `we` or any other pronoun
- When performing a major change, run `make validate` after changes

## Philosophy & Design

- Documentation can be found in [`docs`](docs/) folder

- Design documents for major features are in [`docs/developer/designs/`](docs/developer/designs/):

- Usage Examples are in [`examples`](/examples/)
# High-Level Design: API and SDK Separation to Independent Repository

## Overview

This document outlines a design for extracting KAI-Scheduler's API definitions and shared utilities into a **separate repository** (`KAI-Scheduler-API`) with independent versioning. This enables true decoupling between API contracts and service implementations, allowing services to upgrade dependencies at their own pace.

## Current State

### Architecture
KAI-Scheduler currently uses a **single Go module** architecture where:
- All API types reside in `/pkg/apis/` (scheduling/v1alpha2, v2alpha2, v2 + kai/v1, v1alpha1)
- All services (scheduler, binder, queuecontroller, etc.) are in the same module
- Shared utilities are in `/pkg/common/`
- Generated clients are in `/pkg/apis/client/`

### Problems

1. **Tight Coupling**: All services must use the same dependency versions
   - Upgrading Go version requires coordinated update across all services
   - Kubernetes API version upgrades affect all components simultaneously
   - Cannot test API changes in isolation

2. **Version Lock-in**: Single `go.mod` forces all services to share dependencies
   ```
   k8s.io/api v0.34.3
   k8s.io/client-go v0.34.3
   sigs.k8s.io/controller-runtime v0.22.3
   ```
   Any service requiring a different version creates conflicts

3. **Circular Dependencies**: Risk of import cycles between services and common packages
   - `pkg/common/podgroup/` imports `scheduling/v2alpha2`
   - Services import from `pkg/common/`
   - Creates fragile dependency graph

4. **API Contract Ambiguity**: No clear boundary between "public API" and "internal implementation"
   - Services can import any package from any other service
   - No enforced separation of concerns

5. **Build Complexity**: Changes to one service require rebuilding all services
   - Increases CI/CD time
   - Makes debugging difficult

6. **No Independent API Evolution**: Cannot version and release APIs separately from services
   - API consumers outside kai-scheduler cannot pin to stable API versions
   - Breaking API changes force immediate service updates

## Proposed Architecture

### Separate Repository Structure

Create a **new standalone repository** for the SDK:

**New Repository**: `kai-scheduler/KAI-Scheduler-API`

```
KAI-Scheduler-API/                          # NEW REPOSITORY
├── go.mod                        # module github.com/kai-scheduler/KAI-Scheduler-API
├── api/
│   └── scheduling/
│       ├── v1alpha2/             # BindRequest
│       ├── v2alpha2/             # PodGroup
│       └── v2/                   # Queue
├── constants/                    # Shared constants
├── utilities/                    # Shared utilities
│   ├── resources/                # Resource abstractions (GPU, DRA)
│   ├── podgroup/                 # PodGroup helper functions
│   └── k8s_utils/                # K8s utilities
├── client/                       # Generated clientset/informers/listers
│   ├── clientset/
│   ├── informers/
│   └── listers/
├── CHANGELOG.md                  # SDK-specific changelog
└── README.md                     # SDK documentation
```

**KAI-Scheduler Repository** (updated):

```
kai-scheduler/                    # EXISTING REPOSITORY
├── go.mod                        # Imports KAI-Scheduler-API as normal dependency
│   require (
│       github.com/kai-scheduler/KAI-Scheduler-API v0.1.0
│   )
├── pkg/                          # Service implementations
│   ├── scheduler/
│   ├── binder/
│   ├── queuecontroller/
│   ├── operator/
│   │   └── api/kai/v1/           # Operator-specific configs stay here
│   └── ...
├── cmd/                          # Service entrypoints
└── (pkg/apis/, pkg/common/ REMOVED - moved to KAI-Scheduler-API)
```

### Migration Phases

#### Phase 1: Create SDK Repository

**Goal**: Establish independent SDK repository with CI/CD and versioning

**Steps**:
1. **Create new GitHub repository**: `kai-scheduler/KAI-Scheduler-API`

2. **Initialize repository**:
   ```bash
   cd /tmp
   mkdir KAI-Scheduler-API && cd KAI-Scheduler-API
   git init
   go mod init github.com/kai-scheduler/KAI-Scheduler-API
   ```

3. **Copy code from kai-scheduler** (preserving git history):
   ```bash
   # In kai-scheduler repo
   git log --follow --all -- pkg/apis/scheduling > /tmp/api-history.txt

   # Filter-branch or git-subtree to extract history
   git subtree split --prefix=pkg/apis/scheduling -b sdk-apis
   git subtree split --prefix=pkg/common -b sdk-common

   # In KAI-Scheduler-API repo
   git remote add kai-scheduler ../kai-scheduler
   git fetch kai-scheduler sdk-apis
   git merge -s ours --no-commit --allow-unrelated-histories kai-scheduler/sdk-apis
   git read-tree --prefix=api/scheduling/ -u kai-scheduler/sdk-apis
   ```

4. **Reorganize structure**:
   ```
   api/scheduling/v1alpha2/       # BindRequest
   api/scheduling/v2alpha2/       # PodGroup
   api/scheduling/v2/             # Queue
   constants/                     # From pkg/common/constants
   utilities/resources/           # From pkg/common/resources
   utilities/podgroup/            # From pkg/common/podgroup
   utilities/k8s_utils/          # From pkg/common/k8s_utils
   ```

5. **Set up CI/CD**:
   - GitHub Actions for linting, testing, building
   - Automated client code generation
   - Release automation (semantic versioning with tags)

6. **Create initial release**: `v0.1.0`
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```

**Result**: SDK is now a standalone repository with `v0.1.0` release available.

#### Phase 2: Migrate kai-scheduler to Use SDK

**Goal**: Update kai-scheduler to depend on external KAI-Scheduler-API package

**Steps**:
1. **Update `go.mod`** in kai-scheduler:
   ```go
   module github.com/kai-scheduler/KAI-scheduler

   require (
       github.com/kai-scheduler/KAI-Scheduler-API v0.1.0
       k8s.io/api v0.34.3
       // ... other dependencies
   )

   // Optional: For local development
   // replace github.com/kai-scheduler/KAI-Scheduler-API => ../KAI-Scheduler-API
   ```

2. **Update all imports**:
   ```bash
   # Automated replacement
   find pkg cmd -name '*.go' -type f -exec sed -i '' \
     's|github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling|github.com/kai-scheduler/KAI-Scheduler-API/api/scheduling|g' {} +
   find pkg cmd -name '*.go' -type f -exec sed -i '' \
     's|github.com/kai-scheduler/KAI-scheduler/pkg/common/constants|github.com/kai-scheduler/KAI-Scheduler-API/constants|g' {} +
   find pkg cmd -name '*.go' -type f -exec sed -i '' \
     's|github.com/kai-scheduler/KAI-scheduler/pkg/common/resources|github.com/kai-scheduler/KAI-Scheduler-API/utilities/resources|g' {} +
   find pkg cmd -name '*.go' -type f -exec sed -i '' \
     's|github.com/kai-scheduler/KAI-scheduler/pkg/common/podgroup|github.com/kai-scheduler/KAI-Scheduler-API/utilities/podgroup|g' {} +
   find pkg cmd -name '*.go' -type f -exec sed -i '' \
     's|github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils|github.com/kai-scheduler/KAI-Scheduler-API/utilities/k8s_utils|g' {} +
   ```

3. **Remove old code**:
   ```bash
   git rm -r pkg/apis/scheduling
   git rm -r pkg/common/constants
   git rm -r pkg/common/resources
   git rm -r pkg/common/podgroup
   git rm -r pkg/common/k8s_utils
   ```

4. **Update code generation** in Makefile:
   ```diff
   - API_DIR := pkg/apis
   + # API generation now happens in KAI-Scheduler-API repo
   ```

5. **Run validation**:
   ```bash
   go mod tidy
   make validate
   make test
   ./hack/run-e2e-kind.sh
   ```

**Result**: kai-scheduler now imports KAI-Scheduler-API as a normal external dependency.

#### Phase 3: Establish SDK Versioning Workflow

**Goal**: Define process for SDK updates and kai-scheduler upgrades

**SDK Release Process**:
1. Make changes in KAI-Scheduler-API repository
2. Run SDK tests: `make test`
3. Update SDK CHANGELOG.md
4. Create semantic version tag: `git tag v1.1.0` (minor for features, v1.0.1 for patches)
5. Push tag: `git push origin v1.1.0`
6. GitHub Actions automatically creates release

**kai-scheduler Upgrade Process**:
1. Update `go.mod`: `go get github.com/kai-scheduler/KAI-Scheduler-API@v1.1.0`
2. Run tests to verify compatibility
3. Update kai-scheduler CHANGELOG.md noting SDK upgrade
4. Create PR with version bump

**Result**: Clear separation between API evolution and service implementation changes.

### API Versioning Strategy

**SDK Versioning (Semantic Versioning)**:
- **MAJOR** (v1.x.x → v2.x.x): Breaking API changes (field removals, type changes)
- **MINOR** (v1.0.x → v1.1.x): New features, new fields (backward compatible)
- **PATCH** (v1.0.0 → v1.0.1): Bug fixes, documentation updates

**kai-scheduler Versioning**:
- Independent of SDK versioning
- kai-scheduler v0.15.0 might use KAI-Scheduler-API v1.2.0
- kai-scheduler v0.16.0 might still use KAI-Scheduler-API v1.2.0 (no API changes needed)

**Decision on kai/v1 (Operator Config)**:

**Keep in kai-scheduler repository** (Recommended)
- Operator configs (`pkg/apis/kai/v1/`) are internal to kai-scheduler
- Not consumed by external services or other schedulers
- Only used by operator to manage kai-scheduler components
- Location: `pkg/operator/api/kai/v1/`

**Rationale**: Operator configs are implementation details, not public API contracts.

### Dependency Graph (After Migration)

```
┌──────────────────────────────────────────────────────┐
│  KAI-Scheduler-API Repository (github.com/kai-scheduler/KAI-Scheduler-API)│
│  Version: v1.0.0, v1.1.0, v1.2.0, etc.               │
│  ┌────────────────────────────────────────────────┐  │
│  │ - API types (v1alpha2, v2alpha2, v2)           │  │
│  │ - Constants (labels, annotations)              │  │
│  │ - Utilities (resources, podgroup, k8s_utils)   │  │
│  │ - Generated clients (clientset/informers)      │  │
│  └────────────────────────────────────────────────┘  │
│  Dependencies:                                       │
│  - k8s.io/api v0.34.3                                │
│  - k8s.io/client-go v0.34.3                          │
│  - sigs.k8s.io/controller-runtime v0.22.3            │
└───────────────────────┬──────────────────────────────┘
                        │
                        │ go get github.com/kai-scheduler/KAI-Scheduler-API@v1.2.0
                        │
┌───────────────────────▼──────────────────────────────┐
│  kai-scheduler Repository                            │
│  (github.com/kai-scheduler/KAI-scheduler)            │
│  ┌────────────────────────────────────────────────┐  │
│  │ go.mod:                                        │  │
│  │   require github.com/kai-scheduler/KAI-Scheduler-API v1.2.0│
│  └────────────────────────────────────────────────┘  │
│  ┌─────────────────────────────────┐                 │
│  │ pkg/scheduler/                  │                 │
│  │ import "github.com/kai-scheduler/KAI-Scheduler-API/..."    │
│  └─────────────────────────────────┘                 │
│  ┌─────────────────────────────────┐                 │
│  │ pkg/binder/                     │                 │
│  │ import "github.com/kai-scheduler/KAI-Scheduler-API/..."    │
│  └─────────────────────────────────┘                 │
│  ┌─────────────────────────────────┐                 │
│  │ pkg/queuecontroller/            │                 │
│  │ import "github.com/kai-scheduler/KAI-Scheduler-API/..."    │
│  └─────────────────────────────────┘                 │
│  ┌─────────────────────────────────┐                 │
│  │ pkg/operator/api/kai/v1/        │                 │
│  │ (Operator configs stay here)    │                 │
│  └─────────────────────────────────┘                 │
│  ... (other services)                                │
└──────────────────────────────────────────────────────┘

External Consumers (Future):
┌──────────────────────────────────────┐
│  Custom Scheduler (example)          │
│  import "github.com/kai-scheduler/KAI-Scheduler-API/api/scheduling/v2"│
│  - Can use Queue, PodGroup types     │
│  - Can use resource utilities        │
└──────────────────────────────────────┘
```

## Benefits

1. **True Decoupled Dependencies**
   - SDK can upgrade k8s.io/api independently from kai-scheduler
   - kai-scheduler services can pin to specific SDK versions (v1.0.0, v1.1.0, etc.)
   - SDK can adopt new Kubernetes versions before kai-scheduler is ready
   - No forced upgrades - kai-scheduler upgrades SDK on its own schedule

2. **Independent Versioning and Stability**
   - SDK follows semantic versioning (v1.x.x, v2.x.x)
   - API contracts have explicit version guarantees
   - kai-scheduler can upgrade SDK in controlled, tested phases
   - External tools can depend on stable SDK versions

3. **Clear API Ownership**
   - SDK repository has dedicated owners and release process
   - API changes go through SDK review process first
   - Services cannot accidentally break API contracts
   - Public API surface is explicit and documented

4. **Enables External Ecosystem**
   - Other schedulers can consume KAI-Scheduler-API for Queue/PodGroup types
   - Tools can use SDK utilities without depending on kai-scheduler
   - Custom controllers can watch KAI resources using SDK clients
   - Community contributions to SDK are isolated from service logic

5. **Simplified Dependency Management**
   - Single source of truth for API types
   - kai-scheduler's `go.mod` is cleaner (one SDK dependency vs. many internal packages)
   - Easier to identify unused dependencies in each repository
   - SDK has minimal dependencies (only k8s.io/* packages)

6. **Better CI/CD**
   - SDK tests run independently in separate CI pipeline
   - SDK releases are automated with semantic versioning
   - kai-scheduler only rebuilds when it chooses to upgrade SDK
   - Parallel development: SDK changes don't block kai-scheduler PRs

7. **Cleaner Git History**
   - API changes tracked in SDK repository
   - Service changes tracked in kai-scheduler repository
   - Easier to review PRs (API changes vs. implementation changes)
   - Git blame and history are more focused

## Trade-offs

### Disadvantages

1. **Coordination Overhead**
   - API changes require PRs in two repositories (KAI-Scheduler-API, then kai-scheduler)
   - Breaking changes need coordinated releases
   - More complex release process (release SDK first, then kai-scheduler)

2. **Migration Effort**
   - Create new repository with CI/CD infrastructure
   - Preserve git history for moved code (git-subtree/filter-branch)
   - Update all imports across kai-scheduler codebase
   - Update documentation, build scripts, and developer workflows

3. **Developer Experience Complexity**
   - Developers must clone two repositories
   - Local development requires `replace` directives in go.mod for SDK changes
   - Cross-repo debugging is more complex
   - Need to understand which repo to make changes in

4. **Breaking Changes Are Harder**
   - Cannot make breaking API changes and service changes in single PR
   - Must maintain backward compatibility or coordinate major version bump
   - Deprecation timeline must be explicit (announce in v1.x, remove in v2.0)

5. **Initial Setup Cost**
   - Set up new repository infrastructure (CI/CD, branch protection, code owners)
   - Establish SDK release process and automation
   - Create SDK documentation and contribution guidelines
   - Train team on new workflows

### Mitigation Strategies

1. **Local Development Workflow**
   - Document use of `replace` directive for simultaneous SDK + kai-scheduler development:
     ```go
     // kai-scheduler/go.mod (local dev only)
     replace github.com/kai-scheduler/KAI-Scheduler-API => ../KAI-Scheduler-API
     ```
   - Create `make` target for local SDK linking: `make link-local-sdk`

2. **Automated Tooling**
   - Script to automate import path updates
   - SDK release automation with GitHub Actions
   - Dependency update bot (Dependabot) for kai-scheduler SDK upgrades

3. **Clear Contribution Guidelines**
   - Document in CLAUDE.md: "API changes go in KAI-Scheduler-API first"
   - SDK README explains contribution process
   - kai-scheduler docs link to SDK repository

4. **Versioning Policy**
   - Publish SDK versioning policy (semantic versioning rules)
   - Maintain CHANGELOG.md in both repositories
   - Deprecation warnings for 2 minor versions before removal (e.g., deprecate in v1.5, remove in v2.0)

5. **Monorepo Tools for Cloning**
   - Provide script: `./hack/setup-dev.sh` that clones both repos in correct layout
   - VSCode workspace file that opens both repositories

## Migration Checklist

### Phase 1: SDK Repository Creation

**SDK Repository Setup**:
- [ ] Create new GitHub repository: `kai-scheduler/KAI-Scheduler-API`
- [ ] Set up branch protection rules (require reviews, CI passing)
- [ ] Configure code owners (CODEOWNERS file)
- [ ] Initialize `go.mod`: `module github.com/kai-scheduler/KAI-Scheduler-API`
- [ ] Set up CI/CD pipeline (GitHub Actions):
  - [ ] Linting (golangci-lint)
  - [ ] Unit tests
  - [ ] Code generation validation
  - [ ] Release automation (on tag push)

**Code Migration (Preserving History)**:
- [ ] Use `git subtree split` or `git filter-branch` to extract:
  - [ ] `pkg/apis/scheduling/*` → `api/scheduling/` (with git history)
  - [ ] `pkg/common/constants/*` → `constants/` (with git history)
  - [ ] `pkg/common/resources/*` → `utilities/resources/` (with git history)
  - [ ] `pkg/common/podgroup/*` → `utilities/podgroup/` (with git history)
  - [ ] `pkg/common/k8s_utils/*` → `utilities/k8s_utils/` (with git history)
  - [ ] `pkg/apis/client/*` → `client/` (or regenerate)
- [ ] Update package declarations in all moved files
- [ ] Update import paths within SDK code to use `github.com/kai-scheduler/KAI-Scheduler-API`

**SDK Documentation**:
- [ ] Create SDK README.md with:
  - [ ] Overview of API types (Queue, PodGroup, BindRequest)
  - [ ] Installation instructions
  - [ ] Usage examples
  - [ ] Contributing guidelines
- [ ] Create SDK CHANGELOG.md
- [ ] Add LICENSE file (Apache 2.0 + NVIDIA copyright)
- [ ] Create CLAUDE.md for SDK-specific development guidelines

**SDK Validation and Release**:
- [ ] Run `make lint` in SDK repo
- [ ] Run `make test` in SDK repo
- [ ] Verify code generation: `make clients` (if applicable)
- [ ] Create initial release tag: `git tag v0.1.0`
- [ ] Push tag to trigger release: `git push origin v0.1.0`
- [ ] Verify release appears in GitHub Releases

### Phase 2: kai-scheduler Migration to SDK Dependency

**Update Dependencies**:
- [ ] Update `go.mod` to require SDK:
  ```go
  require github.com/kai-scheduler/KAI-Scheduler-API v0.1.0
  ```
- [ ] Add optional local development replace directive (documented in comments)

**Update Imports** (Automated):
- [ ] Create import migration script (`hack/migrate-sdk-imports.sh`)
- [ ] Run script to update all imports:
  - [ ] `pkg/apis/scheduling/*` → `github.com/kai-scheduler/KAI-Scheduler-API/api/scheduling/*`
  - [ ] `pkg/common/constants` → `github.com/kai-scheduler/KAI-Scheduler-API/constants`
  - [ ] `pkg/common/resources` → `github.com/kai-scheduler/KAI-Scheduler-API/utilities/resources`
  - [ ] `pkg/common/podgroup` → `github.com/kai-scheduler/KAI-Scheduler-API/utilities/podgroup`
  - [ ] `pkg/common/k8s_utils` → `github.com/kai-scheduler/KAI-Scheduler-API/utilities/k8s_utils`
- [ ] Manually review import changes for correctness

**Remove Old Code**:
- [ ] `git rm -r pkg/apis/scheduling/`
- [ ] `git rm -r pkg/common/constants/`
- [ ] `git rm -r pkg/common/resources/`
- [ ] `git rm -r pkg/common/podgroup/`
- [ ] `git rm -r pkg/common/k8s_utils/`
- [ ] Update `.gitignore` if needed

**Update Build System**:
- [ ] Remove SDK code generation from Makefile (now happens in SDK repo)
- [ ] Update `make clients` to note that clients come from SDK
- [ ] Update `make generate` to exclude moved packages
- [ ] Test all make targets: `validate`, `lint`, `test`, `build`

**Validation**:
- [ ] Run `go mod tidy`
- [ ] Run `make validate` to ensure no regressions
- [ ] Run `make test` to verify all tests pass
- [ ] Run `make build` to verify all services compile
- [ ] Run E2E tests: `./hack/run-e2e-kind.sh`
- [ ] Verify CRD installation still works (helm chart)

**Documentation Updates**:
- [ ] Update CLAUDE.md with SDK reference
- [ ] Update README.md to mention SDK dependency
- [ ] Update contribution guidelines (API changes go in SDK first)
- [ ] Create `docs/developer/sdk-development.md` with local workflow
- [ ] Update CHANGELOG.md noting SDK separation

**Developer Tooling**:
- [ ] Create `hack/setup-dev.sh` script to clone both repos
- [ ] Create VSCode workspace file (`kai-scheduler.code-workspace`) for multi-repo setup
- [ ] Document local development with `replace` directive

### Phase 3: Establish SDK Versioning Workflow

**SDK Release Process Documentation**:
- [ ] Document semantic versioning policy in SDK README
- [ ] Create SDK release checklist
- [ ] Set up GitHub release template for SDK
- [ ] Document deprecation policy (2 minor versions warning)

**kai-scheduler SDK Upgrade Process Documentation**:
- [ ] Document how to test SDK upgrades locally
- [ ] Create upgrade checklist template
- [ ] Set up Dependabot for SDK version monitoring
- [ ] Document rollback procedure if SDK upgrade breaks tests

## Examples

### Before (Current State)
```go
// kai-scheduler/pkg/scheduler/cache/cache.go
import (
    "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
    "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
    "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
    "github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)
```

### After (Separate Repository)
```go
// kai-scheduler/pkg/scheduler/cache/cache.go
import (
    "github.com/kai-scheduler/KAI-Scheduler-API/api/scheduling/v1alpha2"
    "github.com/kai-scheduler/KAI-Scheduler-API/api/scheduling/v2alpha2"
    "github.com/kai-scheduler/KAI-Scheduler-API/constants"
    "github.com/kai-scheduler/KAI-Scheduler-API/utilities/resources"
)
```

### SDK Module Definition
```go
// KAI-Scheduler-API/go.mod (separate repository)
module github.com/kai-scheduler/KAI-Scheduler-API

go 1.26

require (
    k8s.io/api v0.34.3
    k8s.io/apimachinery v0.34.3
    k8s.io/client-go v0.34.3
    sigs.k8s.io/controller-runtime v0.22.3
)

// No replace directives - SDK is standalone
```

### kai-scheduler Module Definition
```go
// kai-scheduler/go.mod
module github.com/kai-scheduler/KAI-scheduler

go 1.26

require (
    github.com/kai-scheduler/KAI-Scheduler-API v0.1.0  // External dependency
    k8s.io/api v0.34.3
    k8s.io/client-go v0.34.3
    // ... other dependencies
)

// Optional: For local SDK development
// Uncomment to test SDK changes before releasing
// replace github.com/kai-scheduler/KAI-Scheduler-API => ../KAI-Scheduler-API
```

### Local Development Workflow
```bash
# Developer working on API change + service change simultaneously

# 1. Clone both repositories
git clone git@github.com:kai-scheduler/KAI-Scheduler-API.git
git clone git@github.com:kai-scheduler/KAI-scheduler.git kai-scheduler

# 2. Make API changes in KAI-Scheduler-API
cd KAI-Scheduler-API
# ... edit api/scheduling/v2/queue_types.go
make test

# 3. Enable local development mode in kai-scheduler
cd ../kai-scheduler
# Uncomment replace directive in go.mod:
echo 'replace github.com/kai-scheduler/KAI-Scheduler-API => ../KAI-Scheduler-API' >> go.mod

# 4. Make service changes using new API
# ... edit pkg/scheduler/cache/cache.go
make test

# 5. When ready, release SDK first
cd ../KAI-Scheduler-API
git commit -m "feat(api): add new Queue field"
git tag v1.1.0
git push origin v1.1.0

# 6. Then upgrade kai-scheduler to use released SDK
cd ../kai-scheduler
go get github.com/kai-scheduler/KAI-Scheduler-API@v1.1.0
# Remove or comment out replace directive
git commit -m "feat(scheduler): use new Queue field from KAI-Scheduler-API v1.1.0"
```

## Decisions

1. **Repository Naming Convention**
   - **Decision**: `kai-scheduler/KAI-Scheduler-API` ✓

2. **API Versioning - Start at v1.0.0 or v0.1.0?**
   - **Decision**: `v0.1.0` ✓
   - Allows flexibility for breaking changes as we stabilize the SDK

3. **Should generated clients (clientset/informers) be in SDK?**
   - **Decision**: Yes, include generated clients ✓
   - Enables external consumers to use SDK without running code generation

4. **What should the SDK repository include in terms of CRDs?**
   - **Decision**: Include CRD YAML manifests in SDK at `config/crd/` ✓
   - Manifests are a form of contract with consumers
   - kai-scheduler helm chart remains the primary installation method

5. **How to handle breaking changes across repos?**
   - **Decision**: Defer to later; maintain single API version for now ✓
   - Keeps dependency relationship clear during initial extraction
   - Will establish deprecation policy when needed

6. **Should operator configs (`pkg/apis/kai/v1/`) move to SDK?**
   - **Decision**: Keep in `kai-scheduler/pkg/operator/api/kai/v1/` ✓
   - Operator configs are internal kai-scheduler implementation details

7. **What about feature gates (`pkg/common/feature_gates/`)?**
   - **Decision**: Keep in kai-scheduler ✓
   - Feature gates control runtime behavior, not API contracts

8. **How to handle git history migration?**
   - **Decision**: Use `git subtree split` to preserve full commit history ✓
   - **Why this is necessary**:
     - **Attribution**: Apache 2.0 license requires preserving original authors' credit
     - **Debugging**: Enables `git blame` to trace API design decisions back to original context
     - **Code archaeology**: Future maintainers can understand evolution of API design choices
     - **Migration audit**: Provides clear lineage showing code moved (not rewritten) from kai-scheduler
     - **Best practice**: Matches Kubernetes ecosystem pattern (k8s.io/api preserved history from main repo)
   - Alternative (fresh start) would lose 2+ years of valuable context and violate attribution requirements

## References

- runai-engine repository structure analysis (~/Work/Dev/runai-engine) - multi-module monorepo pattern
- [Go Modules Multi-Module Repos](https://github.com/golang/go/wiki/Modules#multi-module-repositories)
- Kubernetes API repository pattern:
  - [k8s.io/api](https://github.com/kubernetes/api) - Separate API types repository
  - [k8s.io/client-go](https://github.com/kubernetes/client-go) - Separate client library
- [Kubernetes API versioning conventions](https://kubernetes.io/docs/reference/using-api/api-overview/#api-versioning)
- [Semantic Versioning 2.0.0](https://semver.org/)
- Examples of separate SDK repositories:
  - [Karpenter API](https://github.com/aws/karpenter-core) vs [Karpenter Provider](https://github.com/aws/karpenter-provider-aws)

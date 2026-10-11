# pkg/podgrouper

Creates `PodGroup`s for pods by walking to the top owner and applying a per-workload plugin.

## Adding support for a workload type

1. Add `podgrouper/plugins/<workload>/` implementing `grouper.Grouper` (`plugins/grouper/`). Embed or reuse `defaultgrouper` and `minmember` rather than reimplementing min-member and priority logic.
2. Register the plugin by GroupVersionKind in `NewDefaultPluginsHub` (`podgrouper/hub/hub.go`).
3. Add RBAC markers (`+kubebuilder:rbac`) for the new kind in `hub/hub.go`, including `/finalizers` with `patch;update;create`, then run `make manifests`. Without this the podgrouper cannot read the owner at runtime.
4. Unit tests next to the plugin; if the workload comes from a third-party operator, add an e2e under `test/e2e/suites/integrations` (setup scripts in `hack/third_party_integrations`).

- `plugins/skiptopowner` lets a workload stop owner traversal at a given kind; check it before special-casing in a new plugin.
- `pod_controller_test.go` / `suite_test.go` hold the Ginkgo + envtest suite for the controller.

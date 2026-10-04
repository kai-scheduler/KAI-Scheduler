# test

- `e2e/suites/<area>`: Ginkgo e2e suites (allocate, preempt, reclaim, integrations, ...). They need a cluster with KAI installed; locally use `./hack/run-e2e-kind.sh` (`--preserve-cluster`, `--local-images-build`).
- CI label filter: `!autoscale && !scale && !upgrade && !gitops`. Run with `go tool ginkgo` (pinned in `go.mod`), not a globally installed binary:
  ```bash
  go tool ginkgo -r --randomize-all --label-filter '!autoscale && !scale && !upgrade && !gitops' --trace -vv ./test/e2e/suites/<area>
  ```
- Reuse `e2e/modules/` (`resources`, `wait`, `context`, `configurations`, `utils`) for creating workloads and waiting on state; do not add sleeps.
- `e2e/scale/` is the scale-test setup, not part of the normal suite.
- envtest-based controller tests are in `pkg/env-tests` and in `integration_tests` directories next to the code; they run with `go test` and `KUBEBUILDER_ASSETS` (`make envtest`).

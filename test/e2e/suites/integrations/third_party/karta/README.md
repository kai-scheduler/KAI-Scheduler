# Karta generic grouping e2e

This suite verifies KAI's generic Karta fallback with `e2e.example.com/v1 CustomJob`, a minimal workload API that has no dedicated KAI podgrouper plugin. KAI can group its pods only through the matching `run.ai/v1alpha1 Karta` object created by the test.

The test Karta definition uses the non-deprecated `gangScheduling.podGroup` instruction and maps `coordinator` and `worker` components into KAI subgroups.

Before running the suite, install the upstream Karta CRD:

```bash
./hack/third_party_integrations/deploy_karta.sh
```

Then run:

```bash
ginkgo -v ./test/e2e/suites/integrations/third_party/karta
```

The suite installs its own `CustomJob` CRD and temporary pod-grouper RBAC from local YAML fixtures, creates the test `Karta` object from `customjob_karta.yaml`, restarts `pod-grouper` before the test, and removes those additions before restarting `pod-grouper` again during teardown.

# pkg/operator

The operator reconciles `kai/v1` `Config` and `SchedulingShard` into the Deployments, Services, webhooks, etc. of each KAI component.

- Each component is an `Operand` (`operands/interface.go`): `DesiredState` returns the objects to apply; `Monitor`, `IsDeployed`, `IsAvailable` feed status.
- Operand lists are wired in `controller/config_controller.go` (`ConfigReconcilerOperands`) and `controller/schedulingshard_controller.go` (`OperandsForShard`). A new operand does nothing until added there.
- `operands/known_types/` registers the Kubernetes kinds the operator owns and watches (`init()` in `known_types.go`). An operand that produces a new kind of object needs that kind registered there.
- Shared helpers live in `operands/common/` and `operands/deployable/`; reuse before writing new object builders.
- Config/shard defaults live next to the `kai/v1` types (module `github.com/kai-scheduler/api`, mirrored in `pkg/apis/kai/v1`); changing a field means updating the types (see `pkg/apis/AGENTS.md`) and the operand that consumes it.
- The chart (`deployments/kai-scheduler/`) installs the operator; the `Config` CR is applied by a post-install hook job. Component workloads are created by operands, not chart templates. Operator RBAC comes from kubebuilder markers (`make manifests`).
- Controller tests with envtest are in `controller/integration_tests`.

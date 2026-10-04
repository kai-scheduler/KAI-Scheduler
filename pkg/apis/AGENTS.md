# pkg/apis

CRD Go types. Hand-written source is `*_types.go` plus kubebuilder markers; everything else is generated.

| Group | Versions (storage version in bold) | Kinds |
|---|---|---|
| `scheduling` | `v1alpha2` (**BindRequest**), `v2alpha2` (**PodGroup**), `v2` (**Queue**) | BindRequest, PodGroup, Queue |
| `kai` | `v1` (Config, SchedulingShard), `v1alpha1` (**Topology**) | Config, SchedulingShard, Topology |

- Storage version is set by `+kubebuilder:storageversion`; `grep -rn storageversion pkg/apis` to confirm before adding a version.
- After changing types or markers run `make generate manifests clients`. Never edit `zz_generated*`, `pkg/apis/client/`, or `deployments/kai-scheduler/crds/`.
- Changing a field in a served version is an API change: needs a changelog fragment, and users upgrade CRDs through `pkg/helmhooks` (`apply_crds.go`), so check that path still works.
- `kai/v1` Config/SchedulingShard drive the operator; new fields usually need an operand change in `pkg/operator/operands/` and a default.

# pkg/apis

API types are owned by the standalone module `github.com/kai-scheduler/api` (version in `go.mod`). All Go code in this repo imports them from there; nothing outside `pkg/apis` imports the packages below.

`pkg/apis` is a mirror of that module. It is still what `make generate manifests clients` read to produce `deployments/kai-scheduler/crds/` and `pkg/apis/client/`, and those CRDs are currently identical to the module's `config/crd`.

- Do not change API types, CRD fields or API constants only in this repo. Ask first how the change should land in the `api` module and here; the two must not diverge.
- Never edit `zz_generated*`, `pkg/apis/client/` or `deployments/kai-scheduler/crds/` by hand.
- Storage versions: `grep -rn storageversion pkg/apis` (BindRequest `v1alpha2`, PodGroup `v2alpha2`, Queue `v2`, Topology `v1alpha1`). Config and SchedulingShard are `kai/v1`.
- A field change in a served version is an API change and needs a changelog fragment. Users upgrade CRDs through `pkg/helmhooks` (`apply_crds.go`), so check that path still works.

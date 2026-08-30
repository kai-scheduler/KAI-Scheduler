# FIPS-Enabled Images

Every KAI Scheduler release publishes a FIPS-enabled variant of each image alongside the regular one, tagged with a `-fips` suffix:

```
ghcr.io/kai-scheduler/kai-scheduler/scheduler:<version>        # regular
ghcr.io/kai-scheduler/kai-scheduler/scheduler:<version>-fips   # FIPS-enabled
```

## Installing

Set `global.fipsMode=on` to install or upgrade with FIPS-enabled images:

```sh
helm upgrade --install kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace --set global.fipsMode=on
```

The flag appends `-fips` to every resolved image tag — whether the tag comes from a per-service `<service>.image.tag`, `global.tag`, or the chart version — so FIPS selection is orthogonal to version pinning.

`GODEBUG=fips140=on` environment variable is set as the default for the FIPS images' build.

### Enforcing FIPS mode at runtime (`fipsMode=only`)

Set `global.fipsMode=only` to do everything `on` does, plus set `GODEBUG=fips140=on` on every KAI container (scheduler, binder, admission, podgrouper, queue-controller, podgroup-controller, resource-reservation, node-scale-adjuster, numa-placement-exporter, operator, and the crd-upgrader hook):

```sh
helm upgrade --install kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace --set global.fipsMode=only
```

**Using the 'only' mode can cause panic at runtime.** With `fips140=only`, the Go FIPS 140-3 module refuses any non-approved cryptographic algorithm at the call site — as a panic, not a graceful error — per the [`GODEBUG=fips140` option docs](https://go.dev/doc/security/fips140#the-fips140-godebug-option). If any code path in KAI's dependency tree (including transitive TLS/crypto usage) reaches a non-approved algorithm, the affected pod crashes instead of degrading. Using the `only` mode is the user's responsibility: the official go documentation discourages the use of this setting in production. Test it against your cluster's actual configuration make sure this is the actual mode you want to use if you decide to use it in production.

`only` mode also sets `tlsmlkem=0` alongside `fips140=only` on every container. By default, golang's `crypto/tls` FIPS-allowed `X25519MLKEM768` curve preference internally calls the plain X25519 primitive, which is not FIPS compliant and errors under `fips140=only` — breaking every outbound TLS handshake (including any client-go connection to the API server). This can be solved by disabling hybrid curve for TLS exchanges. This is a known upstream gap, not specific to KAI: [golang/go#78298](https://github.com/golang/go/issues/78298) , [kubernetes/kubernetes#133743](https://github.com/kubernetes/kubernetes/issues/133743). This might cause failures if the k8s API server only accepts `X25519MLKEM768`.

## What the FIPS variant is

FIPS images are built with the Go toolchain's native [FIPS 140-3 support](https://go.dev/doc/security/fips140) (`GOFIPS140=v1.0.0`):

- All crypto operations are served by the embedded [Go Cryptographic Module](https://go.dev/doc/security/fips140#the-go-cryptographic-module) v1.0.0, which has been CMVP-validated.
- FIPS 140-3 mode is enabled by default at runtime (`GODEBUG=fips140=on` is the build default), so only approved algorithms are used and the module runs its mandated self-tests at startup.

The regular and FIPS images are otherwise identical: same base image, same binaries' source, same tags scheme.

## Building locally

```sh
make build FIPS=1
```

This compiles all services with `GOFIPS140` and tags the images `<VERSION>-fips`.

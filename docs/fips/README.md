# FIPS-Enabled Images

Every KAI Scheduler release publishes a FIPS-enabled variant of each image alongside the regular one, tagged with a `-fips` suffix:

```
ghcr.io/kai-scheduler/kai-scheduler/scheduler:<version>        # regular
ghcr.io/kai-scheduler/kai-scheduler/scheduler:<version>-fips   # FIPS-enabled
```

## Installing

Set `global.fips=true` to install or upgrade with FIPS-enabled images:

```sh
helm upgrade --install kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace --set global.fips=true
```

The flag appends `-fips` to every resolved image tag — whether the tag comes from a per-service `<service>.image.tag`, `global.tag`, or the chart version — so FIPS selection is orthogonal to version pinning.

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

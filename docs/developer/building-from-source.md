# Building from Source
To build and deploy KAI Scheduler from source, follow these steps:

1. Clone the repository:
   ```sh
   git clone git@github.com:kai-scheduler/KAI-scheduler.git
   cd KAI-scheduler
   ```

2. Build the container images, these images will be built locally (not pushed to a remote registry)
   ```sh
   make build
   ```
   If you want to push the images to a private docker registry, you can set in DOCKER_REPO_BASE var: 
   ```sh
   DOCKER_REPO_BASE=<REGISTRY-URL> make build
   ```

3. Package the Helm chart:
   ```sh
   helm package ./deployments/kai-scheduler -d ./charts
   ```
   
4. Make sure the images are accessible from cluster nodes, either by pushing the images to a private registry or loading them to nodes cache.
   For example, you can load the images to kind cluster with this command:
   ```sh
   for img in $(docker images --format '{{.Repository}}:{{.Tag}}' | grep kai-scheduler); 
      do kind load docker-image $img --name <KIND-CLUSTER-NAME>; done
   ```

5. Install on your cluster:
   ```sh
   helm upgrade -i kai-scheduler -n kai-scheduler --create-namespace ./charts/kai-scheduler-0.0.0.tgz
   ```

## Testing

From the repository root, run `make test` (Helm chart + Go tests in Docker).

### When `make test` fails (e.g. WSL: `docker-credential-desktop.exe` not in PATH)

**Option 1 — Fix Docker config (WSL):**

```sh
cp ~/.docker/config.json ~/.docker/config.json.bak
echo '{}' > ~/.docker/config.json
make test
```

Restore for private registry: `cp ~/.docker/config.json.bak ~/.docker/config.json`. Docker Desktop may restore `credsStore` on restart. See [Docker Desktop WSL](https://docs.docker.com/desktop/wsl/).

**Option 2 — Run without Docker:**

- Unit tests: `go test ./pkg/... ./cmd/... -count=1 -short` (envtest-dependent packages may fail; exit 1 is expected).
- Integration (envtest): use `ENVTEST_K8S_VERSION` from `build/makefile/testenv.mk` (e.g. 1.34.0; do not use `latest`):
  ```sh
  make envtest
  KUBEBUILDER_ASSETS="$(bin/setup-envtest use 1.34.0 -p path --bin-dir bin)" go test ./pkg/... -timeout 30m
  ```
- Validation: `make validate`, `make lint`.

### Version reference

| What | Defined in | Example |
|------|------------|---------|
| envtest K8s | `build/makefile/testenv.mk` → `ENVTEST_K8S_VERSION` | 1.34.0 |
| envtest tool | `build/makefile/testenv.mk` → `ENVTEST_VERSION` | release-0.20 |
| Go / builder | `build/makefile/golang.mk`, `build/builder/Dockerfile` | 1.24.4-bullseye |

Use Makefile/Dockerfile values; do not use `latest`.


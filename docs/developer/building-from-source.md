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
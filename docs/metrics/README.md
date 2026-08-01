# Kube-Prometheus-Stack

Install Prometheus Operator and enable a Prometheus instance. Enable Grafana if
you want to import the dashboards from this directory:

```
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update prometheus-community
helm upgrade -i --create-namespace -n monitoring kube-prometheus-stack prometheus-community/kube-prometheus-stack --values kube-prometheus-values.yaml
```

# Service Monitors for KAI services

Install a Prometheus instance and the relevant ServiceMonitors in the
`kai-scheduler` namespace:

```sh
kubectl apply -f prometheus.yaml
kubectl apply -f service-monitors.yaml
```

To enable the Prometheus instance as a Grafana datasource, apply
`grafana-datasource.yaml`:

```sh
kubectl apply -f grafana-datasource.yaml
```

# Grafana dashboards

The `grafana/` directory contains importable dashboards for scale-test deep
dives:

- `kai-scheduler-internals.json`: scheduler cycle latency, action/plugin
  latency, scenario counters, task scheduling latency, binder latency, and
  preemption/eviction signals.
- `kai-queues-allocation.json`: queue allocation, quota, deserved GPU,
  scheduler fair-share, and UsageDB usage metrics.
- `kai-service-resources.json`: CPU, memory, throttling, restarts, readiness,
  network, and process metrics for KAI pods.
- `kai-controller-runtime-workqueues.json`: controller-runtime reconciliation
  and workqueue latency/backlog metrics.
- `kubernetes-apiserver-scale.json`: Kubernetes API server request rate,
  latency, errors, in-flight requests, API Priority and Fairness, and
  storage/etcd signals during scale tests.

Import the dashboards manually from Grafana, or load them through the Grafana
dashboard sidecar that is enabled in `kube-prometheus-values.yaml`:

```sh
kubectl -n monitoring create configmap kai-grafana-dashboards \
  --from-file=grafana/ \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n monitoring label configmap kai-grafana-dashboards grafana_dashboard=1 --overwrite
```

The Kubernetes API server dashboard requires your Prometheus installation to
scrape kube-apiserver metrics. `kube-prometheus-stack` commonly configures that
outside of KAI ServiceMonitors. If the API server dashboard is empty, verify the
`apiserver_request_total` metric exists in Prometheus first.

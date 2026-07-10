{{/*
Returns a non-empty string when installing on OpenShift: either forced via
.Values.openshift or auto-detected from the ClusterVersion CRD. Autodetection
relies on lookup, which returns nil under offline rendering (helm template,
GitOps tools such as ArgoCD) - set openshift=true explicitly there.
*/}}
{{- define "kai-scheduler.openshift" -}}
{{- if or .Values.openshift (lookup "apiextensions.k8s.io/v1" "CustomResourceDefinition" "" "clusterversions.config.openshift.io") -}}
true
{{- end -}}
{{- end -}}

{{/*
Annotations shared by the post-delete cleanup Job and its ServiceAccount and
RBAC resources. PostDelete requires ArgoCD >= 2.10; without the
argocd.argoproj.io annotations ArgoCD treats Helm hook resources as regular
sync-phase resources.
*/}}
{{- define "kai-scheduler.post-delete-hook-annotations" -}}
"helm.sh/hook": post-delete
"helm.sh/hook-delete-policy": hook-succeeded
argocd.argoproj.io/hook: PostDelete
argocd.argoproj.io/hook-delete-policy: BeforeHookCreation,HookSucceeded
{{- end -}}

{{/*
Resolves a component image tag: explicit tag, then global.tag, then the chart
version. When global.fips is set, appends "-fips" to whatever tag resolves so
the FIPS image variants are used. Usage:
  {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.<svc>.image.tag) }}
*/}}
{{- define "kai-scheduler.imageTag" -}}
{{- $tag := .tag | default .root.Values.global.tag | default .root.Chart.AppVersion -}}
{{- if .root.Values.global.fips -}}{{- $tag = printf "%s-fips" $tag -}}{{- end -}}
{{- $tag -}}
{{- end -}}

{{/*
Renders the kai-config Config CR. Used by the kai-config-deployer hook
ConfigMap so the operator's input config can be applied via kubectl
out-of-band of the Helm release, and rendered inline as a release resource
when kaiConfig.render=true (GitOps/ArgoCD installs, see docs/gitops/README.md).
See deployments/kai-scheduler/templates/hooks/post/kai-config-deployer/.
*/}}
{{- define "kai-scheduler.kai-config" -}}
apiVersion: kai.scheduler/v1
kind: Config
metadata:
  name: kai-config
  {{- if (.Values.kaiConfig | default dict).render }}
  annotations:
    # SkipDryRunOnMissingResource: ArgoCD dry-runs sync-phase resources before
    # any hook runs; on a fresh cluster the Config CRD is not established yet.
    # ServerSideApply: avoids client-side last-applied size limits on the large
    # CR and adopts a CR previously field-managed by kai-config-deployer.
    argocd.argoproj.io/sync-options: SkipDryRunOnMissingResource=true,ServerSideApply=true
  {{- end }}
spec:
  namespace: {{ .Release.Namespace }}
  global:
    {{- if .Values.global.namespaceLabelSelector }}
    namespaceLabelSelector:
      {{- toYaml .Values.global.namespaceLabelSelector | nindent 6 }}
    {{- end }}
    {{- if .Values.global.podLabelSelector }}
    podLabelSelector:
      {{- toYaml .Values.global.podLabelSelector | nindent 6 }}
    {{- end }}
    {{- if .Values.global.nodePoolLabelKey }}
    nodePoolLabelKey: {{ .Values.global.nodePoolLabelKey | quote }}
    {{- end }}
    {{- if .Values.global.jsonLog }}
    jsonLog: true
    {{- end }}
    {{- if .Values.global.affinity }}
    affinity:
      {{- toYaml .Values.global.affinity | nindent 6 }}
    {{- end }}
    {{- if .Values.global.requireDefaultPodAntiAffinityTerm }}
    requireDefaultPodAntiAffinityTerm: true
    {{- end }}
    {{- if .Values.global.nodeSelector }}
    nodeSelector:
      {{- toYaml .Values.global.nodeSelector | nindent 6 }}
    {{- end }}
    {{- if .Values.global.tolerations }}
    tolerations:
      {{- toYaml .Values.global.tolerations | nindent 6 }}
    {{- end }}
    {{- if .Values.global.securityContext }}
    securityContext:
      {{- toYaml .Values.global.securityContext | nindent 6 }}
    {{- end }}
    {{- if .Values.global.imagePullSecrets }}
    additionalImagePullSecrets:
      {{- range .Values.global.imagePullSecrets }}
      - {{ .name }}
      {{- end }}
    {{- end }}
    replicaCount: {{ .Values.operator.replicaCount | default 1 }}
    {{- if .Values.global.vpa }}
    vpa:
      {{- toYaml .Values.global.vpa | nindent 6 }}
    {{- end }}
    {{- if .Values.podgrouper.queueLabelKey }}
    queueLabelKey: {{ .Values.podgrouper.queueLabelKey | quote }}
    {{- end }}

  binder:
    service:
      enabled: {{ .Values.binder.enabled }}
      image:
        name: {{ .Values.binder.image.name }}
        repository: {{ .Values.global.registry }}
        tag: {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.binder.image.tag) }}
        pullPolicy: {{ .Values.binder.image.pullPolicy | default .Values.global.imagePullPolicy }}
      {{- if .Values.binder.resources }}
      resources:
        {{- toYaml .Values.binder.resources | nindent 8 }}
      {{- end }}
      {{- if .Values.binder.affinity }}
      affinity:
        {{- toYaml .Values.binder.affinity | nindent 8 }}
      {{- end }}
    metricsPort: {{ .Values.binder.ports.metricsPort }}
    resourceReservation:
      {{- if .Values.global.resourceReservation.namespace }}
      namespace: {{ .Values.global.resourceReservation.namespace }}
      {{- end }}
      {{- if .Values.global.resourceReservation.serviceAccount }}
      serviceAccountName: {{ .Values.global.resourceReservation.serviceAccount }}
      {{- end }}
      {{- if .Values.global.resourceReservation.appLabel }}
      appLabel: {{ .Values.global.resourceReservation.appLabel }}
      {{- end }}
      {{- if .Values.binder.runtimeClassName }}
      runtimeClassName: {{ .Values.binder.runtimeClassName }}
      {{- end }}
      image:
        name: {{ .Values.binder.resourceReservationImage.name }}
        repository: {{ .Values.global.registry }}
        tag: {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.binder.resourceReservationImage.tag) }}
        pullPolicy: {{ .Values.binder.resourceReservationImage.pullPolicy | default .Values.global.imagePullPolicy }}
      {{- if .Values.binder.resourceReservationPodResources }}
      podResources:
        {{- toYaml .Values.binder.resourceReservationPodResources | nindent 8 }}
      {{- end }}
    {{- if hasKey .Values.binder "cdiEnabled" }}
    cdiEnabled: {{ .Values.binder.cdiEnabled }}
    {{- end }}
    {{- if .Values.binder.plugins }}
    plugins:
      {{- toYaml .Values.binder.plugins | nindent 6 }}
    {{- end }}

  podGrouper:
    service:
      enabled: {{ .Values.podgrouper.enabled }}
      image:
        name: {{ .Values.podgrouper.image.name }}
        repository: {{ .Values.global.registry }}
        tag: {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.podgrouper.image.tag) }}
        pullPolicy: {{ .Values.podgrouper.image.pullPolicy | default .Values.global.imagePullPolicy }}
      {{- if .Values.podgrouper.resources }}
      resources:
        {{- toYaml .Values.podgrouper.resources | nindent 8 }}
      {{- end }}
      {{- if .Values.podgrouper.affinity }}
      affinity:
        {{- toYaml .Values.podgrouper.affinity | nindent 8 }}
      {{- end }}

  podGroupController:
    service:
      enabled: {{ .Values.podgroupcontroller.enabled }}
      image:
        name: {{ .Values.podgroupcontroller.image.name }}
        repository: {{ .Values.global.registry }}
        tag: {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.podgroupcontroller.image.tag) }}
        pullPolicy: {{ .Values.podgroupcontroller.image.pullPolicy | default .Values.global.imagePullPolicy }}
      {{- if .Values.podgroupcontroller.resources }}
      resources:
        {{- toYaml .Values.podgroupcontroller.resources | nindent 8 }}
      {{- end }}
      {{- if .Values.podgroupcontroller.affinity }}
      affinity:
        {{- toYaml .Values.podgroupcontroller.affinity | nindent 8 }}
      {{- end }}

  queueController:
    service:
      enabled: {{ .Values.queuecontroller.enabled }}
      image:
        name: {{ .Values.queuecontroller.image.name }}
        repository: {{ .Values.global.registry }}
        tag: {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.queuecontroller.image.tag) }}
        pullPolicy: {{ .Values.queuecontroller.image.pullPolicy | default .Values.global.imagePullPolicy }}
      {{- if .Values.queuecontroller.resources }}
      resources:
        {{- toYaml .Values.queuecontroller.resources | nindent 8 }}
      {{- end }}
      {{- if .Values.queuecontroller.affinity }}
      affinity:
        {{- toYaml .Values.queuecontroller.affinity | nindent 8 }}
      {{- end }}

  admission:
    service:
      enabled: {{ .Values.admission.enabled }}
      image:
        name: {{ .Values.admission.image.name }}
        repository: {{ .Values.global.registry }}
        tag: {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.admission.image.tag) }}
        pullPolicy: {{ .Values.admission.image.pullPolicy | default .Values.global.imagePullPolicy }}
      {{- if .Values.admission.resources }}
      resources:
        {{- toYaml .Values.admission.resources | nindent 8 }}
      {{- end }}
      {{- if .Values.admission.affinity }}
      affinity:
        {{- toYaml .Values.admission.affinity | nindent 8 }}
      {{- end }}
      podDisruptionBudget:
        {{- if hasKey .Values.admission.podDisruptionBudget "enabled" }}
        enabled: {{ .Values.admission.podDisruptionBudget.enabled }}
        {{- end }}
        {{- if hasKey .Values.admission.podDisruptionBudget "maxUnavailable" }}
        maxUnavailable: {{ .Values.admission.podDisruptionBudget.maxUnavailable }}
        {{- end }}
    gpuSharing: {{ .Values.global.gpuSharing | default false }}
    blockNvidiaVisibleDevices: {{ .Values.global.blockNvidiaVisibleDevices | default false }}
    queueLabelSelector: false
    webhook:
      port: 443
      targetPort: {{ .Values.admission.ports.webhookPort | default 9443 }}
      probePort: {{ .Values.admission.ports.probePort | default 8081 }}
      metricsPort: {{ .Values.admission.ports.metricsPort | default 8080 }}
    {{- if hasKey .Values.admission "gpuFractionRuntimeClassName" }}
    gpuFractionRuntimeClassName: {{ .Values.admission.gpuFractionRuntimeClassName | quote }}
    {{- else if hasKey .Values.admission "gpuPodRuntimeClassName" }}
    gpuPodRuntimeClassName: {{ .Values.admission.gpuPodRuntimeClassName | quote }}
    {{- end }}

  nodeScaleAdjuster:
    service:
      enabled: {{ .Values.global.clusterAutoscaling }}
      image:
        name: {{ .Values.nodescaleadjuster.image.name }}
        repository: {{ .Values.global.registry }}
        tag: {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.nodescaleadjuster.image.tag) }}
        pullPolicy: {{ .Values.nodescaleadjuster.image.pullPolicy | default .Values.global.imagePullPolicy }}
      {{- if .Values.nodescaleadjuster.resources }}
      resources:
        {{- toYaml .Values.nodescaleadjuster.resources | nindent 8 }}
      {{- end }}
      {{- if .Values.nodescaleadjuster.affinity }}
      affinity:
        {{- toYaml .Values.nodescaleadjuster.affinity | nindent 8 }}
      {{- end }}
    args:
      nodeScaleNamespace: {{ .Values.nodescaleadjuster.scalingPodNamespace }}
      scalingPodImage:
        name: {{ .Values.nodescaleadjuster.scalingPodImage.name }}
        repository: {{ .Values.global.registry }}
        tag: {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.nodescaleadjuster.scalingPodImage.tag) }}
        pullPolicy: {{ .Values.nodescaleadjuster.scalingPodImage.pullPolicy | default .Values.global.imagePullPolicy }}

  scheduler:
    service:
      enabled: {{ .Values.scheduler.enabled }}
      image:
        name: {{ .Values.scheduler.image.name }}
        repository: {{ .Values.global.registry }}
        tag: {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.scheduler.image.tag) }}
        pullPolicy: {{ .Values.scheduler.image.pullPolicy | default .Values.global.imagePullPolicy }}
      {{- if .Values.scheduler.resources }}
      resources:
        {{- toYaml .Values.scheduler.resources | nindent 8 }}
      {{- end }}
      {{- if .Values.scheduler.affinity }}
      affinity:
        {{- toYaml .Values.scheduler.affinity | nindent 8 }}
      {{- end }}
    {{- if and .Values.scheduler.ports .Values.scheduler.ports.metricsPort }}
    schedulerService:
      port: {{ .Values.scheduler.ports.metricsPort }}
    {{- end }}

  {{- if .Values.prometheus.enabled }}
  prometheus:
    enabled: true
    {{- if .Values.prometheus.externalPrometheusUrl }}
    externalPrometheusUrl: {{ .Values.prometheus.externalPrometheusUrl | quote }}
    {{- end }}
  {{- end }}

  numaPlacementExporter:
    service:
      {{- if not (kindIs "invalid" .Values.numaPlacementExporter.enabled) }}
      enabled: {{ .Values.numaPlacementExporter.enabled }}
      {{- end }}
      image:
        name: {{ .Values.numaPlacementExporter.image.name }}
        repository: {{ .Values.global.registry }}
        tag: {{ include "kai-scheduler.imageTag" (dict "root" $ "tag" .Values.numaPlacementExporter.image.tag) }}
        pullPolicy: {{ .Values.numaPlacementExporter.image.pullPolicy | default .Values.global.imagePullPolicy }}
      {{- if .Values.numaPlacementExporter.resources }}
      resources:
        {{- toYaml .Values.numaPlacementExporter.resources | nindent 8 }}
      {{- end }}
      {{- if .Values.numaPlacementExporter.affinity }}
      affinity:
        {{- toYaml .Values.numaPlacementExporter.affinity | nindent 8 }}
      {{- end }}
    {{- if .Values.numaPlacementExporter.nodeSelector }}
    nodeSelector:
      {{- toYaml .Values.numaPlacementExporter.nodeSelector | nindent 6 }}
    {{- end }}
    {{- if .Values.numaPlacementExporter.tolerations }}
    tolerations:
      {{- toYaml .Values.numaPlacementExporter.tolerations | nindent 6 }}
    {{- end }}
    {{- if .Values.numaPlacementExporter.pollInterval }}
    pollInterval: {{ .Values.numaPlacementExporter.pollInterval | quote }}
    {{- end }}
    {{- if .Values.numaPlacementExporter.driftResyncInterval }}
    driftResyncInterval: {{ .Values.numaPlacementExporter.driftResyncInterval | quote }}
    {{- end }}
    {{- if .Values.numaPlacementExporter.podResourcesHostPath }}
    podResourcesHostPath: {{ .Values.numaPlacementExporter.podResourcesHostPath | quote }}
    {{- end }}
    {{- if .Values.numaPlacementExporter.podResourcesSocket }}
    podResourcesSocket: {{ .Values.numaPlacementExporter.podResourcesSocket | quote }}
    {{- end }}
    {{- if .Values.numaPlacementExporter.sysfsHostPath }}
    sysfsHostPath: {{ .Values.numaPlacementExporter.sysfsHostPath | quote }}
    {{- end }}
{{- end -}}

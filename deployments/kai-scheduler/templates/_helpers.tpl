# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

{{/*
Operator PodDisruptionBudget: merge values.operator.podDisruptionBudget with safe defaults.
Uses hasKey (not default) so enabled: false and maxUnavailable: 0 are respected (Sprig default() treats them as empty).

Returns a small YAML object with keys: enabled, maxUnavailable
*/}}
{{- define "kai-scheduler.operator.podDisruptionBudgetConfig" -}}
{{- $pdb := .Values.operator.podDisruptionBudget | default dict }}
{{- $pdbEnabled := true }}
{{- if hasKey $pdb "enabled" }}
{{-   $pdbEnabled = $pdb.enabled }}
{{- end }}
{{- $maxUnavailable := 1 }}
{{- if hasKey $pdb "maxUnavailable" }}
{{-   $maxUnavailable = int $pdb.maxUnavailable }}
{{- end }}
{{- dict "enabled" $pdbEnabled "maxUnavailable" $maxUnavailable | toYaml }}
{{- end }}

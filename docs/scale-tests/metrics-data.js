// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

'use strict';

(function exposeMetricsData(root, factory) {
  const api = factory();
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  root.ScaleTestMetrics = api;
})(typeof globalThis !== 'undefined' ? globalThis : window, function createMetricsDataApi() {
  const CHART_CONFIGS = [
    { id: 'fill-cluster', title: 'Fill Cluster with Single-GPU Jobs', label: 'Duration' },
    { id: 'fill-cluster-pending', title: 'Fill Cluster with Pending Tasks', label: 'Duration' },
    { id: 'topology-preferred', title: 'Allocate with Preferred Topology', label: 'Duration' },
    { id: 'topology-none', title: 'Allocate without Preferred Topology', label: 'Duration' },
    { id: 'unschedulable-delay', title: 'Distributed Job Unschedulable Delay', label: 'Duration' },
    { id: 'reclaim-large-job', title: 'Reclaim Time for One Very Large Job', label: 'Duration' },
    { id: 'reclaim-single-gpu', title: 'Single-GPU Reclaim Latency', label: 'Duration' },
    { id: 'reclaim-all-single-gpu', title: 'Reclaim All Single-GPU Jobs', label: 'Duration' },
    { id: 'reclaim-distributed', title: 'Multi-Node Distributed Reclaim', label: 'Duration' },
    { id: 'consolidation', title: 'Consolidation for Distributed Jobs', label: 'Duration' },
    { id: 'nccl-empty-cluster', title: 'NCCL Simulation on Empty Cluster', label: 'Duration' },
  ];

  const TEST_CASES = [
    {
      id: 'fill-cluster-disabled', chartId: 'fill-cluster', timingField: 'time',
      scale: name => name === 'Fill Cluster with single GPU Jobs',
      legacy: (name, hierarchy) => /^fill cluster with single gpu jobs$/i.test(name)
        && hierarchy.some(value => /scheduler disabled/i.test(value)),
    },
    {
      id: 'fill-cluster-running', chartId: 'fill-cluster', timingField: 'time',
      scale: name => name === 'Fill Cluster with single GPU Jobs - scheduler is running while submitting jobs',
      legacy: (name, hierarchy) => /^fill cluster with single gpu jobs$/i.test(name)
        && hierarchy.some(value => /all services are running/i.test(value)),
    },
    {
      id: 'fill-cluster-pending', chartId: 'fill-cluster-pending', timingField: 'time',
      scale: name => /^Fill Cluster with single GPU Jobs with \d+ pending tasks in background$/i.test(name),
      legacy: name => /schedules jobs with pending tasks in background/i.test(name),
    },
    {
      id: 'topology-preferred', chartId: 'topology-preferred', timingField: 'time',
      scale: name => name === 'Allocate with preferred topology',
      legacy: name => /Allocate single distributed job with preferred topology/i.test(name),
    },
    {
      id: 'topology-none', chartId: 'topology-none', timingField: 'time',
      scale: name => name === 'Allocate without preferred topology',
      legacy: name => /Allocate single distributed job without preferred topology/i.test(name),
    },
    {
      id: 'unschedulable-delay', chartId: 'unschedulable-delay', timingField: 'average time to unschedulable (seconds)',
      scale: name => name === 'Average time to unschedulable for distributed job',
      legacy: name => /measure time for reclaim to fail on distributed job last pod/i.test(name),
    },
    {
      id: 'reclaim-large-job', chartId: 'reclaim-large-job', timingField: 'time to reclaim (seconds)',
      scale: name => name === 'Reclaim time for one very large job',
      legacy: name => /reclaims for one very large job/i.test(name),
    },
    {
      id: 'reclaim-single-gpu', chartId: 'reclaim-single-gpu', timingField: 'average time to reclaim single GPU (seconds)',
      scale: name => name === 'Measuring reclaim time for single GPU',
      legacy: name => /reclaims single GPU Jobs at intervals to measure latency/i.test(name),
    },
    {
      id: 'reclaim-all-single-gpu', chartId: 'reclaim-all-single-gpu', timingField: 'time',
      scale: name => /^Reclaim \d+ single GPU Jobs$/i.test(name),
      legacy: name => /reclaims many single GPU Jobs/i.test(name),
    },
    {
      id: 'reclaim-distributed', chartId: 'reclaim-distributed', timingField: 'time',
      scale: name => name === 'Multi Node Reclaim for distributed jobs',
      legacy: name => /reclaims with distributed jobs/i.test(name),
    },
    {
      id: 'consolidation', chartId: 'consolidation', timingField: 'time',
      scale: name => name === 'Consolidation to run multiple distributed jobs',
      legacy: name => /consolidates for many large jobs/i.test(name),
    },
    {
      id: 'nccl-empty-cluster', chartId: 'nccl-empty-cluster', timingField: 'time(seconds)',
      ignoredSeriesFields: ['completed pods'],
      scale: name => name === 'NCCL Simulation on empty cluster',
      legacy: name => /Runs NCCL Simulation on empty cluster/i.test(name),
    },
  ];

  function resolveTestCase(source, name, hierarchy = []) {
    const matcher = source === 'scale-results' ? 'scale' : 'legacy';
    return TEST_CASES.find(testCase => testCase[matcher](name || '', hierarchy || [])) || null;
  }

  function findMetrics(reportEntries) {
    if (!Array.isArray(reportEntries)) return null;
    const entry = reportEntries.find(candidate => candidate.Name === 'Test Metrics');
    if (!entry?.Value?.AsJSON) return null;
    try {
      return JSON.parse(entry.Value.AsJSON);
    } catch (error) {
      console.warn('[scale-tests] Failed to parse legacy Test Metrics entry:', error);
      return null;
    }
  }

  function parseMetricsFromOutput(output) {
    if (!output) return null;
    const metrics = {};
    const aliases = {
      'total time': 'time',
      nodes: 'nodes',
      jobs: 'jobs',
      pods: 'pods',
      'distributed-jobs': 'distributed jobs',
      'distributed_jobs': 'distributed jobs',
      completedpods: 'completed pods',
      pendingpods: 'pending pods',
      'len(testpods)': 'total pods',
    };
    const pattern = /(?:"([^"]+)"|([A-Za-z][A-Za-z0-9_-]*))=(?:"([^"]*)"|([^\s]+))/g;
    let match;
    while ((match = pattern.exec(output)) !== null) {
      const rawKey = (match[1] || match[2]).toLowerCase();
      const key = aliases[rawKey];
      if (!key) continue;
      const raw = match[3] ?? match[4];
      const number = Number(raw);
      metrics[key] = Number.isNaN(number) ? raw : number;
    }
    return Object.keys(metrics).length ? metrics : null;
  }

  function parseDuration(value) {
    if (typeof value === 'number') return Number.isFinite(value) ? value : null;
    if (typeof value !== 'string' || !value.trim()) return null;
    const match = value.trim().match(/^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?$/);
    if (!match || (!match[1] && !match[2] && !match[3])) return null;
    return Number(match[1] || 0) * 3600 + Number(match[2] || 0) * 60 + Number(match[3] || 0);
  }

  function formatDuration(value) {
    const numericValue = Number(value);
    if (!Number.isFinite(numericValue)) return String(value);

    const sign = numericValue < 0 ? '-' : '';
    let remainder = Math.round(Math.abs(numericValue) * 1000) / 1000;
    const hours = Math.floor(remainder / 3600);
    remainder -= hours * 3600;
    const minutes = Math.floor(remainder / 60);
    const seconds = Number((remainder - minutes * 60).toFixed(3));
    const parts = [];

    if (hours) parts.push(`${hours}h`);
    if (minutes) parts.push(`${minutes}m`);
    if (seconds || parts.length === 0) parts.push(`${seconds}s`);
    return `${sign}${parts.join(' ')}`;
  }

  function stableValue(value) {
    if (Array.isArray(value)) return value.map(stableValue);
    if (value && typeof value === 'object') {
      return Object.fromEntries(Object.keys(value).sort().map(key => [key, stableValue(value[key])]));
    }
    return value;
  }

  function seriesDetails(details, timingField, ignoredFields = []) {
    const excludedFields = new Set([timingField, ...ignoredFields]);
    return Object.fromEntries(
      Object.entries(details || {})
        .filter(([key]) => !excludedFields.has(key))
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, value]) => [key, stableValue(value)]),
    );
  }

  function buildSeriesIdentity({ testId, timingField, details, metadata }) {
    const testCase = TEST_CASES.find(candidate => candidate.id === testId);
    return {
      testId,
      details: seriesDetails(details, timingField, testCase?.ignoredSeriesFields),
      metadata: stableValue(metadata || {}),
    };
  }

  function buildSeriesKey({ testId, timingField, details, metadata }) {
    return JSON.stringify(buildSeriesIdentity({ testId, timingField, details, metadata }));
  }

  function isDeepSubset(subset, superset) {
    if (Object.is(subset, superset)) return true;
    if (Array.isArray(subset) || Array.isArray(superset)) {
      return Array.isArray(subset)
        && Array.isArray(superset)
        && subset.length === superset.length
        && subset.every((value, index) => isDeepSubset(value, superset[index]));
    }
    if (!subset || typeof subset !== 'object' || !superset || typeof superset !== 'object') return false;
    return Object.keys(subset).every(key => Object.hasOwn(superset, key)
      && isDeepSubset(subset[key], superset[key]));
  }

  function groupCompatibleObservations(observations) {
    const groups = [];
    const ordered = [...observations].sort(
      (left, right) => new Date(left.timestamp) - new Date(right.timestamp),
    );

    ordered.forEach(observation => {
      const compatibleGroups = groups.filter(group => isDeepSubset(group.identity, observation.seriesIdentity)
        || isDeepSubset(observation.seriesIdentity, group.identity));
      const group = compatibleGroups.sort((left, right) => right.lastTimestamp - left.lastTimestamp)[0];
      const timestamp = new Date(observation.timestamp).getTime();

      if (!group) {
        groups.push({
          identity: observation.seriesIdentity,
          observations: [observation],
          seriesLabel: observation.seriesLabel,
          lastTimestamp: timestamp,
        });
        return;
      }

      if (isDeepSubset(group.identity, observation.seriesIdentity)) {
        group.identity = observation.seriesIdentity;
        group.seriesLabel = observation.seriesLabel;
      }
      group.observations.push(observation);
      group.lastTimestamp = timestamp;
    });

    return groups;
  }

  function formatDetailValue(value) {
    if (value !== null && typeof value === 'object') return JSON.stringify(stableValue(value));
    return String(value);
  }

  function shortHash(value) {
    let hash = 2166136261;
    for (const character of value) {
      hash ^= character.charCodeAt(0);
      hash = Math.imul(hash, 16777619);
    }
    return (hash >>> 0).toString(16).padStart(8, '0').slice(0, 6);
  }

  function metadataLabel(metadata) {
    if (!metadata || Object.keys(metadata).length === 0) return 'config=none';
    const stableMetadata = JSON.stringify(stableValue(metadata));
    const ref = metadata.kai_scheduler_ref ? `ref=${metadata.kai_scheduler_ref} · ` : '';
    return `${ref}config=${shortHash(stableMetadata)}`;
  }

  function buildSeriesLabel(testCase, details, metadata) {
    const dimensions = seriesDetails(details, testCase.timingField, testCase.ignoredSeriesFields);
    const suffix = Object.entries(dimensions).map(([key, value]) => `${key}=${formatDetailValue(value)}`).join(' · ');
    const detailsLabel = suffix ? `${testCase.id} · ${suffix}` : testCase.id;
    return `${detailsLabel} · ${metadataLabel(metadata)}`;
  }

  function buildTooltipLines(observation) {
    const lines = [
      `test: ${observation.testName}`,
      `source: ${observation.source === 'scale-results' ? 'results' : 'legacy Ginkgo'}`,
    ];
    if (observation.commit) lines.push(`commit: ${observation.commit}`);
    Object.keys(observation.details || {}).sort().forEach(key => {
      lines.push(`${key}: ${formatDetailValue(observation.details[key])}`);
    });
    Object.keys(observation.metadata || {}).sort().forEach(key => {
      lines.push(`${key}: ${formatDetailValue(observation.metadata[key])}`);
    });
    return lines;
  }

  function createObservation(run, testCase, testName, status, details) {
    if (!testCase || !details || typeof details !== 'object') return null;
    const rawTiming = details[testCase.timingField];
    const timingSeconds = parseDuration(rawTiming);
    if (timingSeconds === null) return null;
    const seriesIdentity = buildSeriesIdentity({
      testId: testCase.id,
      timingField: testCase.timingField,
      details,
      metadata: run.metadata,
    });
    return {
      chartId: testCase.chartId,
      testId: testCase.id,
      testName,
      status,
      timestamp: run.meta.timestamp,
      timingSeconds,
      timingField: testCase.timingField,
      rawTiming,
      details,
      metadata: run.metadata,
      commit: run.meta.commit,
      source: run.kind,
      seriesIdentity,
      seriesKey: JSON.stringify(seriesIdentity),
      seriesLabel: buildSeriesLabel(testCase, details, run.metadata),
    };
  }

  function scaleObservations(run) {
    return run.result.tests.flatMap(result => {
      if (result?.status !== 'success') return [];
      const testCase = resolveTestCase('scale-results', result.test_name);
      const observation = createObservation(run, testCase, result.test_name, result.status, result.details);
      return observation ? [observation] : [];
    });
  }

  function ginkgoObservations(run) {
    const suite = (Array.isArray(run.report) ? run.report[0] : run.report) || {};
    return (suite.SpecReports || []).flatMap(spec => {
      if (spec.State !== 'passed') return [];
      const testCase = resolveTestCase('ginkgo-report', spec.LeafNodeText, spec.ContainerHierarchyTexts);
      const details = findMetrics(spec.ReportEntries) || parseMetricsFromOutput(spec.CapturedGinkgoWriterOutput);
      const observation = createObservation(run, testCase, spec.LeafNodeText, spec.State, details);
      return observation ? [observation] : [];
    });
  }

  function extractChartObservations(runs) {
    return runs
      .flatMap(run => run.kind === 'scale-results' ? scaleObservations(run) : ginkgoObservations(run))
      .sort((left, right) => new Date(left.timestamp) - new Date(right.timestamp));
  }

  function findMigrationTimestamp(runs) {
    const hasLegacy = runs.some(run => run.kind === 'ginkgo-report');
    const current = runs.filter(run => run.kind === 'scale-results').sort(
      (left, right) => new Date(left.meta.timestamp) - new Date(right.meta.timestamp),
    );
    return hasLegacy && current.length ? current[0].meta.timestamp : null;
  }

  return {
    CHART_CONFIGS,
    TEST_CASES,
    buildSeriesKey,
    buildTooltipLines,
    extractChartObservations,
    findMetrics,
    findMigrationTimestamp,
    formatDuration,
    groupCompatibleObservations,
    parseDuration,
    parseMetricsFromOutput,
    resolveTestCase,
  };
});

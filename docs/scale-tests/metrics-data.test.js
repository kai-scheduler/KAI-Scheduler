// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const test = require('node:test');

const { loadRun } = require('./results.js');
const {
  CHART_CONFIGS,
  buildSeriesKey,
  buildTooltipLines,
  extractChartObservations,
  findMigrationTimestamp,
  formatDuration,
  groupCompatibleObservations,
  parseDuration,
  parseMetricsFromOutput,
  resolveTestCase,
} = require('./metrics-data.js');

const payload = JSON.parse(readFileSync(join(__dirname, 'example-results.json'), 'utf8'));
const results = payload.results;
const meta = { timestamp: '2026-06-29T08:42:33Z', path: 'Public/results.json', commit: 'abc123' };

test('maps every current result into the eleven configured charts', () => {
  const run = loadRun(payload, meta);
  const observations = extractChartObservations([run]);

  assert.equal(CHART_CONFIGS.length, 11);
  assert.equal(observations.length, 13);
  assert.equal(new Set(observations.map(point => point.chartId)).size, 11);
  assert.ok(results.tests.every(result => resolveTestCase('scale-results', result.test_name)));
});

test('preserves complete details and raw timing values in observations', () => {
  const run = loadRun(payload, meta);
  const observation = extractChartObservations([run]).find(point => point.testName === 'NCCL Simulation on empty cluster');
  const expectedDetails = results.tests.at(-1).details;

  assert.deepEqual(observation.details, expectedDetails);
  assert.equal(observation.rawTiming, expectedDetails['time(seconds)']);
  assert.equal(observation.timingSeconds, expectedDetails['time(seconds)']);
  assert.equal(observation.commit, 'abc123');
  const tooltip = buildTooltipLines(observation);
  assert.ok(tooltip.includes('test: NCCL Simulation on empty cluster'));
  assert.ok(tooltip.includes(`time(seconds): ${expectedDetails['time(seconds)']}`));
  assert.ok(tooltip.includes(`total pods: ${expectedDetails['total pods']}`));
});

test('preserves wrapped run metadata in observations and tooltips', () => {
  const run = loadRun(payload, meta);
  const observations = extractChartObservations([run]);
  const observation = observations[0];

  assert.ok(observations.every(point => point.metadata === run.metadata));
  assert.ok(buildTooltipLines(observation).includes('kai_scheduler_ref: main'));
  assert.ok(buildTooltipLines(observation).includes('kwok_node_count: 500'));
  assert.ok(!buildTooltipLines(observation).some(line => line.startsWith('metadata.')));
  assert.ok(!buildTooltipLines(observation).some(line => line.startsWith('kai_commit_hash:')));
  assert.ok(!buildTooltipLines(observation).some(line => line.startsWith('test_focus:')));
  assert.ok(!buildTooltipLines(observation).some(line => line.startsWith('timestamp:')));
});

test('series identity ignores timing and source but includes details and metadata', () => {
  const base = {
    testId: 'nccl-empty-cluster',
    timingField: 'time(seconds)',
    details: { nodes: 500, 'completed pods': 100, 'pending pods': 0, 'time(seconds)': 10 },
    metadata: { kai_scheduler_ref: 'main', kwok_node_count: 500 },
  };

  assert.equal(
    buildSeriesKey({ ...base, source: 'scale-results' }),
    buildSeriesKey({ ...base, source: 'ginkgo-report', details: { ...base.details, 'time(seconds)': 20 } }),
  );
  assert.notEqual(
    buildSeriesKey(base),
    buildSeriesKey({ ...base, details: { ...base.details, 'pending pods': 1 } }),
  );
  assert.notEqual(
    buildSeriesKey(base),
    buildSeriesKey({ ...base, metadata: { ...base.metadata, kai_scheduler_ref: 'v0.16.0' } }),
  );
  assert.equal(
    buildSeriesKey(base),
    buildSeriesKey({
      ...base,
      details: { 'time(seconds)': 10, 'pending pods': 0, 'completed pods': 100, nodes: 500 },
      metadata: { kwok_node_count: 500, kai_scheduler_ref: 'main' },
    }),
  );
});

test('NCCL series identity ignores completed pods but retains the result in point details', () => {
  const earlierPayload = structuredClone(payload);
  const laterPayload = structuredClone(payload);
  const earlierResult = earlierPayload.results.tests.find(result => result.test_name === 'NCCL Simulation on empty cluster');
  const laterResult = laterPayload.results.tests.find(result => result.test_name === 'NCCL Simulation on empty cluster');
  laterResult.details['completed pods'] = earlierResult.details['completed pods'] + 37;
  laterResult.details['time(seconds)'] = earlierResult.details['time(seconds)'] + 1;

  const earlierRun = loadRun(earlierPayload, { ...meta, timestamp: '2026-07-03T08:39:44Z' });
  const laterRun = loadRun(laterPayload, { ...meta, timestamp: '2026-07-04T08:39:44Z' });
  const observations = extractChartObservations([earlierRun, laterRun])
    .filter(point => point.testId === 'nccl-empty-cluster');
  const groups = groupCompatibleObservations(observations);

  assert.equal(observations.length, 2);
  assert.equal(observations[0].seriesKey, observations[1].seriesKey);
  assert.equal(groups.length, 1);
  assert.ok(!groups[0].seriesLabel.includes('completed pods'));
  assert.equal(observations[1].details['completed pods'], laterResult.details['completed pods']);
  assert.ok(buildTooltipLines(observations[1]).includes(`completed pods: ${laterResult.details['completed pods']}`));
});

test('groups pending-task results when newer metadata is a compatible superset', () => {
  const currentRun = loadRun(payload, { ...meta, timestamp: '2026-07-03T08:39:44Z' });
  const earlierRun = loadRun(results, { ...meta, timestamp: '2026-07-01T08:38:52Z' });
  const observations = extractChartObservations([earlierRun, currentRun])
    .filter(point => point.testId === 'fill-cluster-pending');

  assert.equal(observations.length, 2);
  assert.notEqual(observations[0].seriesKey, observations[1].seriesKey);
  const groups = groupCompatibleObservations(observations);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].observations.length, 2);
  assert.match(groups[0].seriesLabel, /ref=main/);
});

test('splits a future version after a sparse point enriches an existing line', () => {
  const earlierRun = loadRun(results, { ...meta, timestamp: '2026-07-01T08:38:52Z' });
  const currentRun = loadRun(payload, { ...meta, timestamp: '2026-07-03T08:39:44Z' });
  const futurePayload = structuredClone(payload);
  futurePayload.metadata.kai_scheduler_ref = 'v0.16.0';
  const futureRun = loadRun(futurePayload, { ...meta, timestamp: '2026-07-04T08:39:44Z' });
  const observations = extractChartObservations([earlierRun, currentRun, futureRun])
    .filter(point => point.testId === 'fill-cluster-pending');

  const groups = groupCompatibleObservations(observations);
  assert.equal(groups.length, 2);
  assert.deepEqual(groups.map(group => group.observations.length), [2, 1]);
});

test('parses every supported timing representation', () => {
  assert.equal(parseDuration('1h5m30.5s'), 3930.5);
  assert.equal(parseDuration('6m36.5s'), 396.5);
  assert.equal(parseDuration('48.25s'), 48.25);
  assert.equal(parseDuration(8.34), 8.34);
  assert.equal(parseDuration('1..2s'), null);
});

test('formats chart durations with compact seconds, minutes, and hours', () => {
  assert.equal(formatDuration(0), '0s');
  assert.equal(formatDuration(45.234), '45.234s');
  assert.equal(formatDuration(60), '1m');
  assert.equal(formatDuration(396.5), '6m 36.5s');
  assert.equal(formatDuration(3600), '1h');
  assert.equal(formatDuration(3930.5), '1h 5m 30.5s');
});

test('marks the first scale-results run only when both formats are loaded', () => {
  const legacy = { kind: 'ginkgo-report', meta: { timestamp: '2026-06-28T00:00:00Z' }, report: [] };
  const current = { kind: 'scale-results', meta: { timestamp: '2026-06-29T00:00:00Z' }, result: results };

  assert.equal(findMigrationTimestamp([legacy, current]), '2026-06-29T00:00:00Z');
  assert.equal(findMigrationTimestamp([current]), null);
});

test('extracts all legacy metrics into the same eleven chart IDs', () => {
  const legacyPayload = JSON.parse(readFileSync(join(__dirname, 'example-report.json'), 'utf8'));
  const legacyRun = {
    kind: 'ginkgo-report',
    meta: { timestamp: '2026-03-06T06:00:00Z', path: 'Public/report.json', commit: 'legacy' },
    report: legacyPayload,
  };
  const observations = extractChartObservations([legacyRun]);

  assert.equal(observations.length, 13);
  assert.equal(new Set(observations.map(point => point.chartId)).size, 11);
  assert.ok(observations.every(point => point.details && point.rawTiming !== undefined));
});

test('extracts canonical chart fields from legacy Ginkgo log output', () => {
  const output = 'time=2026-06-20T10:00:00Z level=INFO msg="Scheduled pods" "Total time"=6m36.5s nodes=500 jobs=4000';

  assert.deepEqual(parseMetricsFromOutput(output), {
    jobs: 4000,
    nodes: 500,
    time: '6m36.5s',
  });
});

test('graphs legacy reports that only contain Ginkgo output metrics', () => {
  const legacyPayload = JSON.parse(readFileSync(join(__dirname, 'example-report.json'), 'utf8'));
  legacyPayload[0].SpecReports.forEach(spec => { spec.ReportEntries = []; });
  const legacyRun = {
    kind: 'ginkgo-report',
    meta: { timestamp: '2026-03-06T06:00:00Z', path: 'Public/report.json', commit: 'legacy' },
    report: legacyPayload,
  };

  const observations = extractChartObservations([legacyRun]);
  assert.equal(observations.length, 8);
  assert.ok(observations.every(point => point.details.time));
  assert.ok(observations.some(point => point.source === 'ginkgo-report' && point.details.nodes === 500));
});

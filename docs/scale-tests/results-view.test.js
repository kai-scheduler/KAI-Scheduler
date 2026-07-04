// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');

const {
  detailEntries,
  isResultVisible,
  resultSearchText,
  summarizeRun,
} = require('./results-view.js');

test('formats every detail without dropping nested values', () => {
  const details = {
    nodes: 500,
    outcome: { completed: 100, pending: 2 },
    samples: [1, 2],
    optional: null,
  };

  assert.deepEqual(detailEntries(details), [
    ['nodes', '500'],
    ['optional', 'null'],
    ['outcome', '{\n  "completed": 100,\n  "pending": 2\n}'],
    ['samples', '[\n  1,\n  2\n]'],
  ]);
});

test('derives scale run counts from tests and detects a top-level mismatch', () => {
  const run = {
    kind: 'scale-results',
    result: {
      status: 'success',
      tests: [
        { test_name: 'passed', status: 'success', details: {} },
        { test_name: 'failed', status: 'failure', details: { reason: 'timeout' } },
      ],
    },
  };

  assert.deepEqual(summarizeRun(run), { passed: 1, failed: 1, skipped: 0, total: 2, mismatch: true });
});

test('searches custom names, statuses, detail keys, and detail values', () => {
  const result = {
    test_name: 'NCCL Simulation on empty cluster',
    status: 'success',
    details: { 'pending pods': 2, nested: { reason: 'capacity' } },
  };

  const searchable = resultSearchText(result);
  assert.match(searchable, /nccl simulation/);
  assert.match(searchable, /pending pods/);
  assert.match(searchable, /capacity/);
  assert.equal(isResultVisible(result, { pass: true, fail: true, skip: true, query: 'capacity' }), true);
  assert.equal(isResultVisible(result, { pass: false, fail: true, skip: true, query: '' }), false);
});

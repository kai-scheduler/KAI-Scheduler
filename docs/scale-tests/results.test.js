// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const test = require('node:test');

const {
  LEGACY_GINKGO_SUPPORT_UNTIL,
  loadRun,
} = require('./results.js');

const scalePayload = JSON.parse(readFileSync(join(__dirname, 'example-results.json'), 'utf8'));
const scaleResults = scalePayload.results;
const ginkgoPayload = JSON.parse(readFileSync(join(__dirname, 'example-report.json'), 'utf8'));
const meta = {
  timestamp: '2026-06-29T08:42:33Z',
  path: 'Public/results.json',
  commit: 'aa514fc997453d075067e4aceae9319909c92ed6',
};

test('loads the July 3 scale result schema without changing the payload', () => {
  const original = structuredClone(scalePayload);
  const run = loadRun(scalePayload, meta);

  assert.equal(run.kind, 'scale-results');
  assert.strictEqual(run.payload, scalePayload);
  assert.strictEqual(run.result, scalePayload.results);
  assert.notStrictEqual(run.metadata, scalePayload.metadata);
  assert.equal(run.metadata.kai_scheduler_ref, 'main');
  assert.equal(run.metadata.test_focus, undefined);
  assert.equal(run.metadata.kai_commit_hash, undefined);
  assert.equal(run.metadata.timestamp, undefined);
  assert.equal(run.payload.metadata.test_focus, '');
  assert.ok(run.payload.metadata.kai_commit_hash);
  assert.ok(run.payload.metadata.timestamp);
  assert.deepEqual(run.payload, original);
  assert.deepEqual(run.meta, meta);
});

test('keeps the commit hash when an empty scheduler ref is removed', () => {
  const payload = {
    metadata: {
      kai_scheduler_ref: '',
      kai_commit_hash: 'abc123',
      nested: { remove: '', keep: 'value' },
      values: ['', 'keep'],
    },
    results: scaleResults,
  };

  const run = loadRun(payload, meta);
  assert.deepEqual(run.metadata, {
    kai_commit_hash: 'abc123',
    nested: { keep: 'value' },
    values: ['', 'keep'],
  });
  assert.equal(payload.metadata.nested.remove, '');
});

test('loads an earlier unwrapped scale result as a compatibility input', () => {
  const run = loadRun(scaleResults, meta);
  assert.equal(run.kind, 'scale-results');
  assert.strictEqual(run.payload, scaleResults);
  assert.strictEqual(run.result, scaleResults);
  assert.equal(run.metadata, null);
});

test('loads a Ginkgo report before the compatibility cutoff', () => {
  const run = loadRun(ginkgoPayload, meta, LEGACY_GINKGO_SUPPORT_UNTIL - 1);

  assert.equal(run.kind, 'ginkgo-report');
  assert.strictEqual(run.report, ginkgoPayload);
});

test('rejects a Ginkgo report after the compatibility cutoff', () => {
  assert.throws(
    () => loadRun(ginkgoPayload, meta, LEGACY_GINKGO_SUPPORT_UNTIL),
    /Legacy Ginkgo report support expired/,
  );
});

test('rejects an unknown payload shape', () => {
  assert.throws(() => loadRun({ status: 'success' }, meta), /Unknown scale-test result schema/);
});

test('rejects a malformed scale result without changing valid detail values', () => {
  assert.throws(
    () => loadRun({ status: 'success', tests: [{ test_name: '', status: 'success', details: null }] }, meta),
    /Invalid scale-test result at index 0/,
  );
});

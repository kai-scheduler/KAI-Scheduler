// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

'use strict';

(function exposeResultsView(root, factory) {
  const api = factory();
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  root.ScaleTestResultsView = api;
})(typeof globalThis !== 'undefined' ? globalThis : window, function createResultsViewApi() {
  function stableValue(value) {
    if (Array.isArray(value)) return value.map(stableValue);
    if (value && typeof value === 'object') {
      return Object.fromEntries(Object.keys(value).sort().map(key => [key, stableValue(value[key])]));
    }
    return value;
  }

  function formatDetailValue(value) {
    if (value !== null && typeof value === 'object') return JSON.stringify(stableValue(value), null, 2);
    return String(value);
  }

  function detailEntries(details) {
    if (!details || typeof details !== 'object' || Array.isArray(details)) return [];
    return Object.keys(details).sort().map(key => [key, formatDetailValue(details[key])]);
  }

  function resultSearchText(result) {
    return [result?.test_name, result?.status, JSON.stringify(result?.details || {})]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();
  }

  function isResultVisible(result, filters) {
    const state = result?.status === 'success' ? 'pass' : result?.status === 'failure' ? 'fail' : 'skip';
    if (!filters[state]) return false;
    return !filters.query || resultSearchText(result).includes(filters.query.toLowerCase());
  }

  function summarizeRun(run) {
    if (run.kind === 'scale-results') {
      const tests = run.result.tests || [];
      const passed = tests.filter(result => result.status === 'success').length;
      const failed = tests.filter(result => result.status === 'failure').length;
      const skipped = tests.length - passed - failed;
      const derivedStatus = failed > 0 ? 'failure' : 'success';
      return {
        passed,
        failed,
        skipped,
        total: tests.length,
        mismatch: run.result.status !== derivedStatus,
      };
    }

    const suite = (Array.isArray(run.report) ? run.report[0] : run.report) || {};
    const specs = (suite.SpecReports || []).filter(spec => spec.LeafNodeType === 'It' || spec.State === 'failed');
    const passed = specs.filter(spec => spec.State === 'passed').length;
    const failed = specs.filter(spec => spec.State === 'failed').length;
    return {
      passed,
      failed,
      skipped: specs.length - passed - failed,
      total: specs.length,
      mismatch: false,
    };
  }

  return { detailEntries, isResultVisible, resultSearchText, summarizeRun };
});

// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

'use strict';

(function exposeResults(root, factory) {
  const api = factory();
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  root.ScaleTestResults = api;
})(typeof globalThis !== 'undefined' ? globalThis : window, function createResultsApi() {
  const LEGACY_GINKGO_SUPPORT_UNTIL = Date.parse('2026-07-30T00:00:00Z');

  function isResultSet(payload) {
    return payload !== null
      && typeof payload === 'object'
      && !Array.isArray(payload)
      && typeof payload.status === 'string'
      && Array.isArray(payload.tests);
  }

  function isScaleResults(payload) {
    return payload !== null
      && typeof payload === 'object'
      && !Array.isArray(payload)
      && payload.metadata !== null
      && typeof payload.metadata === 'object'
      && isResultSet(payload.results);
  }

  function isLegacyScaleResults(payload) {
    return isResultSet(payload);
  }

  function isGinkgoReport(payload) {
    const suite = Array.isArray(payload) ? payload[0] : payload;
    return suite !== null && typeof suite === 'object' && Array.isArray(suite.SpecReports);
  }

  function validateScaleResults(payload) {
    payload.tests.forEach((result, index) => {
      if (!result || typeof result !== 'object'
          || typeof result.test_name !== 'string' || !result.test_name.trim()
          || typeof result.status !== 'string') {
        throw new Error(`Invalid scale-test result at index ${index}`);
      }
    });
  }

  function dropEmptyMetadataFields(value) {
    if (Array.isArray(value)) return value.map(dropEmptyMetadataFields);
    if (!value || typeof value !== 'object') return value;
    return Object.fromEntries(
      Object.entries(value)
        .filter(([, fieldValue]) => fieldValue !== '')
        .map(([key, fieldValue]) => [key, dropEmptyMetadataFields(fieldValue)]),
    );
  }

  function sanitizeMetadata(metadata) {
    const sanitized = dropEmptyMetadataFields(metadata);
    delete sanitized.timestamp;
    if (Object.hasOwn(sanitized, 'kai_scheduler_ref')) delete sanitized.kai_commit_hash;
    return sanitized;
  }

  // The scale test records service client config in its results; the upload workflow moves it to metadata.
  function runMetadata(metadata, result) {
    const serviceClientConfig = result.service_client_config;
    if (!metadata && !serviceClientConfig) return null;
    const merged = { ...metadata };
    if (serviceClientConfig && !Object.hasOwn(merged, 'service_client_config')) {
      merged.service_client_config = serviceClientConfig;
    }
    return sanitizeMetadata(merged);
  }

  function loadRun(payload, meta, now = Date.now()) {
    if (isScaleResults(payload)) {
      validateScaleResults(payload.results);
      return {
        kind: 'scale-results',
        meta: { ...meta },
        payload,
        result: payload.results,
        metadata: runMetadata(payload.metadata, payload.results),
      };
    }

    if (isLegacyScaleResults(payload)) {
      validateScaleResults(payload);
      return {
        kind: 'scale-results',
        meta: { ...meta },
        payload,
        result: payload,
        metadata: runMetadata(null, payload),
      };
    }

    if (isGinkgoReport(payload)) {
      if (now >= LEGACY_GINKGO_SUPPORT_UNTIL) {
        throw new Error('Legacy Ginkgo report support expired on 2026-07-30');
      }
      return { kind: 'ginkgo-report', meta: { ...meta }, report: payload };
    }

    throw new Error('Unknown scale-test result schema');
  }

  function getGinkgoSuite(run) {
    if (run.kind !== 'ginkgo-report') return null;
    return (Array.isArray(run.report) ? run.report[0] : run.report) || {};
  }

  return {
    LEGACY_GINKGO_SUPPORT_UNTIL,
    getGinkgoSuite,
    isGinkgoReport,
    isLegacyScaleResults,
    isScaleResults,
    loadRun,
  };
});

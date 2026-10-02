/* Copyright 2026 NVIDIA CORPORATION */
/* SPDX-License-Identifier: Apache-2.0 */

'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');

const {
  buildConfigPatch,
  buildKubectlPatch,
  calculate,
  presets,
  validate,
} = require('./calculator.js');

test('500-node reference recommends 6 Gi request and 7 Gi limit', () => {
  const result = calculate(presets[500]);

  assert.deepEqual(result.recommendations, {
    memoryRequestGiB: 6,
    memoryLimitGiB: 7,
    cpuRequest: 2,
    cpuLimitIfRequired: 4,
  });
  assert.equal(result.inferred.largestSearch, 130048);
  assert.equal(result.componentsGiB.searchReserve, 2);
});

test('1000-node reference uses the tested 7 Gi request and 8 Gi limit', () => {
  const result = calculate(presets[1000]);

  assert.deepEqual(result.recommendations, {
    memoryRequestGiB: 7,
    memoryLimitGiB: 8,
    cpuRequest: 3,
    cpuLimitIfRequired: 5,
  });
  assert.equal(result.inferred.largestSearch, 516096);
  assert.equal(result.componentsGiB.searchReserve, 2.5);
});

test('service recommendations combine controller formulas with profile floors', () => {
  const result = calculate(presets[500]);
  const services = Object.fromEntries(result.services.map(service => [service.key, service]));

  assert.deepEqual(
    [services.binder.memoryRequest, services.binder.memoryLimit],
    ['4864Mi', '5888Mi'],
  );
  assert.deepEqual(
    [services.podGrouper.memoryRequest, services.podGrouper.memoryLimit],
    ['1500Mi', '2Gi'],
  );
  assert.deepEqual(
    [services.podGroupController.memoryRequest, services.podGroupController.memoryLimit],
    ['2Gi', '3Gi'],
  );
  assert.deepEqual(
    [services.admission.memoryRequest, services.admission.memoryLimit],
    ['64Mi', '128Mi'],
  );
  assert.equal(services.podGroupController.cpuLimit, null);
  assert.equal(services.operator.managedBy, 'Helm');
});

test('Config patch uses CRD field names and excludes the Helm-managed operator', () => {
  const result = calculate(presets[1000]);
  const patch = buildConfigPatch(result);

  assert.deepEqual(Object.keys(patch.spec), [
    'scheduler',
    'binder',
    'podGrouper',
    'podGroupController',
    'queueController',
    'admission',
  ]);
  assert.deepEqual(patch.spec.scheduler.service.resources.requests, {
    cpu: '3',
    memory: '7Gi',
  });
  assert.deepEqual(patch.spec.binder.service.resources.requests, {
    cpu: '1',
    memory: '9344Mi',
  });
  assert.equal(patch.spec.podGroupController.service.resources.limits.cpu, null);
  assert.equal(patch.spec.queueController.service.resources.limits.cpu, null);
  assert.equal(patch.spec.operator, undefined);
});

test('kubectl command targets the named cluster-scoped Config', () => {
  const result = calculate(presets[500]);
  const command = buildKubectlPatch(result, 'production-kai-config');
  const prefix = 'kubectl patch configs.kai.scheduler production-kai-config --type=merge --patch \'';

  assert.ok(command.startsWith(prefix));
  assert.ok(command.endsWith('\''));
  assert.deepEqual(JSON.parse(command.slice(prefix.length, -1)), buildConfigPatch(result));
  assert.throws(
    () => buildKubectlPatch(result, 'unsafe name; echo no'),
    /valid lowercase Kubernetes resource name/,
  );
  assert.throws(
    () => buildKubectlPatch(result, 'kai.-config'),
    /valid lowercase Kubernetes resource name/,
  );
});

test('scenarios beyond the 1000-node envelope are marked untested', () => {
  const result = calculate({
    ...presets[1000],
    nodes: 1009,
    eligibleNodes: 1009,
  });

  assert.deepEqual(result.profile, {
    name: '1000',
    exceedsTestedProfiles: true,
  });
});

test('search reserve increases by 0.5 GiB for each fourfold pressure tier', () => {
  const input = {
    ...presets[500],
    nodes: 4000,
    eligibleNodes: 4000,
    averagePodsPerWorkload: 1,
    largestJobPods: 1,
  };
  assert.equal(calculate(input).componentsGiB.searchReserve, 0.5);

  input.largestJobPods = 2;
  assert.equal(calculate(input).componentsGiB.searchReserve, 1);

  input.largestJobPods = 5;
  assert.equal(calculate(input).componentsGiB.searchReserve, 1.5);
});

test('backlog reserve is capped at 1 GiB', () => {
  const result = calculate({
    ...presets[500],
    totalGPUs: 1,
    gpusPerWorkerPod: 1,
  });

  assert.equal(result.componentsGiB.backlogReserve, 1);
});

test('validation rejects inconsistent cluster and workload values', () => {
  const errors = validate({
    ...presets[500],
    eligibleNodes: 600,
    largestJobPods: 50000,
    averagePodsPerWorkload: 60000,
  });

  assert.deepEqual(errors.map(error => error.field), [
    'eligibleNodes',
    'largestJobPods',
    'averagePodsPerWorkload',
  ]);
});

test('calculation exposes inferred PodGroup and BindRequest bounds', () => {
  const result = calculate({
    ...presets[500],
    totalPods: 10000,
    workloads: 100,
    averagePodsPerWorkload: 20,
  });

  assert.equal(result.inferred.workloadPods, 2000);
  assert.equal(result.inferred.podGroups, 100);
  assert.equal(result.inferred.bindRequestsUpperBound, 2000);
});

test('inferred workload Pod count rounds fractional estimates up', () => {
  const result = calculate({
    ...presets[500],
    totalPods: 100,
    workloads: 3,
    averagePodsPerWorkload: 1.1,
    largestJobPods: 2,
  });

  assert.equal(result.inferred.workloadPods, 4);
});

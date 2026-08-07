/* Copyright 2026 NVIDIA CORPORATION */
/* SPDX-License-Identifier: Apache-2.0 */

(function initialize(root, factory) {
  const calculator = factory();
  if (typeof module === 'object' && module.exports) {
    module.exports = calculator;
  } else {
    root.KAIResourceCalculator = calculator;
    document.addEventListener('DOMContentLoaded', calculator.initializePage);
  }
}(typeof globalThis !== 'undefined' ? globalThis : this, function calculatorFactory() {
  'use strict';

  const complexityReserveGiB = {
    standard: 0,
    'topology-or-reclaim': 0.5,
    heavy: 1,
  };

  const testedProfiles = {
    500: {
      envelope: {
        nodes: 520,
        totalPods: 46100,
        workloads: 8000,
        averagePodsPerWorkload: 57,
        largestJobPods: 500,
        totalGPUs: 4000,
      },
      services: {
        scheduler: {cpuRequest: '2', cpuLimit: '4', memoryRequestMi: 4096, memoryLimitMi: 7168},
        binder: {cpuRequest: '250m', cpuLimit: '1', memoryRequestMi: 3072, memoryLimitMi: 4096},
        podGrouper: {cpuRequest: '250m', cpuLimit: '1', memoryRequestMi: 1500, memoryLimitMi: 2048},
        podGroupController: {cpuRequest: '500m', cpuLimit: null, memoryRequestMi: 2048, memoryLimitMi: 3072},
        queueController: {cpuRequest: '250m', cpuLimit: null, memoryRequestMi: 256, memoryLimitMi: 512},
        admission: {cpuRequest: '50m', cpuLimit: '250m', memoryRequestMi: 64, memoryLimitMi: 128},
        operator: {cpuRequest: '25m', cpuLimit: '100m', memoryRequestMi: 128, memoryLimitMi: 256},
      },
    },
    1000: {
      envelope: {
        nodes: 1008,
        totalPods: 90200,
        workloads: 16000,
        averagePodsPerWorkload: 103,
        largestJobPods: 1000,
        totalGPUs: 8000,
      },
      services: {
        scheduler: {cpuRequest: '3', cpuLimit: '5', memoryRequestMi: 7168, memoryLimitMi: 8192},
        binder: {cpuRequest: '1', cpuLimit: '2', memoryRequestMi: 5120, memoryLimitMi: 6144},
        podGrouper: {cpuRequest: '500m', cpuLimit: '2', memoryRequestMi: 3072, memoryLimitMi: 4096},
        podGroupController: {cpuRequest: '1', cpuLimit: null, memoryRequestMi: 3500, memoryLimitMi: 4096},
        queueController: {cpuRequest: '500m', cpuLimit: null, memoryRequestMi: 400, memoryLimitMi: 512},
        admission: {cpuRequest: '50m', cpuLimit: '250m', memoryRequestMi: 64, memoryLimitMi: 128},
        operator: {cpuRequest: '25m', cpuLimit: '100m', memoryRequestMi: 128, memoryLimitMi: 256},
      },
    },
  };

  const serviceDefinitions = [
    {key: 'scheduler', label: 'Scheduler', configKey: 'scheduler'},
    {key: 'binder', label: 'Binder', configKey: 'binder'},
    {key: 'podGrouper', label: 'Pod grouper', configKey: 'podGrouper'},
    {key: 'podGroupController', label: 'PodGroup controller', configKey: 'podGroupController'},
    {key: 'queueController', label: 'Queue controller', configKey: 'queueController'},
    {key: 'admission', label: 'Admission, per replica', configKey: 'admission'},
    {key: 'operator', label: 'Operator', configKey: null},
  ];

  const presets = {
    500: {
      nodes: 508,
      eligibleNodes: 508,
      totalPods: 46076,
      workloads: 810,
      averagePodsPerWorkload: 56.78,
      largestJobPods: 256,
      totalGPUs: 4000,
      gpusPerWorkerPod: 8,
      complexity: 'standard',
    },
    1000: {
      nodes: 1008,
      eligibleNodes: 1008,
      totalPods: 90193,
      workloads: 900,
      averagePodsPerWorkload: 102.3,
      largestJobPods: 512,
      totalGPUs: 8000,
      gpusPerWorkerPod: 8,
      complexity: 'standard',
    },
  };

  function number(input, field) {
    const value = Number(input[field]);
    if (!Number.isFinite(value)) {
      throw new Error(`${field} must be a number`);
    }
    return value;
  }

  function validate(input) {
    const errors = [];
    const positiveFields = [
      ['nodes', 'Nodes'],
      ['eligibleNodes', 'Eligible nodes'],
      ['totalPods', 'Peak total Pods'],
      ['workloads', 'Active workloads'],
      ['averagePodsPerWorkload', 'Average workload size'],
      ['largestJobPods', 'Largest workload'],
      ['totalGPUs', 'Total GPUs'],
      ['gpusPerWorkerPod', 'GPUs per worker Pod'],
    ];

    const values = {};
    for (const [field, label] of positiveFields) {
      values[field] = Number(input[field]);
      if (!Number.isFinite(values[field]) || values[field] <= 0) {
        errors.push({field, message: `${label} must be greater than zero.`});
      }
    }

    const integerFields = [
      ['nodes', 'Nodes'],
      ['eligibleNodes', 'Eligible nodes'],
      ['totalPods', 'Peak total Pods'],
      ['workloads', 'Active workloads'],
      ['largestJobPods', 'Largest workload'],
    ];
    for (const [field, label] of integerFields) {
      if (Number.isFinite(values[field]) && !Number.isInteger(values[field])) {
        errors.push({field, message: `${label} must be a whole number.`});
      }
    }

    if (values.eligibleNodes > values.nodes) {
      errors.push({field: 'eligibleNodes', message: 'Eligible nodes cannot exceed total nodes.'});
    }
    if (values.largestJobPods > values.totalPods) {
      errors.push({field: 'largestJobPods', message: 'Largest workload cannot exceed peak total Pods.'});
    }
    if (values.averagePodsPerWorkload > values.largestJobPods) {
      errors.push({field: 'averagePodsPerWorkload', message: 'Average workload size cannot exceed largest workload size.'});
    }
    if (values.gpusPerWorkerPod > values.totalGPUs) {
      errors.push({field: 'gpusPerWorkerPod', message: 'GPUs per worker Pod cannot exceed total GPUs.'});
    }
    if (!(input.complexity in complexityReserveGiB)) {
      errors.push({field: 'complexity', message: 'Select a valid scheduling complexity.'});
    }

    return errors;
  }

  function tierReserve(value, baseline, maxTier = Infinity) {
    if (value <= baseline) return 0;
    return 0.5 * Math.min(maxTier, Math.ceil(Math.log(value / baseline) / Math.log(4)));
  }

  function fitsEnvelope(input, envelope) {
    return Object.entries(envelope).every(([field, maximum]) => number(input, field) <= maximum);
  }

  function selectTestedProfile(input) {
    const fits500 = fitsEnvelope(input, testedProfiles[500].envelope);
    const fits1000 = fitsEnvelope(input, testedProfiles[1000].envelope);
    const name = fits500 ? '500' : '1000';
    return {
      name,
      profile: testedProfiles[name],
      exceedsTestedProfiles: !fits1000,
    };
  }

  function roundUpMi(value) {
    return Math.ceil(value / 128) * 128;
  }

  function formatMemoryMi(value) {
    return value % 1024 === 0 ? `${value / 1024}Gi` : `${value}Mi`;
  }

  function withFormulaMemory(profileService, formulaMi) {
    const memoryRequestMi = Math.max(profileService.memoryRequestMi, roundUpMi(formulaMi));
    const profileHeadroomMi = profileService.memoryLimitMi - profileService.memoryRequestMi;
    const memoryLimitMi = Math.max(profileService.memoryLimitMi, memoryRequestMi + profileHeadroomMi);
    return {
      cpuRequest: profileService.cpuRequest,
      cpuLimit: profileService.cpuLimit,
      memoryRequest: formatMemoryMi(memoryRequestMi),
      memoryLimit: formatMemoryMi(memoryLimitMi),
    };
  }

  function fromProfile(profileService) {
    return {
      cpuRequest: profileService.cpuRequest,
      cpuLimit: profileService.cpuLimit,
      memoryRequest: formatMemoryMi(profileService.memoryRequestMi),
      memoryLimit: formatMemoryMi(profileService.memoryLimitMi),
    };
  }

  function calculateServiceResources(input, inferred, recommendations, profileSelection) {
    const profileServices = profileSelection.profile.services;
    const formulaMi = {
      binder: 256 + Math.max(
        50 * number(input, 'totalPods') / 1000,
        100 * inferred.bindRequestsUpperBound / 1000,
      ),
      podGrouper: 256 + 25 * inferred.workloadPods / 1000,
      podGroupController: 256 + 35 * inferred.workloadPods / 1000,
      queueController: 64 + 20 * inferred.podGroups / 1000,
    };

    const calculated = {
      scheduler: {
        cpuRequest: String(recommendations.cpuRequest),
        cpuLimit: String(recommendations.cpuLimitIfRequired),
        memoryRequest: `${recommendations.memoryRequestGiB}Gi`,
        memoryLimit: `${recommendations.memoryLimitGiB}Gi`,
      },
      binder: withFormulaMemory(profileServices.binder, formulaMi.binder),
      podGrouper: withFormulaMemory(profileServices.podGrouper, formulaMi.podGrouper),
      podGroupController: withFormulaMemory(profileServices.podGroupController, formulaMi.podGroupController),
      queueController: withFormulaMemory(profileServices.queueController, formulaMi.queueController),
      admission: fromProfile(profileServices.admission),
      operator: fromProfile(profileServices.operator),
    };

    return serviceDefinitions.map(definition => ({
      ...definition,
      ...calculated[definition.key],
      managedBy: definition.configKey === null ? 'Helm' : 'Config',
    }));
  }

  function calculate(input) {
    const errors = validate(input);
    if (errors.length > 0) {
      const error = new Error(errors.map(({message}) => message).join(' '));
      error.validationErrors = errors;
      throw error;
    }

    const nodes = number(input, 'nodes');
    const eligibleNodes = number(input, 'eligibleNodes');
    const totalPods = number(input, 'totalPods');
    const workloads = number(input, 'workloads');
    const averagePodsPerWorkload = number(input, 'averagePodsPerWorkload');
    const largestJobPods = number(input, 'largestJobPods');
    const totalGPUs = number(input, 'totalGPUs');
    const gpusPerWorkerPod = number(input, 'gpusPerWorkerPod');

    const workloadPods = Math.min(totalPods, Math.ceil(workloads * averagePodsPerWorkload));
    const workerCapacity = Math.max(1, Math.floor(totalGPUs / gpusPerWorkerPod));
    const backlogRatio = workloadPods / workerCapacity;

    const cacheComponentsGiB = {
      base: 1,
      pods: 0.16 * totalPods / 10000,
      workloads: 0.01 * workloads / 1000,
      nodes: 0.02 * eligibleNodes / 1000,
      bindRequests: 0.04 * workloadPods / 10000,
    };
    const cacheGiB = Object.values(cacheComponentsGiB).reduce((sum, value) => sum + value, 0);

    const largestSearch = largestJobPods * eligibleNodes;
    const searchReserveGiB = 0.5 + tierReserve(largestSearch, 4000);
    const backlogReserveGiB = tierReserve(backlogRatio, 1, 2);
    const schedulingComplexityReserveGiB = complexityReserveGiB[input.complexity];
    const schedulingReserveGiB = searchReserveGiB + backlogReserveGiB + schedulingComplexityReserveGiB;
    const subtotalGiB = cacheGiB + schedulingReserveGiB;
    const safetyHeadroomGiB = subtotalGiB * 0.25;
    const formulaMemoryLimitGiB = Math.ceil(subtotalGiB + safetyHeadroomGiB);
    const formulaMemoryRequestGiB = Math.ceil(0.75 * formulaMemoryLimitGiB);
    const cpuRequest = Math.ceil(0.5 + eligibleNodes / 1000 +
      0.25 * searchReserveGiB + 0.25 * backlogReserveGiB +
      0.25 * schedulingComplexityReserveGiB);

    const profileSelection = selectTestedProfile(input);
    const profileScheduler = profileSelection.profile.services.scheduler;
    const profileSchedulerRequestGiB = profileScheduler.memoryRequestMi / 1024;
    const profileSchedulerLimitGiB = profileScheduler.memoryLimitMi / 1024;
    const profileSchedulerCpuRequest = Number(profileScheduler.cpuRequest);
    const profileSchedulerCpuLimit = Number(profileScheduler.cpuLimit);
    const recommendations = {
      memoryRequestGiB: Math.max(formulaMemoryRequestGiB, profileSchedulerRequestGiB),
      memoryLimitGiB: Math.max(formulaMemoryLimitGiB, profileSchedulerLimitGiB),
      cpuRequest: Math.max(cpuRequest, profileSchedulerCpuRequest),
      cpuLimitIfRequired: Math.max(cpuRequest + 2, profileSchedulerCpuLimit),
    };
    const inferred = {
      workloadPods,
      podGroups: Math.ceil(workloads),
      bindRequestsUpperBound: workloadPods,
      workerCapacity,
      backlogRatio,
      largestSearch,
    };

    return {
      recommendations,
      inferred,
      profile: {
        name: profileSelection.name,
        exceedsTestedProfiles: profileSelection.exceedsTestedProfiles,
      },
      services: calculateServiceResources(input, inferred, recommendations, profileSelection),
      componentsGiB: {
        cache: cacheGiB,
        cacheComponents: cacheComponentsGiB,
        schedulingReserve: schedulingReserveGiB,
        searchReserve: searchReserveGiB,
        backlogReserve: backlogReserveGiB,
        complexityReserve: schedulingComplexityReserveGiB,
        safetyHeadroom: safetyHeadroomGiB,
        subtotal: subtotalGiB,
      },
    };
  }

  function buildConfigPatch(result) {
    const spec = {};
    for (const service of result.services) {
      if (service.configKey === null) continue;
      spec[service.configKey] = {
        service: {
          resources: {
            requests: {
              cpu: service.cpuRequest,
              memory: service.memoryRequest,
            },
            limits: {
              cpu: service.cpuLimit,
              memory: service.memoryLimit,
            },
          },
        },
      };
    }
    return {spec};
  }

  function normalizeConfigName(name = 'kai-config') {
    const value = String(name).trim();
    const dnsSubdomain = /^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?(?:\.[a-z0-9](?:[-a-z0-9]*[a-z0-9])?)*$/;
    const labelsAreValid = value.split('.').every(label => label.length <= 63);
    if (value.length === 0 || value.length > 253 || !labelsAreValid || !dnsSubdomain.test(value)) {
      throw new Error('Config name must be a valid lowercase Kubernetes resource name.');
    }
    return value;
  }

  function buildKubectlPatch(result, configName = 'kai-config') {
    const name = normalizeConfigName(configName);
    const patch = JSON.stringify(buildConfigPatch(result));
    return `kubectl patch configs.kai.scheduler ${name} --type=merge --patch '${patch}'`;
  }

  function formatNumber(value, maximumFractionDigits = 0) {
    return new Intl.NumberFormat(undefined, {maximumFractionDigits}).format(value);
  }

  function formatGiB(value) {
    return `${formatNumber(value, 2)} GiB`;
  }

  function readForm(form) {
    return Object.fromEntries(new FormData(form).entries());
  }

  function setText(id, value) {
    document.getElementById(id).textContent = value;
  }

  function renderServices(result) {
    const tableBody = document.getElementById('service-resources');
    const rows = result.services.map(service => {
      const row = document.createElement('tr');
      const values = [
        service.label,
        service.cpuRequest,
        service.cpuLimit === null ? 'omit' : service.cpuLimit,
        service.memoryRequest,
        service.memoryLimit,
        service.managedBy,
      ];
      for (const value of values) {
        const cell = document.createElement('td');
        cell.textContent = value;
        row.appendChild(cell);
      }
      return row;
    });
    tableBody.replaceChildren(...rows);

    setText('service-profile', `${result.profile.name}-node tested profile floor`);
    const warning = document.getElementById('profile-warning');
    warning.hidden = !result.profile.exceedsTestedProfiles;
  }

  function renderPatchCommand(result) {
    const input = document.getElementById('config-name');
    const command = document.getElementById('patch-command');
    const copyButton = document.getElementById('copy-command');
    const status = document.getElementById('copy-status');
    try {
      command.value = buildKubectlPatch(result, input.value);
      input.removeAttribute('aria-invalid');
      copyButton.disabled = false;
      status.textContent = '';
    } catch (error) {
      command.value = '';
      input.setAttribute('aria-invalid', 'true');
      copyButton.disabled = true;
      status.textContent = error.message;
    }
  }

  function renderResult(result, statusText) {
    const {recommendations, inferred, componentsGiB} = result;
    setText('memory-request', `${recommendations.memoryRequestGiB} Gi`);
    setText('memory-limit', `${recommendations.memoryLimitGiB} Gi`);
    setText('cpu-request', `${recommendations.cpuRequest} cores`);
    setText('cpu-limit', `${recommendations.cpuLimitIfRequired} cores`);
    setText('cache-value', formatGiB(componentsGiB.cache));
    setText('reserve-value', formatGiB(componentsGiB.schedulingReserve));
    setText('headroom-value', formatGiB(componentsGiB.safetyHeadroom));
    setText('workload-pods', formatNumber(inferred.workloadPods));
    setText('worker-capacity', `${formatNumber(inferred.workerCapacity)} Pods`);
    setText('backlog-ratio', `${formatNumber(inferred.backlogRatio, 1)}×`);
    setText('largest-search', formatNumber(inferred.largestSearch));
    setText('pod-groups', formatNumber(inferred.podGroups));
    setText('bind-requests', formatNumber(inferred.bindRequestsUpperBound));
    setText('search-reserve', formatGiB(componentsGiB.searchReserve));
    setText('backlog-reserve', formatGiB(componentsGiB.backlogReserve));
    setText('complexity-reserve', formatGiB(componentsGiB.complexityReserve));
    setText('result-status', statusText);
    renderServices(result);
    renderPatchCommand(result);

    const rawLimit = componentsGiB.subtotal + componentsGiB.safetyHeadroom;
    document.getElementById('cache-meter').style.width = `${100 * componentsGiB.cache / rawLimit}%`;
    document.getElementById('reserve-meter').style.width = `${100 * componentsGiB.schedulingReserve / rawLimit}%`;
    document.getElementById('headroom-meter').style.width = `${100 * componentsGiB.safetyHeadroom / rawLimit}%`;
  }

  function renderErrors(errors, form, focusSummary) {
    const summary = document.getElementById('error-summary');
    for (const element of form.elements) {
      if (element instanceof HTMLElement && element.matches('input, select')) {
        element.removeAttribute('aria-invalid');
      }
    }

    if (errors.length === 0) {
      summary.hidden = true;
      summary.replaceChildren();
      return;
    }

    const heading = document.createElement('strong');
    heading.textContent = 'Check these inputs:';
    const list = document.createElement('ul');
    for (const {field, message} of errors) {
      const input = form.elements.namedItem(field);
      if (input instanceof HTMLElement) input.setAttribute('aria-invalid', 'true');
      const item = document.createElement('li');
      item.textContent = message;
      list.appendChild(item);
    }
    summary.replaceChildren(heading, list);
    summary.hidden = false;
    if (focusSummary) summary.focus();
  }

  function populate(form, values) {
    for (const [field, value] of Object.entries(values)) {
      const element = form.elements.namedItem(field);
      if (element) element.value = String(value);
    }
  }

  function initializePage() {
    const form = document.getElementById('calculator-form');
    if (!form) return;
    const presetButtons = Array.from(document.querySelectorAll('[data-preset]'));
    const configName = document.getElementById('config-name');
    const patchCommand = document.getElementById('patch-command');
    const copyButton = document.getElementById('copy-command');
    let activePreset = '500';
    let lastResult;

    function update(input, statusText, focusErrors = false) {
      const errors = validate(input);
      renderErrors(errors, form, focusErrors);
      if (errors.length > 0) return;
      lastResult = calculate(input);
      renderResult(lastResult, statusText);
    }

    function selectPreset(name) {
      activePreset = name;
      populate(form, presets[name]);
      for (const button of presetButtons) {
        button.setAttribute('aria-pressed', String(button.dataset.preset === name));
      }
      update(readForm(form), `Using ${name}-node example`);
    }

    for (const button of presetButtons) {
      button.addEventListener('click', () => selectPreset(button.dataset.preset));
    }

    form.addEventListener('input', () => {
      activePreset = null;
      for (const button of presetButtons) button.setAttribute('aria-pressed', 'false');
      const input = readForm(form);
      const errors = validate(input);
      if (errors.length === 0) {
        renderErrors([], form, false);
        lastResult = calculate(input);
        renderResult(lastResult, 'Custom estimate');
      } else {
        setText('result-status', 'Fix inputs to update');
      }
    });

    form.addEventListener('submit', event => {
      event.preventDefault();
      update(readForm(form), activePreset ? `Using ${activePreset}-node example` : 'Custom estimate', true);
    });

    document.getElementById('reset-button').addEventListener('click', () => selectPreset('500'));
    configName.addEventListener('input', () => {
      if (lastResult) renderPatchCommand(lastResult);
    });
    copyButton.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(patchCommand.value);
        setText('copy-status', 'Command copied.');
      } catch (error) {
        patchCommand.focus();
        patchCommand.select();
        setText('copy-status', 'Select and copy the command manually.');
      }
    });
    selectPreset('500');
  }

  return {buildConfigPatch, buildKubectlPatch, calculate, validate, presets, initializePage};
}));

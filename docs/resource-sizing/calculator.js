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
    const memoryLimitGiB = Math.ceil(subtotalGiB + safetyHeadroomGiB);
    const memoryRequestGiB = Math.ceil(0.75 * memoryLimitGiB);
    const cpuRequest = Math.ceil(0.5 + eligibleNodes / 1000 +
      0.25 * searchReserveGiB + 0.25 * backlogReserveGiB +
      0.25 * schedulingComplexityReserveGiB);

    return {
      recommendations: {
        memoryRequestGiB,
        memoryLimitGiB,
        cpuRequest,
        cpuLimitIfRequired: cpuRequest + 2,
      },
      inferred: {
        workloadPods,
        podGroups: Math.ceil(workloads),
        bindRequestsUpperBound: workloadPods,
        workerCapacity,
        backlogRatio,
        largestSearch,
      },
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
    let activePreset = '500';

    function update(input, statusText, focusErrors = false) {
      const errors = validate(input);
      renderErrors(errors, form, focusErrors);
      if (errors.length > 0) return;
      renderResult(calculate(input), statusText);
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
        renderResult(calculate(input), 'Custom estimate');
      } else {
        setText('result-status', 'Fix inputs to update');
      }
    });

    form.addEventListener('submit', event => {
      event.preventDefault();
      update(readForm(form), activePreset ? `Using ${activePreset}-node example` : 'Custom estimate', true);
    });

    document.getElementById('reset-button').addEventListener('click', () => selectPreset('500'));
    selectPreset('500');
  }

  return {calculate, validate, presets, initializePage};
}));

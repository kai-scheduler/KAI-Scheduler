// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package timeaware

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"k8s.io/client-go/kubernetes"

	e2econstant "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant"
)

const (
	prometheusOperatedServiceName = "prometheus-operated"
)

type promQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func queryPrometheusInstant(ctx context.Context, kubeClient kubernetes.Interface, query string) ([]float64, error) {
	params := map[string]string{"query": query}

	raw, err := kubeClient.
		CoreV1().
		Services(e2econstant.SystemPodsNamespace).
		ProxyGet("http", prometheusOperatedServiceName, "9090", "/api/v1/query", params).
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}

	resp := promQueryResponse{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("prometheus query status=%s", resp.Status)
	}

	values := make([]float64, 0, len(resp.Data.Result))
	for _, r := range resp.Data.Result {
		if len(r.Value) < 2 {
			continue
		}
		valueStr, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			continue
		}
		values = append(values, v)
	}
	return values, nil
}

// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const metricsDocPath = "../../../docs/metrics/METRICS.md"

var metricNameCell = regexp.MustCompile("^\\|\\s*`([^`]+)`\\s*\\|")

// TestMetricsDocMatchesRegisteredMetrics keeps docs/metrics/METRICS.md in sync
// with the metrics registered through promauto: it fails when a registered
// metric is missing from the component's section of the doc, or when the doc
// lists a metric that is no longer registered.
func TestMetricsDocMatchesRegisteredMetrics(t *testing.T) {
	components := []struct {
		docSection  string
		metricsFile string
	}{
		{docSection: "Scheduler Metrics", metricsFile: "metrics.go"},
		{docSection: "Queue Controller Metrics", metricsFile: "../../queuecontroller/metrics/metrics.go"},
	}

	for _, component := range components {
		t.Run(component.docSection, func(t *testing.T) {
			registered := registeredMetricNames(t, component.metricsFile)
			documented := documentedMetricNames(t, metricsDocPath, component.docSection)
			require.NotEmpty(t, registered, "no promauto metrics found in %s", component.metricsFile)

			require.Empty(t, difference(registered, documented),
				"metrics registered in %s but missing from the %q section of %s",
				component.metricsFile, component.docSection, metricsDocPath)
			require.Empty(t, difference(documented, registered),
				"metrics listed in the %q section of %s but not registered in %s",
				component.docSection, metricsDocPath, component.metricsFile)
		})
	}
}

// registeredMetricNames returns the Name of every metric created with a
// promauto.New* call in the given Go file. A name that isn't a string literal
// fails the test, so new metrics can't silently skip the check.
func registeredMetricNames(t *testing.T, path string) []string {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	require.NoError(t, err)

	var names []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isPromautoCall(call) {
			return true
		}
		require.NotEmpty(t, call.Args, "promauto call without options at %s", fileSet.Position(call.Pos()))
		opts, ok := call.Args[0].(*ast.CompositeLit)
		require.True(t, ok, "promauto options are not a literal at %s", fileSet.Position(call.Pos()))

		for _, element := range opts.Elts {
			field, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, ok := field.Key.(*ast.Ident); !ok || key.Name != "Name" {
				continue
			}
			value, ok := field.Value.(*ast.BasicLit)
			require.True(t, ok && value.Kind == token.STRING,
				"metric name is not a string literal at %s", fileSet.Position(field.Pos()))
			name, err := strconv.Unquote(value.Value)
			require.NoError(t, err)
			names = append(names, name)
		}
		return true
	})
	return names
}

func isPromautoCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "promauto" && strings.HasPrefix(selector.Sel.Name, "New")
}

// documentedMetricNames returns the names in the first column of the
// "Metric Name" tables under the given level-2 heading of the metrics doc.
func documentedMetricNames(t *testing.T, path, section string) []string {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	var names []string
	inSection, inMetricTable, foundSection := false, false, false
	for _, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "## ") {
			inSection = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == section
			foundSection = foundSection || inSection
			inMetricTable = false
			continue
		}
		if !inSection {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			inMetricTable = false
			continue
		}
		if strings.HasPrefix(line, "| Metric Name") {
			inMetricTable = true
			continue
		}
		if match := metricNameCell.FindStringSubmatch(line); inMetricTable && match != nil {
			names = append(names, match[1])
		}
	}
	require.True(t, foundSection, "section %q not found in %s", section, path)
	return names
}

// difference returns the names in a that are not in b.
func difference(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, name := range b {
		inB[name] = true
	}
	var missing []string
	for _, name := range a {
		if !inB[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

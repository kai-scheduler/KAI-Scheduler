// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"encoding/json"
	"strings"
)

// identityFields are JSON keys whose string values are object names.
var identityFields = map[string]struct{}{
	"name":              {},
	"Name":              {},
	"namespace":         {},
	"Namespace":         {},
	"uid":               {},
	"UID":               {},
	"nodeName":          {},
	"node":              {},
	"queue":             {},
	"parentQueue":       {},
	"priorityClassName": {},
	"selectedNode":      {},
	"topology":          {},
	"deviceClassName":   {},
	"pool":              {},
	"clusterName":       {},
	"shardName":         {},
	"claimName":         {},
	"resourceClaimName": {},
	"podName":           {},
	"nodePool":          {},
}

// labelMaps are objects whose keys and values are labels or selectors.
var labelMaps = map[string]struct{}{
	"labels":       {},
	"nodeSelector": {},
	"matchLabels":  {},
}

// redactCtx carries the hasher and the label keys whose values must stay numeric.
// Gt and Lt compare those values as integers, so hashing them would change the match.
type redactCtx struct {
	h           Hasher
	numericKeys map[string]struct{}
}

// RedactEnvelope hashes or drops identifying strings in a record.
// Strings are hashed or dropped unless a rule keeps them. Label keys and selector
// values go through Hasher.Label so both sides of a match hash the same way.
func RedactEnvelope(env *Envelope, h Hasher) error {
	if env == nil || len(env.Record) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(env.Record, &decoded); err != nil {
		return err
	}
	ctx := &redactCtx{h: h, numericKeys: map[string]struct{}{}}
	collectNumericKeys(decoded, ctx.numericKeys)
	raw, err := json.Marshal(walk(decoded, "", ctx))
	if err != nil {
		return err
	}
	env.Record = raw
	return nil
}

func collectNumericKeys(value any, keys map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		if op, _ := typed["operator"].(string); op == "Gt" || op == "Lt" {
			if key, _ := typed["key"].(string); key != "" {
				keys[key] = struct{}{}
			}
		}
		for _, child := range typed {
			collectNumericKeys(child, keys)
		}
	case []any:
		for _, child := range typed {
			collectNumericKeys(child, keys)
		}
	}
}

func walk(value any, path string, ctx *redactCtx) any {
	switch typed := value.(type) {
	case map[string]any:
		return walkMap(typed, path, ctx)
	case []any:
		for i := range typed {
			typed[i] = walk(typed[i], path, ctx)
		}
		return typed
	default:
		return value
	}
}

func walkMap(in map[string]any, path string, ctx *redactCtx) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, child := range in {
		if dropKey(key) {
			continue
		}
		if key == "annotations" {
			out[key] = hashAnnotations(child, ctx.h)
			continue
		}
		if key == "queues" {
			if usageQueues, ok := child.(map[string]any); ok {
				out[key] = hashNamedMap(usageQueues, ctx)
				continue
			}
		}
		if _, labelMap := labelMaps[key]; labelMap {
			if flatStringMap(child) {
				out[key] = hashLabelObject(child, ctx)
				continue
			}
		}
		if key == "arguments" && strings.HasSuffix(path, "plugins") || (key == "arguments" && strings.Contains(path, "plugin")) {
			out[key] = hashPluginArgs(child, ctx.h)
			continue
		}
		if key == "namespaces" {
			if hashed, ok := hashNameList(child, ctx.h); ok {
				out[key] = hashed
				continue
			}
		}
		if key == "reasonCodes" {
			out[key] = keepReasonCodes(child)
			continue
		}
		if text, ok := child.(string); ok && topologyLabelField(key, in) {
			out[key] = ctx.h.LabelKey(text)
			continue
		}
		if requirementKey, values, ok := requirement(key, child, in); ok && key == "values" {
			operator, _ := in["operator"].(string)
			out[key] = hashRequirementValues(requirementKey, operator, values, ctx.h)
			continue
		}
		if key == "key" {
			if text, ok := child.(string); ok {
				out[key] = requirementKeyName(text, ctx.h)
				continue
			}
		}
		if _, ident := identityFields[key]; ident {
			if text, ok := child.(string); ok {
				// Plugin names are scheduler code identifiers. Hashing them makes a
				// redacted config unable to load predicates, so replay ignores node selectors.
				if key == "name" && strings.Contains(path, "plugins") {
					out[key] = text
					continue
				}
				out[key] = ctx.h.Hash(text)
				continue
			}
		}
		if key == "message" || key == "error" {
			continue
		}
		if key == "reason" {
			if text, ok := child.(string); ok && reasonCode(text) {
				out[key] = text
			}
			continue
		}
		if key == "nodePoolLabelValue" || key == "NodePoolLabelValue" {
			if text, ok := child.(string); ok {
				labelKey := stringField(in, "nodePoolLabelKey", "NodePoolLabelKey")
				out[key] = ctx.h.LabelValue(labelKey, text)
				continue
			}
		}
		if key == "nodePoolLabelKey" || key == "NodePoolLabelKey" || key == "queueLabelKey" || key == "QueueLabelKey" {
			if text, ok := child.(string); ok {
				out[key] = ctx.h.LabelKey(text)
				continue
			}
		}
		if key == "value" {
			if text, ok := child.(string); ok {
				if kept, include := taintTolerationValue(path, in, text, ctx); include {
					out[key] = kept
				}
				continue
			}
		}
		if key == "secretKeyRef" || key == "secretRef" {
			continue
		}
		if draAttributeValue(path, key) {
			out[key] = child
			continue
		}
		out[key] = walk(child, path+"."+key, ctx)
	}
	return out
}

func stringField(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := object[key].(string); ok {
			return text
		}
	}
	return ""
}

func dropKey(key string) bool {
	switch key {
	case "managedFields", "kubectl.kubernetes.io/last-applied-configuration", "usageDBConfig", "image", "command", "args":
		return true
	default:
		return false
	}
}

func hashNamedMap(in map[string]any, ctx *redactCtx) map[string]any {
	out := make(map[string]any, len(in))
	for key, child := range in {
		out[ctx.h.Hash(key)] = walk(child, "usage", ctx)
	}
	return out
}

// topologyLabelField is a label key stored outside a label map. The topology plugin looks
// levels up by nodeLabel, and a pod affinity topologyKey must match the node label key.
func topologyLabelField(key string, object map[string]any) bool {
	switch key {
	case "nodeLabel", "topologyKey", "requiredTopologyLevel", "preferredTopologyLevel":
		return true
	case "alias":
		_, hasNodeLabel := object["nodeLabel"]
		return hasNodeLabel
	default:
		return false
	}
}

// taintTolerationValue hashes an Equal taint or toleration value with the same label scope as the key.
// An empty operator is Equal. Entries under spec.taints and spec.tolerations are recognized by path,
// so a toleration that also omits effect still hashes. Gt and Lt values stay numeric.
// Other string values are dropped.
func taintTolerationValue(path string, object map[string]any, text string, ctx *redactCtx) (string, bool) {
	op, _ := object["operator"].(string)
	if op == "Gt" || op == "Lt" {
		return text, true
	}
	taintKey, hasKey := object["key"].(string)
	if !hasKey {
		return "", false
	}
	_, hasEffect := object["effect"]
	_, hasOp := object["operator"]
	if !hasEffect && !hasOp && !taintOrTolerationPath(path) {
		return "", false
	}
	if _, numeric := ctx.numericKeys[taintKey]; numeric {
		return text, true
	}
	return ctx.h.LabelValue(taintKey, text), true
}

func taintOrTolerationPath(path string) bool {
	return strings.Contains(path, "spec.taints") || strings.Contains(path, "spec.tolerations")
}

func hashNameList(value any, h Hasher) ([]any, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]any, len(list))
	for i, item := range list {
		text, isText := item.(string)
		if !isText {
			return nil, false
		}
		out[i] = h.Hash(text)
	}
	return out, true
}

func keepReasonCodes(value any) []any {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	var out []any
	for _, item := range list {
		text, isText := item.(string)
		if isText && KeepReasonCode(text) {
			out = append(out, text)
		}
	}
	return out
}

func timestampAnnotation(key string) bool {
	switch key {
	case "kai.scheduler/stale-podgroup-timestamp",
		"kai.scheduler/last-start-timestamp",
		"kai.scheduler/last-eviction-timestamp":
		return true
	default:
		return false
	}
}

func hashAnnotations(value any, h Hasher) map[string]any {
	in, ok := value.(map[string]any)
	if !ok || len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, raw := range in {
		text, _ := raw.(string)
		if timestampAnnotation(key) {
			out[key] = text
			continue
		}
		if !annotationKept(key) {
			continue
		}
		outKey, outValue := h.Label(key, text)
		out[outKey] = outValue
	}
	return out
}

func flatStringMap(value any) bool {
	in, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for _, raw := range in {
		if _, isString := raw.(string); !isString {
			return false
		}
	}
	return true
}

func hashLabelObject(value any, ctx *redactCtx) map[string]any {
	in, ok := value.(map[string]any)
	if !ok || len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, raw := range in {
		text, _ := raw.(string)
		outKey, outValue := ctx.h.Label(key, text)
		if _, numeric := ctx.numericKeys[key]; numeric {
			outValue = text
		}
		if outKey == "" {
			continue
		}
		out[outKey] = outValue
	}
	return out
}

func hashPluginArgs(value any, h Hasher) map[string]any {
	in, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, raw := range in {
		text, isText := raw.(string)
		if !isText {
			out[key] = raw
			continue
		}
		if key == "clusterName" || key == "shardName" {
			out[key] = h.Hash(text)
			continue
		}
		if credentialLike(key) || credentialLike(text) {
			continue
		}
		out[key] = text
	}
	return out
}

func requirement(key string, child any, object map[string]any) (string, []any, bool) {
	if key != "values" {
		return "", nil, false
	}
	values, ok := child.([]any)
	if !ok {
		return "", nil, false
	}
	name, _ := object["key"].(string)
	return name, values, true
}

func requirementKeyName(key string, h Hasher) string {
	if key == "metadata.name" {
		return key
	}
	return h.LabelKey(key)
}

func hashRequirementValues(key, operator string, values []any, h Hasher) []any {
	out := make([]any, len(values))
	for i, value := range values {
		text, ok := value.(string)
		if !ok {
			out[i] = value
			continue
		}
		if operator == "Gt" || operator == "Lt" {
			out[i] = text
			continue
		}
		if key == "metadata.name" {
			out[i] = h.Hash(text)
			continue
		}
		out[i] = h.LabelValue(key, text)
	}
	return out
}

func draAttributeValue(path, key string) bool {
	if !strings.Contains(path, "attribute") {
		return false
	}
	switch key {
	case "string", "stringValue", "version", "bool", "int", "quantity":
		return true
	default:
		return false
	}
}

func reasonCode(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if r == ' ' || r == '"' || r == '/' || r == '\'' {
			return false
		}
	}
	return true
}

func credentialLike(value string) bool {
	lower := strings.ToLower(value)
	for _, needle := range []string{"password", "secret", "token", "apikey", "api_key", "credential"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

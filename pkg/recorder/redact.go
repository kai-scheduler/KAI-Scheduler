// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"encoding/json"
	"strings"
)

// identityFields are JSON keys whose string values are object names.
var identityFields = map[string]struct{}{
	"name":                   {},
	"namespace":              {},
	"uid":                    {},
	"nodeName":               {},
	"node":                   {},
	"queue":                  {},
	"parentQueue":            {},
	"priorityClassName":      {},
	"selectedNode":           {},
	"topology":               {},
	"deviceClassName":        {},
	"pool":                   {},
	"clusterName":            {},
	"shardName":              {},
	"claimName":              {},
	"resourceClaimName":      {},
	"podName":                {},
	"nodePool":               {},
	"requiredTopologyLevel":  {},
	"preferredTopologyLevel": {},
}

// labelMaps are objects whose keys and values are labels or selectors.
var labelMaps = map[string]struct{}{
	"labels":       {},
	"nodeSelector": {},
	"matchLabels":  {},
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
	raw, err := json.Marshal(walk(decoded, "", h))
	if err != nil {
		return err
	}
	env.Record = raw
	return nil
}

func walk(value any, path string, h Hasher) any {
	switch typed := value.(type) {
	case map[string]any:
		return walkMap(typed, path, h)
	case []any:
		for i := range typed {
			typed[i] = walk(typed[i], path, h)
		}
		return typed
	default:
		return value
	}
}

func walkMap(in map[string]any, path string, h Hasher) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, child := range in {
		if dropKey(key) {
			continue
		}
		if key == "annotations" {
			out[key] = hashAnnotations(child, h)
			continue
		}
		if _, labelMap := labelMaps[key]; labelMap {
			if flatStringMap(child) {
				out[key] = hashLabelObject(child, h)
				continue
			}
		}
		if key == "arguments" && strings.HasSuffix(path, "plugins") || (key == "arguments" && strings.Contains(path, "plugin")) {
			out[key] = hashPluginArgs(child, h)
			continue
		}
		if requirementKey, values, ok := requirement(key, child, in); ok && key == "values" {
			out[key] = hashRequirementValues(requirementKey, values, h)
			continue
		}
		if key == "key" {
			if text, ok := child.(string); ok {
				out[key] = requirementKeyName(text, h)
				continue
			}
		}
		if _, ident := identityFields[key]; ident {
			if text, ok := child.(string); ok {
				out[key] = h.Hash(text)
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
				out[key] = h.LabelValue(labelKey, text)
				continue
			}
		}
		if key == "nodePoolLabelKey" || key == "NodePoolLabelKey" || key == "queueLabelKey" || key == "QueueLabelKey" {
			if text, ok := child.(string); ok {
				out[key] = h.LabelKey(text)
				continue
			}
		}
		if key == "value" {
			if _, isString := child.(string); isString {
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
		out[key] = walk(child, path+"."+key, h)
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

func hashAnnotations(value any, h Hasher) map[string]any {
	in, ok := value.(map[string]any)
	if !ok || len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, raw := range in {
		if !annotationKept(key) {
			continue
		}
		text, _ := raw.(string)
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

func hashLabelObject(value any, h Hasher) map[string]any {
	in, ok := value.(map[string]any)
	if !ok || len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, raw := range in {
		text, _ := raw.(string)
		outKey, outValue := h.Label(key, text)
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

func hashRequirementValues(key string, values []any, h Hasher) []any {
	out := make([]any, len(values))
	for i, value := range values {
		text, ok := value.(string)
		if !ok {
			out[i] = value
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

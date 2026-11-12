// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Hasher replaces identifying strings with a keyed hash so the same input always
// produces the same id. The key stays with the caller and is not written to output.
type Hasher struct {
	key []byte
}

func NewHasher(key []byte) Hasher {
	copied := make([]byte, len(key))
	copy(copied, key)
	return Hasher{key: copied}
}

// Hash returns a stable hex id. Empty input stays empty so unset fields stay unset.
// The id is capped at 63 characters, the Kubernetes label-value limit. Replay
// selects nodes and queues with hashed node-pool values, and a longer value
// cannot be used as a selector.
func (h Hasher) Hash(value string) string {
	if value == "" {
		return value
	}
	if len(h.key) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, h.key)
	_, _ = mac.Write([]byte(value))
	sum := hex.EncodeToString(mac.Sum(nil))
	if len(sum) > 63 {
		return sum[:63]
	}
	return sum
}

func (h Hasher) HashEach(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = h.Hash(value)
	}
	return out
}

// keptLabelKeys are non-identifying. The key and the value both stay.
var keptLabelKeys = map[string]struct{}{
	"topology.kubernetes.io/zone":      {},
	"node.kubernetes.io/instance-type": {},
	"gpu-fraction":                     {},
	"gpu-memory":                       {},
}

// identityKeys are scheduling keys whose values are object names. The key stays so
// joins still work; the value is hashed. Both sides of a selector use Label.
var identityKeys = map[string]struct{}{
	"pod-group-name":              {},
	"kai.scheduler/subgroup-name": {},
	"kai.scheduler/queue":         {},
	"kai.scheduler/node-pool":     {},
	"kubernetes.io/hostname":      {},
	"metadata.name":               {},
	"runai-gpu-group":             {},
}

// annotationAllow is the only annotation keys that are recorded.
var annotationAllow = map[string]struct{}{
	"pod-group-name": {},
	"gpu-fraction":   {},
	"gpu-memory":     {},
}

func isIdentityKey(key string) bool {
	if _, found := identityKeys[key]; found {
		return true
	}
	return strings.HasPrefix(key, "runai-gpu-group/")
}

func isKeptLabelKey(key string) bool {
	if _, found := keptLabelKeys[key]; found {
		return true
	}
	return strings.HasPrefix(key, "nvidia.com/gpu.")
}

func annotationKept(key string) bool {
	_, found := annotationAllow[key]
	return found
}

// Label rewrites one selector entry. Kept keys stay plaintext. Identity keys keep
// the key and hash the value. Every other key and value is hashed.
func (h Hasher) Label(key, value string) (string, string) {
	if key == "" {
		return "", ""
	}
	if isKeptLabelKey(key) {
		return key, value
	}
	if isIdentityKey(key) {
		return key, h.Hash(value)
	}
	return h.Hash(key), h.Hash(value)
}

// LabelKey is the key half of Label, used for selector keys stored outside a label map.
func (h Hasher) LabelKey(key string) string {
	out, _ := h.Label(key, "")
	return out
}

// LabelValue is the value half of Label.
func (h Hasher) LabelValue(key, value string) string {
	_, out := h.Label(key, value)
	return out
}

// FreeTextIsName reports whether a string is unsafe to keep as a reason code.
// Strings that look like sentences with object paths are dropped rather than parsed.
func FreeTextIsName(value string) bool {
	if value == "" {
		return false
	}
	if strings.Contains(value, "/") {
		return true
	}
	return strings.Contains(value, " ") && len(value) > 80
}

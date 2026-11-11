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
func (h Hasher) Hash(value string) string {
	if value == "" || len(h.key) == 0 {
		return value
	}
	mac := hmac.New(sha256.New, h.key)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
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

var wellKnownDomains = []string{
	"kubernetes.io",
	"nvidia.com",
	"kai.scheduler",
	"run.ai",
}

// identityKeys are scheduling keys whose values are object names. The key stays so
// joins still work; the value is hashed.
var identityKeys = map[string]struct{}{
	"pod-group-name":              {},
	"kai.scheduler/subgroup-name": {},
	"kai.scheduler/queue":         {},
	"kai.scheduler/node-pool":     {},
	"runai-gpu-group":             {},
}

func isIdentityKey(key string) bool {
	if _, found := identityKeys[key]; found {
		return true
	}
	return strings.HasPrefix(key, "runai-gpu-group/")
}

// WellKnownKey reports keys whose domain is one of the scheduling domains.
// A suffix match keeps keys such as topology.kubernetes.io/zone intact.
func WellKnownKey(key string) bool {
	name := key
	if i := strings.Index(key, "/"); i >= 0 {
		name = key[:i]
	}
	for _, domain := range wellKnownDomains {
		if name == domain || strings.HasSuffix(name, "."+domain) {
			return true
		}
	}
	return false
}

// LabelKey hashes custom label keys and leaves well-known keys unchanged.
func (h Hasher) LabelKey(key string) string {
	if WellKnownKey(key) || isIdentityKey(key) {
		return key
	}
	return h.Hash(key)
}

// LabelValue hashes values that identify objects. Well-known non-identity values stay.
func (h Hasher) LabelValue(key, value string) string {
	if WellKnownKey(key) && !isIdentityKey(key) {
		return value
	}
	return h.Hash(value)
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

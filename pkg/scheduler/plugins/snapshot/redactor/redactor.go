// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
)

// RedactionStats tracks how many fields of each type were redacted.
// Used for CLI reporting and test assertions.
type RedactionStats struct {
	PodsRedacted                   int
	NodesRedacted                  int
	LabelsRedacted                 int
	AnnotationsRedacted            int
	EnvVarsRedacted                int
	SecretsRedacted                int
	ConfigMapsRedacted             int
	VolumesRedacted                int
	AffinityRedacted               int
	PersistentVolumesRedacted      int
	PersistentVolumeClaimsRedacted int
	PriorityClassesRedacted        int
	QueuesRedacted                 int
	PodGroupsRedacted              int
	BindRequestsRedacted           int
	CSICapacitiesRedacted          int
	StorageClassesRedacted         int
	CSIDriversRedacted             int
	ResourceClaimsRedacted         int
	ResourceSlicesRedacted         int
	DeviceClassesRedacted          int
	TopologiesRedacted             int
	NodeSelectorsRedacted          int
	TolerationsRedacted            int
	ProbesRedacted                 int
	CSIStorageCapacitiesRedacted   int
}

// Redactor holds the salt, translation table, and running stats
// for one redaction pass. All methods are safe for concurrent use.
type Redactor struct {
	mu               sync.RWMutex
	salt             string
	translationTable map[string]string
	stats            RedactionStats
}

// NewRedactor creates a ready-to-use Redactor.
// Pass a non-empty salt to make the obfuscated values unguessable
// even when the original names are known.
func NewRedactor(salt string) *Redactor {
	return &Redactor{
		salt:             salt,
		translationTable: make(map[string]string),
	}
}

// Obfuscate returns a deterministic, prefixed SHA-256-based hash of value.
//
// Determinism is the key property here: the same value + prefix + salt
// always produces the same output. This is what keeps cross-resource
// references consistent after redaction. For example, a pod's NodeName
// and the node's own Name both call Obfuscate(name, "node"), so they
// always resolve to the same obfuscated string.
//
// Empty and whitespace-only strings are returned as-is because many
// optional Kubernetes fields are legitimately empty and should not
// be replaced with a hash.
func (r *Redactor) Obfuscate(value, prefix string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	h := sha256.New()
	h.Write([]byte(prefix + ":" + value + r.salt))
	sum := h.Sum(nil)

	obfuscated := fmt.Sprintf("%s-%x", prefix, sum[:8])
	r.translationTable[obfuscated] = value
	return obfuscated
}

// GetTranslationTable returns a defensive copy of the full obfuscated to original
// mapping so callers can save it alongside the redacted snapshot.
func (r *Redactor) GetTranslationTable() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]string, len(r.translationTable))
	for k, v := range r.translationTable {
		out[k] = v
	}
	return out
}

// GetStats returns a point-in-time copy of the redaction statistics.
func (r *Redactor) GetStats() RedactionStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stats
}

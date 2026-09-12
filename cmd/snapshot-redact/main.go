// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot/redactor"
)

// RedactionReport is written to disk when --report is given.
// It gives the recipient a summary without requiring them to diff the files.
type RedactionReport struct {
	SourceFile           string                  `json:"source_file"`
	OutputFile           string                  `json:"output_file"`
	TranslationTableFile string                  `json:"translation_table_file"`
	Success              bool                    `json:"success"`
	Stats                redactor.RedactionStats `json:"stats"`
	TranslationTableSize int                     `json:"translation_table_size"`
	Message              string                  `json:"message"`
}

func main() {
	inputZip := flag.String("in", "", "Path to the input snapshot zip file (REQUIRED)")
	outputZip := flag.String("out", "redacted-snapshot.zip", "Path to save the redacted snapshot zip")
	translationFile := flag.String("table", "translation-table.json", "Path to save the translation table JSON")
	reportFile := flag.String("report", "", "Path to save the redaction report (optional)")
	dryRun := flag.Bool("dry-run", false, "Preview redaction without writing any files")
	force := flag.Bool("force", false, "Overwrite output files if they already exist")
	verbose := flag.Bool("v", false, "Print detailed progress to stdout")
	salt := flag.String("salt", "", "Secret salt to improve obfuscation privacy")

	flag.Parse()

	if *inputZip == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --in flag is required")
		flag.Usage()
		os.Exit(1)
	}

	if err := validateInputFile(*inputZip); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	if !*dryRun {
		if err := validateOutputFiles(*outputZip, *translationFile, *reportFile, *force); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
	}

	logf(*verbose, "Loading snapshot from: %s", *inputZip)

	snap, err := loadSnapshot(*inputZip)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to load snapshot: %v\n", err)
		os.Exit(1)
	}

	if snap == nil || snap.RawObjects == nil {
		fmt.Fprintln(os.Stderr, "ERROR: Snapshot is nil or contains no raw objects")
		os.Exit(1)
	}

	logf(*verbose, "Snapshot loaded — pods: %d  nodes: %d  configmaps: %d",
		len(snap.RawObjects.Pods),
		len(snap.RawObjects.Nodes),
		len(snap.RawObjects.ConfigMaps),
	)

	r := redactor.NewRedactor(*salt)
	if err := r.RedactSnapshot(snap); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Redaction failed: %v\n", err)
		os.Exit(1)
	}

	stats := r.GetStats()
	table := r.GetTranslationTable()

	if *verbose {
		printStats(stats, len(table))
	}

	if *dryRun {
		fmt.Println("\n=== DRY RUN — no files written ===")
		fmt.Printf("Would create: %s\n", *outputZip)
		fmt.Printf("Would create: %s\n", *translationFile)
		if *reportFile != "" {
			fmt.Printf("Would create: %s\n", *reportFile)
		}
		printSummary(stats, len(table))
		return
	}

	if err := saveSnapshot(*outputZip, snap); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to save redacted snapshot: %v\n", err)
		os.Exit(1)
	}
	logf(*verbose, "Redacted snapshot saved to: %s", *outputZip)

	if err := saveTranslationTable(*translationFile, table); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to save translation table: %v\n", err)
		os.Exit(1)
	}
	logf(*verbose, "Translation table saved to: %s", *translationFile)

	if *reportFile != "" {
		report := RedactionReport{
			SourceFile:           *inputZip,
			OutputFile:           *outputZip,
			TranslationTableFile: *translationFile,
			Success:              true,
			Stats:                stats,
			TranslationTableSize: len(table),
			Message:              "Redaction completed successfully",
		}
		if err := saveReport(*reportFile, report); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to save report: %v\n", err)
		} else {
			logf(*verbose, "Report saved to: %s", *reportFile)
		}
	}

	fmt.Printf("✓ Redacted snapshot : %s\n", *outputZip)
	fmt.Printf("✓ Translation table : %s\n", *translationFile)
	if *reportFile != "" {
		fmt.Printf("✓ Report            : %s\n", *reportFile)
	}
	printSummary(stats, len(table))
}

// validateInputFile checks that the file exists, is not a directory,
// and can be opened as a valid zip archive.
func validateInputFile(filename string) error {
	info, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", filename)
		}
		return fmt.Errorf("cannot access file: %v", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", filename)
	}
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return fmt.Errorf("file is not a valid zip archive: %v", err)
	}
	zr.Close()
	return nil
}

// validateOutputFiles ensures none of the output paths already exist unless
// --force is set, and that their parent directories are accessible.
func validateOutputFiles(outZip, tableFile, reportFile string, force bool) error {
	paths := []string{outZip, tableFile}
	if reportFile != "" {
		paths = append(paths, reportFile)
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil && !force {
			return fmt.Errorf("output file already exists: %s (use --force to overwrite)", p)
		}
		dir := filepath.Dir(p)
		if dir == "" {
			dir = "."
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("output directory is not accessible: %s", dir)
		}
	}
	return nil
}

// loadSnapshot opens the zip, finds snapshot.json, and decodes it.
func loadSnapshot(filename string) (*snapshot.Snapshot, error) {
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name != snapshot.SnapshotFileName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open %s in zip: %v", snapshot.SnapshotFileName, err)
		}
		defer rc.Close()

		var snap snapshot.Snapshot
		dec := json.NewDecoder(rc)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&snap); err != nil {
			return nil, fmt.Errorf("failed to decode snapshot JSON: %v", err)
		}
		if snap.RawObjects == nil {
			return nil, errors.New("snapshot contains no raw objects")
		}
		return &snap, nil
	}

	return nil, fmt.Errorf("%s not found in zip", snapshot.SnapshotFileName)
}

// saveSnapshot writes the redacted snapshot to a zip using an atomic
// write-then-rename so a partial failure never corrupts the output file.
func saveSnapshot(filename string, snap *snapshot.Snapshot) error {
	if snap == nil {
		return errors.New("snapshot is nil")
	}

	tmp := filename + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	zw := zip.NewWriter(f)
	fw, err := zw.Create(snapshot.SnapshotFileName)
	if err != nil {
		zw.Close()
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to create entry in zip: %v", err)
	}

	enc := json.NewEncoder(fw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(snap); err != nil {
		zw.Close()
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to encode snapshot: %v", err)
	}

	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to close zip writer: %v", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to close file: %v", err)
	}

	if err := os.Rename(tmp, filename); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to rename temp file: %v", err)
	}

	return nil
}

// saveTranslationTable serialises the table as indented JSON with
// permissions 0600 so the sensitive mapping is not world-readable.
func saveTranslationTable(filename string, table map[string]string) error {
	if table == nil {
		return errors.New("translation table is nil")
	}

	data, err := json.MarshalIndent(table, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal translation table: %v", err)
	}

	tmp := filename + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("failed to write translation table: %v", err)
	}

	if err := os.Rename(tmp, filename); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to rename translation table: %v", err)
	}

	return nil
}

// saveReport writes the JSON report with standard 0644 permissions.
func saveReport(filename string, report RedactionReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %v", err)
	}

	tmp := filename + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("failed to write report: %v", err)
	}

	if err := os.Rename(tmp, filename); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to rename report: %v", err)
	}

	return nil
}

// logf prints a formatted message only when verbose mode is enabled.
func logf(verbose bool, format string, args ...any) {
	if verbose {
		fmt.Printf(format+"\n", args...)
	}
}

// printStats prints the per-field counts when verbose is on.
func printStats(s redactor.RedactionStats, tableSize int) {
	fmt.Println("--- Redaction details ---")
	fmt.Printf("  Pods              : %d\n", s.PodsRedacted)
	fmt.Printf("  Nodes             : %d\n", s.NodesRedacted)
	fmt.Printf("  Labels            : %d\n", s.LabelsRedacted)
	fmt.Printf("  Annotations       : %d\n", s.AnnotationsRedacted)
	fmt.Printf("  Env vars          : %d\n", s.EnvVarsRedacted)
	fmt.Printf("  Secrets           : %d\n", s.SecretsRedacted)
	fmt.Printf("  ConfigMaps        : %d\n", s.ConfigMapsRedacted)
	fmt.Printf("  Volumes           : %d\n", s.VolumesRedacted)
	fmt.Printf("  Probes            : %d\n", s.ProbesRedacted)
	fmt.Printf("  Affinity rules    : %d\n", s.AffinityRedacted)
	fmt.Printf("  Translation table : %d entries\n", tableSize)
}

// printSummary always prints, regardless of verbose flag.
func printSummary(s redactor.RedactionStats, tableSize int) {
	fmt.Println("\n=== REDACTION SUMMARY ===")
	fmt.Printf("Pods: %d  Nodes: %d  Labels: %d  Annotations: %d\n",
		s.PodsRedacted, s.NodesRedacted, s.LabelsRedacted, s.AnnotationsRedacted)
	fmt.Printf("Env vars: %d  Secrets: %d  ConfigMaps: %d  Probes: %d\n",
		s.EnvVarsRedacted, s.SecretsRedacted, s.ConfigMapsRedacted, s.ProbesRedacted)
	fmt.Printf("Queues: %d  PodGroups: %d  BindRequests: %d\n",
		s.QueuesRedacted, s.PodGroupsRedacted, s.BindRequestsRedacted)
	fmt.Printf("PVs: %d  PVCs: %d  StorageClasses: %d\n",
		s.PersistentVolumesRedacted, s.PersistentVolumeClaimsRedacted, s.StorageClassesRedacted)
	fmt.Printf("Translation table entries: %d\n", tableSize)
}

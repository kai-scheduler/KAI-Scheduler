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

// RedactionReport contains summary of redaction operation
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
	outputZip := flag.String("out", "", "Path to save the redacted snapshot zip (default: redacted-snapshot.zip)")
	translationFile := flag.String("table", "", "Path to save the translation table JSON (default: translation-table.json)")
	reportFile := flag.String("report", "", "Path to save the redaction report (optional)")
	dryRun := flag.Bool("dry-run", false, "Preview what will be redacted without writing files")
	force := flag.Bool("force", false, "Overwrite output files if they exist")
	verbose := flag.Bool("v", false, "Verbose output")

	flag.Parse()

	// Set defaults
	if *outputZip == "" {
		*outputZip = "redacted-snapshot.zip"
	}
	if *translationFile == "" {
		*translationFile = "translation-table.json"
	}

	// Validate input
	if *inputZip == "" {
		fmt.Fprintf(os.Stderr, "ERROR: --in flag is required\n")
		flag.Usage()
		os.Exit(1)
	}

	if err := validateInputFile(*inputZip); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Input file validation failed: %v\n", err)
		os.Exit(1)
	}

	if !*dryRun {
		if err := validateOutputFiles(*outputZip, *translationFile, *force); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Output file validation failed: %v\n", err)
			os.Exit(1)
		}
	}

	if *verbose {
		fmt.Printf("Loading snapshot from: %s\n", *inputZip)
	}

	// Load the snapshot
	snap, err := loadSnapshot(*inputZip)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to load snapshot: %v\n", err)
		os.Exit(1)
	}

	if snap == nil || snap.RawObjects == nil {
		fmt.Fprintf(os.Stderr, "ERROR: Snapshot is nil or empty\n")
		os.Exit(1)
	}

	if *verbose {
		fmt.Printf("Snapshot loaded successfully\n")
		fmt.Printf("  Pods: %d\n", len(snap.RawObjects.Pods))
		fmt.Printf("  Nodes: %d\n", len(snap.RawObjects.Nodes))
		fmt.Printf("  ConfigMaps: %d\n", len(snap.RawObjects.ConfigMaps))
	}

	// Run redaction
	if *verbose {
		fmt.Println("Starting redaction process...")
	}

	r := redactor.NewRedactor()
	if err := r.RedactSnapshot(snap); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Redaction failed: %v\n", err)
		os.Exit(1)
	}

	stats := r.GetStats()
	translationTable := r.GetTranslationTable()

	if *verbose {
		fmt.Println("Redaction completed successfully")
		fmt.Printf("  Pods redacted: %d\n", stats.PodsRedacted)
		fmt.Printf("  Nodes redacted: %d\n", stats.NodesRedacted)
		fmt.Printf("  Labels/Annotations redacted: %d\n", stats.LabelsRedacted)
		fmt.Printf("  Env vars redacted: %d\n", stats.EnvVarsRedacted)
		fmt.Printf("  Secrets redacted: %d\n", stats.SecretsRedacted)
		fmt.Printf("  ConfigMaps redacted: %d\n", stats.ConfigMapsRedacted)
		fmt.Printf("  Translation table entries: %d\n", len(translationTable))
	}

	if *dryRun {
		fmt.Println("\n=== DRY RUN MODE ===")
		fmt.Println("Files were NOT written. Remove --dry-run flag to actually create files.")
		fmt.Printf("\nWould have created:\n")
		fmt.Printf("  - Redacted snapshot: %s\n", *outputZip)
		fmt.Printf("  - Translation table: %s\n", *translationFile)
		printRedactionSummary(stats, len(translationTable))
		return
	}

	// Save redacted snapshot
	if *verbose {
		fmt.Printf("Saving redacted snapshot to: %s\n", *outputZip)
	}
	if err := saveSnapshot(*outputZip, snap); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to save redacted snapshot: %v\n", err)
		os.Exit(1)
	}

	// Save translation table
	if *verbose {
		fmt.Printf("Saving translation table to: %s\n", *translationFile)
	}
	if err := saveTranslationTable(*translationFile, translationTable); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to save translation table: %v\n", err)
		os.Exit(1)
	}

	// Generate report if requested
	if *reportFile != "" {
		report := RedactionReport{
			SourceFile:           *inputZip,
			OutputFile:           *outputZip,
			TranslationTableFile: *translationFile,
			Success:              true,
			Stats:                stats,
			TranslationTableSize: len(translationTable),
			Message:              "Redaction completed successfully",
		}
		if err := saveReport(*reportFile, report); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to save report: %v\n", err)
		} else if *verbose {
			fmt.Printf("Redaction report saved to: %s\n", *reportFile)
		}
	}

	fmt.Printf("\n✓ Successfully created redacted snapshot: %s\n", *outputZip)
	fmt.Printf("✓ Successfully created translation table: %s\n", *translationFile)
	printRedactionSummary(stats, len(translationTable))
}

// validateInputFile checks if input file exists and is readable
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

	// Try to open as zip
	_, err = zip.OpenReader(filename)
	if err != nil {
		return fmt.Errorf("file is not a valid zip: %v", err)
	}

	return nil
}

// validateOutputFiles checks if output files can be created
func validateOutputFiles(outZip, tableFile string, force bool) error {
	// Check if files already exist
	if _, err := os.Stat(outZip); err == nil && !force {
		return fmt.Errorf("output file already exists: %s (use --force to overwrite)", outZip)
	}

	if _, err := os.Stat(tableFile); err == nil && !force {
		return fmt.Errorf("translation table file already exists: %s (use --force to overwrite)", tableFile)
	}

	// Check if directories are writable
	outDir := filepath.Dir(outZip)
	if outDir == "" {
		outDir = "."
	}
	if info, err := os.Stat(outDir); err != nil || !info.IsDir() {
		return fmt.Errorf("output directory is not accessible: %s", outDir)
	}

	tableDir := filepath.Dir(tableFile)
	if tableDir == "" {
		tableDir = "."
	}
	if info, err := os.Stat(tableDir); err != nil || !info.IsDir() {
		return fmt.Errorf("table directory is not accessible: %s", tableDir)
	}

	return nil
}

func loadSnapshot(filename string) (*snapshot.Snapshot, error) {
	zipFile, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer zipFile.Close()

	for _, file := range zipFile.File {
		if file.Name == snapshot.SnapshotFileName {
			jsonFile, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open snapshot.json in zip: %v", err)
			}
			defer jsonFile.Close()

			var snap snapshot.Snapshot
			decoder := json.NewDecoder(jsonFile)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&snap); err != nil {
				return nil, fmt.Errorf("failed to decode snapshot JSON: %v", err)
			}

			if snap.RawObjects == nil {
				return nil, errors.New("snapshot contains no raw objects")
			}

			return &snap, nil
		}
	}

	return nil, fmt.Errorf("snapshot.json not found in zip file")
}

func saveSnapshot(filename string, snap *snapshot.Snapshot) error {
	if snap == nil {
		return errors.New("snapshot is nil")
	}

	// Create temporary file first to avoid partial writes
	tmpFile := filename + ".tmp"
	outFile, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %v", err)
	}
	defer outFile.Close()

	zipWriter := zip.NewWriter(outFile)
	defer zipWriter.Close()

	fileWriter, err := zipWriter.Create(snapshot.SnapshotFileName)
	if err != nil {
		zipWriter.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("failed to create file in zip: %v", err)
	}

	encoder := json.NewEncoder(fileWriter)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(snap); err != nil {
		zipWriter.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("failed to encode snapshot: %v", err)
	}

	if err := zipWriter.Close(); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to close zip writer: %v", err)
	}

	if err := outFile.Close(); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to close output file: %v", err)
	}

	// Atomic rename
	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename file: %v", err)
	}

	return nil
}

func saveTranslationTable(filename string, table map[string]string) error {
	if table == nil {
		return errors.New("translation table is nil")
	}

	tmpFile := filename + ".tmp"
	data, err := json.MarshalIndent(table, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal translation table: %v", err)
	}

	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write translation table: %v", err)
	}

	// Atomic rename
	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename translation table file: %v", err)
	}

	return nil
}

func saveReport(filename string, report RedactionReport) error {
	tmpFile := filename + ".tmp"
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %v", err)
	}

	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write report: %v", err)
	}

	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename report file: %v", err)
	}

	return nil
}

func printRedactionSummary(stats redactor.RedactionStats, tableSize int) {
	fmt.Println("\n=== REDACTION SUMMARY ===")
	fmt.Printf("Resources Redacted:\n")
	fmt.Printf("  Pods: %d\n", stats.PodsRedacted)
	fmt.Printf("  Nodes: %d\n", stats.NodesRedacted)
	fmt.Printf("  Labels/Annotations: %d\n", stats.LabelsRedacted)
	fmt.Printf("  Environment Variables: %d\n", stats.EnvVarsRedacted)
	fmt.Printf("  Secrets: %d\n", stats.SecretsRedacted)
	fmt.Printf("  ConfigMaps: %d\n", stats.ConfigMapsRedacted)
	fmt.Printf("  Volumes: %d\n", stats.VolumesRedacted)
	fmt.Printf("  Affinity Rules: %d\n", stats.Affinity)
	fmt.Printf("\nTranslation Table Entries: %d\n", tableSize)
}

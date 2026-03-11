// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/go-logr/logr"
	"github.com/go-logr/stdr"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	env_tests "github.com/kai-scheduler/KAI-scheduler/pkg/env-tests"
	"github.com/kai-scheduler/KAI-scheduler/pkg/env-tests/timeaware"
)

var (
	inputFile            = flag.String("input", "examples/example_config.yaml", "Path to input YAML configuration file (use '-' for stdin)")
	outputFile           = flag.String("output", "simulation_results.csv", "Path to output CSV file (use '-' for stdout, default: simulation_results.csv)")
	enableControllerLogs = flag.Bool("enable-controller-logs", false, "Enable controller logs")
)

func main() {
	flag.Parse()

	if *enableControllerLogs {
		ctrllog.SetLogger(stdr.New(log.Default()))
	} else {
		ctrllog.SetLogger(logr.Discard())
	}

	if err := run(*inputFile, *outputFile); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run(inputFile, outputFile string) error {
	ctx := context.Background()

	simulation, err := readConfig(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read configuration: %w", err)
	}
	simulation.SetDefaults()

	// Set up test environment
	log.Println("Setting up test environment...")
	cfg, ctrlClient, testEnv, err := env_tests.SetupEnvTest(nil)
	if err != nil {
		return fmt.Errorf("failed to setup test environment: %w", err)
	}
	defer func() {
		log.Println("Shutting down test environment...")
		if err := testEnv.Stop(); err != nil {
			log.Printf("Error stopping test environment: %v", err)
		}
	}()

	log.Println("Running simulation...")
	allocationHistory, err, cleanupErr := timeaware.RunSimulation(ctx, ctrlClient, cfg, *simulation)
	if err != nil {
		return fmt.Errorf("failed to run simulation: %w", err)
	}
	if cleanupErr != nil {
		log.Printf("Warning: cleanup error: %v", cleanupErr)
	}

	log.Println("Simulation completed successfully")

	// Write output
	if err := writeOutput(outputFile, allocationHistory.ToCSV()); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}

// readConfig reads the configuration from a file or stdin.
func readConfig(inputFile string) (*timeaware.TimeAwareSimulation, error) {
	var data []byte
	var err error
	if inputFile == "-" {
		log.Println("Reading configuration from stdin...")
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read configuration from stdin: %w", err)
		}
	} else {
		log.Printf("Reading configuration from file: %s", inputFile)
		data, err = os.ReadFile(inputFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read configuration from file: %w", err)
		}
	}

	var simulation timeaware.TimeAwareSimulation
	if err := yaml.Unmarshal(data, &simulation); err != nil {
		return nil, fmt.Errorf("failed to parse YAML configuration: %w", err)
	}
	return &simulation, nil
}

// writeOutput writes the CSV data to a file or stdout.
func writeOutput(outputFile, csvData string) error {
	if outputFile == "-" {
		log.Println("Writing results to stdout...")
		fmt.Print(csvData)
		return nil
	}

	log.Printf("Writing results to file: %s", outputFile)
	if err := os.WriteFile(outputFile, []byte(csvData), 0644); err != nil {
		return err
	}
	log.Printf("Results written to: %s", outputFile)
	return nil
}

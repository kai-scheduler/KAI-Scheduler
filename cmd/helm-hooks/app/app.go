// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap/zapcore"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/helmhooks"
)

const (
	applyCRDsCommand         = "apply-crds"
	applyConfigCommand       = "apply-config"
	migrateTopologiesCommand = "migrate-topologies"
	cleanupCommand           = "cleanup"
)

// clientFactory defers connecting to the cluster until the subcommand arguments are valid.
type clientFactory func() (client.Client, error)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(kaiv1.AddToScheme(scheme))
	utilruntime.Must(kaiv1alpha1.AddToScheme(scheme))
}

// Run executes the subcommand named by args[0] with the remaining args as its flags.
func Run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return errors.New("subcommand required")
	}
	command, flags := args[0], args[1:]

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{TimeEncoder: zapcore.ISO8601TimeEncoder})))
	ctx := ctrl.SetupSignalHandler()

	switch command {
	case applyCRDsCommand:
		return applyCRDs(ctx, newClusterClient, flags)
	case applyConfigCommand:
		return applyConfig(ctx, newClusterClient, flags)
	case migrateTopologiesCommand:
		return migrateTopologies(ctx, newClusterClient, flags)
	case cleanupCommand:
		return cleanup(ctx, newClusterClient, flags)
	default:
		printUsage()
		return fmt.Errorf("unknown subcommand %q", command)
	}
}

func newClusterClient() (client.Client, error) {
	c, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	return c, nil
}

func applyCRDs(ctx context.Context, newClient clientFactory, flags []string) error {
	if err := parseFlags(flag.NewFlagSet(applyCRDsCommand, flag.ContinueOnError), flags); err != nil {
		return err
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	return helmhooks.ApplyCRDs(ctx, c)
}

func applyConfig(ctx context.Context, newClient clientFactory, flags []string) error {
	fs := flag.NewFlagSet(applyConfigCommand, flag.ContinueOnError)
	file := fs.String("file", "", "path to the Config manifest to apply")
	if err := parseFlags(fs, flags); err != nil {
		return err
	}
	if *file == "" {
		return missingFlagError(applyConfigCommand, "file")
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	return helmhooks.ApplyConfig(ctx, c, *file)
}

func migrateTopologies(ctx context.Context, newClient clientFactory, flags []string) error {
	if err := parseFlags(flag.NewFlagSet(migrateTopologiesCommand, flag.ContinueOnError), flags); err != nil {
		return err
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	return helmhooks.MigrateTopologies(ctx, c)
}

func cleanup(ctx context.Context, newClient clientFactory, flags []string) error {
	fs := flag.NewFlagSet(cleanupCommand, flag.ContinueOnError)
	namespace := fs.String("namespace", "", "namespace holding the operator-managed deployments")
	deleteConfig := fs.String("delete-config", "", "name of the Config to delete; skipped when empty")
	if err := parseFlags(fs, flags); err != nil {
		return err
	}
	if *namespace == "" {
		return missingFlagError(cleanupCommand, "namespace")
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	return helmhooks.Cleanup(ctx, c, *namespace, *deleteConfig)
}

// parseFlags also rejects leftover positional arguments, which the flag package otherwise ignores.
func parseFlags(fs *flag.FlagSet, flags []string) error {
	if err := fs.Parse(flags); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%s: unexpected arguments %q", fs.Name(), fs.Args())
	}
	return nil
}

func missingFlagError(command, flagName string) error {
	return fmt.Errorf("%s: --%s is required", command, flagName)
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: helm-hooks <subcommand> [flags]

Subcommands:
  %s                      server-side apply the KAI CRDs bundled in this binary
  %s --file=<path>      server-side apply the Config manifest at <path>
  %s              copy Kueue Topologies into KAI Topologies
  %s --namespace=<ns> [--delete-config=<name>]
                                  delete operator-managed deployments and optionally a Config
`, applyCRDsCommand, applyConfigCommand, migrateTopologiesCommand, cleanupCommand)
}

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

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(kaiv1.AddToScheme(scheme))
	utilruntime.Must(kaiv1alpha1.AddToScheme(scheme))
}

// Run executes the hook subcommand named by args[0] with the remaining args as its flags.
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
		return runWithClient(ctx, command, flags, helmhooks.ApplyCRDs)
	case applyConfigCommand:
		fs := flag.NewFlagSet(command, flag.ContinueOnError)
		file := fs.String("file", "", "path to the Config manifest to apply")
		if err := fs.Parse(flags); err != nil {
			return err
		}
		if *file == "" {
			return fmt.Errorf("%s: --file is required", command)
		}
		return runWithClient(ctx, command, nil, func(ctx context.Context, c client.Client) error {
			return helmhooks.ApplyConfig(ctx, c, *file)
		})
	case migrateTopologiesCommand:
		return runWithClient(ctx, command, flags, helmhooks.MigrateTopologies)
	case cleanupCommand:
		fs := flag.NewFlagSet(command, flag.ContinueOnError)
		namespace := fs.String("namespace", "", "namespace holding the operator-managed deployments")
		deleteConfig := fs.String("delete-config", "", "name of the Config to delete; skipped when empty")
		if err := fs.Parse(flags); err != nil {
			return err
		}
		if *namespace == "" {
			return fmt.Errorf("%s: --namespace is required", command)
		}
		return runWithClient(ctx, command, nil, func(ctx context.Context, c client.Client) error {
			return helmhooks.Cleanup(ctx, c, *namespace, *deleteConfig)
		})
	default:
		printUsage()
		return fmt.Errorf("unknown subcommand %q", command)
	}
}

func runWithClient(ctx context.Context, command string, flags []string,
	hook func(context.Context, client.Client) error) error {
	if err := flag.NewFlagSet(command, flag.ContinueOnError).Parse(flags); err != nil {
		return err
	}
	c, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	return hook(ctx, c)
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

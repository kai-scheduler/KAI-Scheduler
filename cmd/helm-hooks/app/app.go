// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

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

// hook is a chart hook ready to run against the cluster.
type hook func(context.Context, client.Client) error

type subcommand struct {
	name        string
	flagsUsage  string
	description string
	// parse validates the subcommand arguments and returns the hook to run.
	parse func(name string, args []string) (hook, error)
}

var subcommands = []subcommand{
	{
		name:        "apply-crds",
		description: "server-side apply the KAI CRDs bundled in this binary",
		parse:       parseApplyCRDs,
	},
	{
		name:        "apply-config",
		flagsUsage:  "--file=<path>",
		description: "server-side apply the Config manifest at <path>",
		parse:       parseApplyConfig,
	},
	{
		name:        "migrate-topologies",
		description: "copy Kueue Topologies into KAI Topologies",
		parse:       parseMigrateTopologies,
	},
	{
		name:        "cleanup",
		flagsUsage:  "--namespace=<ns> [--delete-config=<name>]",
		description: "delete operator-managed deployments and optionally a Config",
		parse:       parseCleanup,
	},
}

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(kaiv1.AddToScheme(scheme))
	utilruntime.Must(kaiv1alpha1.AddToScheme(scheme))
}

// Run executes the subcommand named by args[0] with the remaining args as its flags.
func Run(args []string) error {
	run, err := parseArgs(args)
	if err != nil {
		return err
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{TimeEncoder: zapcore.ISO8601TimeEncoder})))
	c, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	return run(ctrl.SetupSignalHandler(), c)
}

func parseArgs(args []string) (hook, error) {
	if len(args) == 0 {
		printUsage()
		return nil, errors.New("subcommand required")
	}
	for _, sub := range subcommands {
		if sub.name == args[0] {
			return sub.parse(sub.name, args[1:])
		}
	}
	printUsage()
	return nil, fmt.Errorf("unknown subcommand %q", args[0])
}

func parseApplyCRDs(name string, args []string) (hook, error) {
	return parseWithoutFlags(name, args, helmhooks.ApplyCRDs)
}

func parseMigrateTopologies(name string, args []string) (hook, error) {
	return parseWithoutFlags(name, args, helmhooks.MigrateTopologies)
}

func parseApplyConfig(name string, args []string) (hook, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	file := fs.String("file", "", "path to the Config manifest to apply")
	if err := parseFlags(fs, args); err != nil {
		return nil, err
	}
	if *file == "" {
		return nil, missingFlagError(name, "file")
	}
	return func(ctx context.Context, c client.Client) error {
		return helmhooks.ApplyConfig(ctx, c, *file)
	}, nil
}

func parseCleanup(name string, args []string) (hook, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	namespace := fs.String("namespace", "", "namespace holding the operator-managed deployments")
	deleteConfig := fs.String("delete-config", "", "name of the Config to delete; skipped when empty")
	if err := parseFlags(fs, args); err != nil {
		return nil, err
	}
	if *namespace == "" {
		return nil, missingFlagError(name, "namespace")
	}
	return func(ctx context.Context, c client.Client) error {
		return helmhooks.Cleanup(ctx, c, *namespace, *deleteConfig)
	}, nil
}

func parseWithoutFlags(name string, args []string, run hook) (hook, error) {
	if err := parseFlags(flag.NewFlagSet(name, flag.ContinueOnError), args); err != nil {
		return nil, err
	}
	return run, nil
}

// parseFlags rejects leftover positional arguments, which the flag package otherwise ignores.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
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
	w := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Usage: helm-hooks <subcommand> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subcommands:")
	for _, sub := range subcommands {
		fmt.Fprintf(w, "  %s %s\t%s\n", sub.name, sub.flagsUsage, sub.description)
	}
	_ = w.Flush()
}

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rh-ecosystem-edge/nvidia-ci/internal/manifests"
)

func parseOptions(args, environment []string) (manifests.Options, error) {
	options := manifests.Options{Variables: make(map[string]string)}
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			options.Variables[name] = value
		}
	}
	flags := flag.NewFlagSet("apply-manifests", flag.ContinueOnError)
	flags.StringVar(&options.Action, "action", "apply", "apply or delete")
	flags.StringVar(&options.Directory, "manifest-dir", "manifests/nno", "directory of YAML manifests")
	flags.StringVar(&options.Kubeconfig, "kubeconfig", "", "required kubeconfig path")
	flags.StringVar(&options.PoolName, "worker-mcp-name", "worker", "worker MachineConfigPool name")
	flags.StringVar(&options.VariableFilter, "manifest-vars", "", "template variables to substitute, for example '$FOO $BAR'")
	flags.BoolVar(&options.WaitForMCP, "wait-for-worker-mcp", false, "wait for worker rollouts")
	flags.BoolVar(&options.BatchMCP, "batch-worker-mcp", false, "batch independent new MachineConfigs")
	flags.DurationVar(&options.StartTimeout, "worker-mcp-rollout-start-timeout", 2*time.Minute, "rollout start timeout")
	flags.DurationVar(&options.RolloutTimeout, "worker-mcp-rollout-timeout", 40*time.Minute, "rollout completion timeout")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	return options, nil
}

func main() {
	options, err := parseOptions(os.Args[1:], os.Environ())
	if err == nil {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = manifests.Run(ctx, options)
	}
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

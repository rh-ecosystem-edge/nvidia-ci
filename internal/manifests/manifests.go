package manifests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const (
	machineConfigKind = "MachineConfig"
	actionApply       = "apply"
	actionDelete      = "delete"
	commandCreate     = "create"
	commandPatch      = "patch"
)

// Options supplies rollout settings and template values without environment lookups.
type Options struct {
	Action, Directory, Kubeconfig, PoolName string
	VariableFilter                          string
	Variables                               map[string]string
	WaitForMCP, BatchMCP                    bool
	StartTimeout, RolloutTimeout            time.Duration
}

type commandRunner func(context.Context, []byte, []string, string, ...string) ([]byte, error)

type runner struct {
	options Options
	command commandRunner
}

type batch struct {
	resources      []unstructured.Unstructured
	machineConfigs []string
}

// Run applies or deletes the manifests described by options.
func Run(ctx context.Context, options Options) error {
	return (runner{options: options, command: executeCommand}).run(ctx)
}

func executeCommand(ctx context.Context, input []byte, environment []string, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = environment
	command.Stdin = bytes.NewReader(input)
	command.Stderr = os.Stderr
	output, err := command.Output()
	if err != nil {
		return output, fmt.Errorf("%s failed: %w", name, errors.Join(ctx.Err(), err))
	}

	return output, nil
}

func (r runner) oc(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	return r.command(ctx, input, nil, "oc", append(args, "--kubeconfig="+r.options.Kubeconfig)...)
}

func (r runner) run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	files, err := findManifests(r.options.Directory)
	if err != nil {
		return err
	}
	if r.options.Action == actionDelete {
		slices.Reverse(files)
		return r.changeFiles(ctx, files)
	}
	if err := checkRequiredVariables(files, r.options.Variables); err != nil {
		return err
	}
	prepared, err := r.prepareBatch(ctx, files)
	if err != nil {
		return err
	}
	if prepared != nil {
		return r.applyBatch(ctx, prepared)
	}

	return r.changeFiles(ctx, files)
}

func (r runner) validate() error {
	if r.options.Action != actionApply && r.options.Action != actionDelete {
		return fmt.Errorf("action must be apply or delete")
	}
	if r.options.Kubeconfig == "" {
		return fmt.Errorf("kubeconfig must be supplied explicitly")
	}
	if r.options.Action == actionApply && r.options.WaitForMCP &&
		(r.options.PoolName == "" || r.options.StartTimeout <= 0 || r.options.RolloutTimeout <= 0) {
		return fmt.Errorf("pool name and positive rollout timeouts are required")
	}

	return nil
}

func findManifests(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && (filepath.Ext(entry.Name()) == ".yaml" || filepath.Ext(entry.Name()) == ".yml") {
			files = append(files, filepath.Join(directory, entry.Name()))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no YAML manifests found in %s", directory)
	}

	return files, nil
}

func sibling(path, extension string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + extension
}

func checkRequiredVariables(files []string, variables map[string]string) error {
	for _, path := range files {
		data, err := os.ReadFile(sibling(path, ".env-required"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			name := strings.TrimSpace(line)
			if name != "" && !strings.HasPrefix(name, "#") && variables[name] == "" {
				return fmt.Errorf("missing required variable %s for %s", name, path)
			}
		}
	}

	return nil
}

func renderManifest(ctx context.Context, path string, variables map[string]string, filter string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	environment := make([]string, 0, len(variables))
	for name, value := range variables {
		environment = append(environment, name+"="+value)
	}
	args := []string{"--"}
	if filter != "" {
		args = append(args, filter)
	}

	return executeCommand(ctx, data, environment, "envsubst", args...)
}

func decodeResources(data []byte) ([]unstructured.Unstructured, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var resources []unstructured.Unstructured
	for {
		var resource unstructured.Unstructured
		if err := decoder.Decode(&resource); errors.Is(err, io.EOF) {
			return resources, nil
		} else if err != nil {
			return nil, err
		}
		pending := []unstructured.Unstructured{resource}
		for len(pending) > 0 {
			resource, pending = pending[0], pending[1:]
			if resource.IsList() {
				list, err := resource.ToList()
				if err != nil {
					return nil, err
				}
				pending = append(list.Items, pending...)
			} else if resource.Object != nil {
				resources = append(resources, resource)
			}
		}
	}
}

func isMachineConfig(resource unstructured.Unstructured) bool {
	return resource.GetAPIVersion() == "machineconfiguration.openshift.io/v1" && resource.GetKind() == machineConfigKind
}

func (r runner) prepareBatch(ctx context.Context, files []string) (*batch, error) {
	if !r.options.WaitForMCP || !r.options.BatchMCP {
		return nil, nil
	}
	for _, path := range files {
		if _, err := os.Stat(sibling(path, ".sh")); err == nil {
			return nil, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	prepared := &batch{}
	names := make(map[string]bool)
	for _, path := range files {
		data, err := renderManifest(ctx, path, r.options.Variables, r.options.VariableFilter)
		if err != nil {
			return nil, err
		}
		items, err := decodeResources(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		prepared.resources = append(prepared.resources, items...)
		for _, resource := range items {
			if !isMachineConfig(resource) {
				continue
			}
			name := resource.GetName()
			existing, err := r.oc(ctx, nil, "get", "machineconfig/"+name, "--ignore-not-found", "-o", "name")
			if err != nil {
				return nil, err
			}
			// Existing MC names cannot prove updated contents have rendered.
			if names[name] || len(bytes.TrimSpace(existing)) > 0 {
				fmt.Println("Existing or repeated MachineConfig found; using per-file rollout waits.")

				return nil, nil
			}
			names[name] = true
			prepared.machineConfigs = append(prepared.machineConfigs, name)
		}
	}

	return prepared, nil
}

func (r runner) applyResources(ctx context.Context, resources []unstructured.Unstructured) error {
	for _, resource := range resources {
		data, err := json.Marshal(resource.Object)
		if err != nil {
			return err
		}
		args := []string{actionApply, "-f", "-"}
		if isMachineConfig(resource) {
			args = []string{commandCreate, "--save-config", "-f", "-"}
		}
		if err := r.changeResource(ctx, data, args...); err != nil {
			return err
		}
	}

	return nil
}

func (r runner) changeResource(ctx context.Context, data []byte, args ...string) error {
	output, err := r.oc(ctx, data, args...)
	fmt.Print(string(output))

	return err
}

func (r runner) changeFiles(ctx context.Context, files []string) error {
	args := []string{r.options.Action, "-f", "-"}
	if r.options.Action == actionDelete {
		args = append(args, "--ignore-not-found=true")
	}
	for _, path := range files {
		data, err := renderManifest(ctx, path, r.options.Variables, r.options.VariableFilter)
		if err != nil {
			return err
		}
		if err := r.changeResource(ctx, data, args...); err != nil {
			return err
		}
		if r.options.Action == actionDelete {
			continue
		}
		if r.options.WaitForMCP {
			if err := r.waitForRollout(ctx, ""); err != nil {
				return err
			}
		}
		hook := sibling(path, ".sh")
		if _, err := os.Stat(hook); err == nil {
			output, err := r.command(ctx, nil, nil, "bash", hook)
			fmt.Print(string(output))
			if err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	return nil
}

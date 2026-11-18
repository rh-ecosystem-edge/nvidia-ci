package manifests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	oldConfiguration = "rendered-old"
	newConfiguration = "rendered-new"
)

var errSimulated = errors.New("simulated failure")

type operation struct {
	args   []string
	input  []byte
	paused bool
}

type fakeCluster struct {
	mode           string
	pool           poolState
	created        []string
	waited         []string
	changed        []string
	events         []operation
	cancel         context.CancelFunc
	hook           func() error
	renderErr      error
	startErr       error
	objects        map[string]unstructured.Unstructured
	holdRender     bool
	afterApply     func()
	mutateApplied  func(*unstructured.Unstructured)
	beforePoolRead func()
}

func (f *fakeCluster) store(resource unstructured.Unstructured) {
	if f.objects == nil {
		f.objects = make(map[string]unstructured.Unstructured)
	}
	f.objects[poolResourceName(resource)] = *resource.DeepCopy()
}

func (f *fakeCluster) patch(data string) error {
	patch, err := jsonpatch.DecodePatch([]byte(data))
	if err != nil {
		return err
	}
	current := f.pool
	if f.mode == "version-conflict" {
		current.ResourceVersion = "2"
	}
	document, err := json.Marshal(current)
	if err != nil {
		return err
	}
	document, err = patch.Apply(document)
	if err != nil {
		return err
	}
	var updated poolState
	if err := json.Unmarshal(document, &updated); err != nil {
		return err
	}
	wasPaused := f.pool.Spec.Paused
	if wasPaused && f.mode == "resume-error" {
		return errSimulated
	}
	f.pool = updated
	if !wasPaused && f.pool.Spec.Paused {
		if f.mode == "foreign-owner" {
			f.pool.Annotations[pauseOwner] = "someone-else"
		}
		if f.cancel != nil && f.mode == "cancel" {
			f.cancel()
		}
		if f.mode == "pause-error" || f.mode == "foreign-owner" {
			return errSimulated
		}
	}
	if wasPaused && !f.pool.Spec.Paused {
		f.pool.Status.Configuration = f.pool.Spec.Configuration
		if f.mode == "stale" {
			f.pool.Status.ObservedGeneration = 0
		}
	}

	return nil
}

func (f *fakeCluster) command(ctx context.Context, input []byte, _ []string, _ io.Writer, name string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if name == "envsubst" {
		return input, f.renderErr
	}
	f.events = append(f.events, operation{args: slices.Clone(args), input: slices.Clone(input), paused: f.pool.Spec.Paused})
	if name == "bash" && f.hook != nil {
		return nil, f.hook()
	}
	if name != "oc" || args[len(args)-1] != "--kubeconfig=/explicit/kubeconfig" {
		return nil, fmt.Errorf("unexpected command or implicit kubeconfig: %s %v", name, args)
	}
	switch args[0] {
	case "get":
		if strings.HasPrefix(args[1], "machineconfig/") {
			if f.mode == "existing" {
				return []byte(args[1]), nil
			}

			return nil, nil
		}
		if !strings.HasPrefix(args[1], "mcp/") {
			if resource, found := f.objects[args[1]]; found {
				return json.Marshal(resource.Object)
			}

			return nil, nil
		}

		if f.beforePoolRead != nil {
			f.beforePoolRead()
		}
		return json.Marshal(f.pool)
	case commandPatch:
		return nil, f.patch(args[slices.Index(args, "-p")+1])
	case actionApply, commandCreate, actionDelete:
		if f.mode == "apply-fail" || (args[0] == commandCreate && f.mode == "create-race") {
			return nil, errSimulated
		}
		resources, err := decodeResources(input)
		if err != nil {
			return nil, err
		}
		var output strings.Builder
		for _, resource := range resources {
			if resource.GetUID() == "" {
				previous := f.objects[poolResourceName(resource)]
				resource.SetUID(previous.GetUID())
			}
			if f.mutateApplied != nil {
				f.mutateApplied(&resource)
			}
			f.changed = append(f.changed, resource.GetName())
			if isMachineConfig(resource) && args[0] != actionDelete {
				f.created = append(f.created, resource.GetName())
			}
			f.store(resource)
			if !f.pool.Spec.Paused && !f.holdRender && isPoolConfiguration(resource) {
				f.pool.Spec.Configuration.Name = newConfiguration + "-" + resource.GetName()
				f.pool.Status.Configuration = f.pool.Spec.Configuration
			}
			if slices.Contains(args, "json") {
				if err := json.NewEncoder(&output).Encode(resource.Object); err != nil {
					return nil, err
				}
			} else {
				fmt.Fprintf(&output, "%s/%s configured\n", resource.GetKind(), resource.GetName())
			}
		}
		if f.mode == "cancel-apply" && f.cancel != nil {
			f.cancel()

			return nil, ctx.Err()
		}
		if f.afterApply != nil {
			f.afterApply()
		}

		return []byte(output.String()), nil
	case commandWait:
		condition := args[2]
		if condition == "--for=condition=Updating=True" {
			return nil, f.startErr
		}
		if _, remainder, ok := strings.Cut(condition, `@.name=="`); ok {
			name, _, _ := strings.Cut(remainder, `"`)
			if !f.pool.Spec.Paused || !slices.Contains(f.created, name) {
				return nil, fmt.Errorf("invalid source wait: %s", condition)
			}
			f.waited = append(f.waited, name)
			f.pool.Spec.Configuration.Source = append(f.pool.Spec.Configuration.Source, poolSource{Name: name})
			if f.mode == "render-timeout" {
				return nil, errSimulated
			}
			if slices.Equal(f.created, f.waited) {
				f.pool.Spec.Configuration.Name = newConfiguration
			}
		}
		if strings.Contains(condition, ".status.configuration.name") &&
			(f.pool.Spec.Paused || f.pool.Status.Configuration.Name != newConfiguration ||
				!strings.HasSuffix(condition, "="+newConfiguration)) {
			return nil, fmt.Errorf("rollout returned before all applied configs rendered")
		}
	default:
		return nil, fmt.Errorf("unexpected oc command: %v", args)
	}

	return nil, nil
}

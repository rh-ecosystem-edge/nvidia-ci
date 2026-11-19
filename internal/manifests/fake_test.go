package manifests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

const (
	oldConfiguration = "rendered-old"
	newConfiguration = "rendered-new"
	firstConfigName  = "first"
	alreadyPaused    = "already-paused"
)

var errSimulated = errors.New("simulated failure")

type operation struct {
	args   []string
	input  []byte
	paused bool
}

type fakeCluster struct {
	mode           string
	existing       string
	pool           poolState
	created        []string
	waited         []string
	changed        []string
	events         []operation
	cancel         context.CancelFunc
	hook           func() error
	renderErr      error
	startErr       error
	afterApply     func()
	beforePoolRead func()
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
	version, _ := strconv.Atoi(f.pool.ResourceVersion)
	updated.ResourceVersion = strconv.Itoa(version + 1)
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
	case commandGet:
		if strings.HasPrefix(args[1], "machineconfig/") {
			if args[1] == "machineconfig/"+f.existing {
				return []byte(args[1]), nil
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
		for _, resource := range resources {
			f.changed = append(f.changed, resource.GetName())
			if isMachineConfig(resource) && args[0] != actionDelete {
				f.created = append(f.created, resource.GetName())
			}
		}
		if f.mode == "cancel-apply" && f.cancel != nil {
			f.cancel()

			return nil, ctx.Err()
		}
		if f.afterApply != nil {
			f.afterApply()
		}
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
	default:
		return nil, fmt.Errorf("unexpected oc command: %v", args)
	}

	return nil, nil
}

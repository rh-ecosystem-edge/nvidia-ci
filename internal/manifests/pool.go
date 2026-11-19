package manifests

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	pauseOwner     = "nvidia-ci/pause-owner"
	cleanupTimeout = time.Minute
	patchAdd       = "add"
)

type poolConfiguration struct {
	Name   string       `json:"name"`
	Source []poolSource `json:"source"`
}

type poolSource struct {
	Name string `json:"name"`
}

func (configuration poolConfiguration) hasSource(name string) bool {
	return slices.ContainsFunc(configuration.Source, func(source poolSource) bool { return source.Name == name })
}

type poolState struct {
	metav1.ObjectMeta `json:"metadata"`
	Spec              struct {
		Paused                bool                  `json:"paused"`
		Configuration         poolConfiguration     `json:"configuration"`
		MachineConfigSelector *metav1.LabelSelector `json:"machineConfigSelector"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration int64              `json:"observedGeneration"`
		Configuration      poolConfiguration  `json:"configuration"`
		Conditions         []metav1.Condition `json:"conditions"`
	} `json:"status"`
}

type patchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

func (r runner) getPool(ctx context.Context) (*poolState, error) {
	data, err := r.oc(ctx, nil, commandGet, "mcp/"+r.options.PoolName, "-o", "json")
	if err != nil {
		return nil, err
	}
	var pool poolState
	err = json.Unmarshal(data, &pool)
	return &pool, err
}

func poolIsSettled(pool *poolState) bool {
	return meta.IsStatusConditionTrue(pool.Status.Conditions, "Updated") &&
		!meta.IsStatusConditionTrue(pool.Status.Conditions, "Updating") &&
		!pool.Spec.Paused && pool.Status.ObservedGeneration >= pool.Generation &&
		pool.Spec.Configuration.Name != "" && pool.Spec.Configuration.Name == pool.Status.Configuration.Name &&
		!meta.IsStatusConditionTrue(pool.Status.Conditions, "Degraded") &&
		!meta.IsStatusConditionTrue(pool.Status.Conditions, "RenderDegraded")
}

func poolSelector(pool *poolState) (labels.Selector, error) {
	if pool.Spec.MachineConfigSelector == nil || len(pool.Spec.MachineConfigSelector.MatchLabels)+len(pool.Spec.MachineConfigSelector.MatchExpressions) == 0 {
		return nil, fmt.Errorf("mcp/%s has an empty machineConfigSelector", pool.Name)
	}
	selector, err := metav1.LabelSelectorAsSelector(pool.Spec.MachineConfigSelector)
	if err != nil {
		return nil, fmt.Errorf("mcp/%s machineConfigSelector: %w", pool.Name, err)
	}

	return selector, nil
}

func (r runner) getSettledPool(ctx context.Context) (*poolState, error) {
	pool, err := r.getPool(ctx)
	if err != nil {
		return nil, err
	}
	if condition := meta.FindStatusCondition(pool.Status.Conditions, "RenderDegraded"); condition != nil && condition.Status == metav1.ConditionTrue {
		return pool, fmt.Errorf("mcp/%s render failed: %s", r.options.PoolName, condition.Message)
	}
	if !poolIsSettled(pool) {
		err = fmt.Errorf("mcp/%s has not converged to its current configuration", r.options.PoolName)
	}

	return pool, err
}

func (r runner) waitForPool(ctx context.Context, condition string, timeout time.Duration) error {
	fmt.Printf("Waiting for mcp/%s: %s (timeout: %s)\n", r.options.PoolName, condition, timeout)
	_, err := r.oc(ctx, nil, commandWait, "mcp/"+r.options.PoolName, "--for="+condition, "--timeout="+timeout.String())

	return err
}

func (r runner) checkPoolReady(ctx context.Context) (*poolState, error) {
	pool, err := r.getPool(ctx)
	if err != nil {
		return nil, err
	}
	if pool.Spec.Paused {
		return nil, fmt.Errorf("mcp/%s is already paused; refusing to change it", r.options.PoolName)
	}
	if err := r.waitForPool(ctx, "condition=Updated=True", r.options.RolloutTimeout); err != nil {
		return nil, err
	}
	return r.getSettledPool(ctx)
}

func (r runner) patchPool(ctx context.Context, operations []patchOperation) error {
	data, err := json.Marshal(operations)
	if err != nil {
		return err
	}
	_, err = r.oc(ctx, nil, commandPatch, "mcp/"+r.options.PoolName, "--type=json", "-p", string(data))

	return err
}

func (r runner) setPoolPause(ctx context.Context, pool *poolState, owner string, paused bool) error {
	annotations := pool.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[pauseOwner] = owner

	return r.patchPool(ctx, []patchOperation{
		{Op: "test", Path: "/metadata/resourceVersion", Value: pool.ResourceVersion},
		{Op: patchAdd, Path: "/metadata/annotations", Value: annotations},
		{Op: patchAdd, Path: "/spec/paused", Value: paused},
	})
}

func (r runner) resumePool(ctx context.Context, owner, originalVersion string, required bool) error {
	var pool *poolState
	var lastErr error
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		pool, lastErr = r.getPool(ctx)
		if lastErr != nil {
			return false, nil
		}
		if pool.Annotations[pauseOwner] == owner {
			return true, nil
		}
		if required {
			return false, fmt.Errorf("pause ownership changed; refusing to resume another caller's pause")
		}
		if pool.ResourceVersion != originalVersion {
			return true, nil
		}
		// Commit a new version so a delayed pause cannot pass its version test.
		lastErr = r.setPoolPause(ctx, pool, owner, false)
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("pause outcome could not be confirmed: %w", errors.Join(err, lastErr))
	}
	if pool.Annotations[pauseOwner] != owner {
		return nil
	}
	fmt.Printf("Resuming mcp/%s\n", r.options.PoolName)

	return r.patchPool(ctx, []patchOperation{
		{Op: "test", Path: "/metadata/annotations/nvidia-ci~1pause-owner", Value: owner},
		{Op: patchAdd, Path: "/spec/paused", Value: false},
		{Op: "remove", Path: "/metadata/annotations/nvidia-ci~1pause-owner"},
	})
}

func (r runner) applyPaused(ctx context.Context, prepared *batch) (target string, err error) {
	pool, err := r.checkPoolReady(ctx)
	if err != nil {
		return "", err
	}
	selector, err := poolSelector(pool)
	if err != nil {
		return "", err
	}
	for _, resource := range prepared.resources {
		if isMachineConfig(resource) && !selector.Matches(labels.Set(resource.GetLabels())) {
			return "", fmt.Errorf("MachineConfig/%s is not selected by mcp/%s", resource.GetName(), r.options.PoolName)
		}
	}
	for _, name := range prepared.machineConfigs {
		if pool.Spec.Configuration.hasSource(name) || pool.Status.Configuration.hasSource(name) {
			return "", fmt.Errorf("MachineConfig/%s is still referenced by mcp/%s; refusing to batch a reused name", name, r.options.PoolName)
		}
	}
	owner, paused := rand.Text(), false
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		// A failed pause client can still have committed its ownership token.
		if resumeErr := r.resumePool(cleanupCtx, owner, pool.ResourceVersion, paused); resumeErr != nil {
			err = errors.Join(err, fmt.Errorf("could not resume mcp/%s; check the pool before retrying: %w",
				r.options.PoolName, resumeErr))
		}
	}()
	// Let an in-flight pause finish before cancellation starts cleanup.
	pauseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	fmt.Printf("Pausing mcp/%s\n", r.options.PoolName)
	err = r.setPoolPause(pauseCtx, pool, owner, true)
	cancel()
	if err != nil {
		return "", err
	}
	paused = true
	if err := r.applyResources(ctx, prepared.resources); err != nil {
		return "", err
	}

	return r.waitForRender(ctx, prepared.machineConfigs)
}

func (r runner) waitForRender(ctx context.Context, machineConfigs []string) (string, error) {
	for _, name := range machineConfigs {
		condition := fmt.Sprintf(`jsonpath={.spec.configuration.source[?(@.name=="%s")].name}=%s`, name, name)
		if err := r.waitForPool(ctx, condition, r.options.StartTimeout); err != nil {
			_, poolErr := r.getSettledPool(ctx)
			return "", errors.Join(err, poolErr)
		}
	}
	pool, err := r.getPool(ctx)
	if err != nil {
		return "", err
	}
	if pool.Spec.Configuration.Name == "" {
		return "", fmt.Errorf("mcp/%s has no rendered target", r.options.PoolName)
	}
	for _, name := range machineConfigs {
		if !pool.Spec.Configuration.hasSource(name) {
			return "", fmt.Errorf("mcp/%s rendered target %s is missing MachineConfig/%s", r.options.PoolName, pool.Spec.Configuration.Name, name)
		}
	}

	return pool.Spec.Configuration.Name, nil
}

func (r runner) waitForRollout(ctx context.Context, target string, machineConfigs []string) error {
	if target == "" {
		_ = r.waitForPool(ctx, "condition=Updating=True", r.options.StartTimeout)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := r.waitForPool(ctx, "condition=Updated=True", r.options.RolloutTimeout); err != nil {
			return err
		}
		_, err := r.getSettledPool(ctx)
		return err
	}
	fmt.Printf("Waiting for mcp/%s to converge to %s (timeout: %s)\n", r.options.PoolName, target, r.options.RolloutTimeout)
	var renderErr error
	err := wait.PollUntilContextTimeout(ctx, time.Second, r.options.RolloutTimeout, true, func(ctx context.Context) (bool, error) {
		pool, err := r.getPool(ctx)
		if err != nil {
			return false, err
		}
		renderErr = nil
		if condition := meta.FindStatusCondition(pool.Status.Conditions, "RenderDegraded"); condition != nil && condition.Status == metav1.ConditionTrue {
			renderErr = fmt.Errorf("mcp/%s render failed: %s", r.options.PoolName, condition.Message)
		}
		if pool.Spec.Configuration.Name != target {
			return false, fmt.Errorf("mcp/%s targets %s instead of captured target %s", r.options.PoolName, pool.Spec.Configuration.Name, target)
		}
		settled := poolIsSettled(pool)
		for _, name := range machineConfigs {
			if !pool.Spec.Configuration.hasSource(name) {
				return false, fmt.Errorf("mcp/%s captured target %s lost MachineConfig/%s", r.options.PoolName, target, name)
			}
			settled = settled && pool.Status.Configuration.hasSource(name)
		}
		return settled, nil
	})
	if err != nil {
		return fmt.Errorf("mcp/%s has not converged: %w", r.options.PoolName, errors.Join(err, renderErr))
	}
	return nil
}

func (r runner) applyBatch(ctx context.Context, prepared *batch) error {
	if len(prepared.machineConfigs) == 0 {
		return r.applyResources(ctx, prepared.resources)
	}
	target, err := r.applyPaused(ctx, prepared)
	if err != nil {
		return err
	}

	return r.waitForRollout(ctx, target, prepared.machineConfigs)
}

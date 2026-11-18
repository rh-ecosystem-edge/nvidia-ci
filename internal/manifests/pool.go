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
)

const (
	pauseOwner     = "nvidia-ci/pause-owner"
	cleanupTimeout = time.Minute
)

type poolConfiguration struct {
	Name   string       `json:"name"`
	Source []poolSource `json:"source"`
}

type poolSource struct {
	Name string `json:"name"`
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
	data, err := r.oc(ctx, nil, "get", "mcp/"+r.options.PoolName, "-o", "json")
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
		!meta.IsStatusConditionTrue(pool.Status.Conditions, "Degraded") &&
		!meta.IsStatusConditionTrue(pool.Status.Conditions, "RenderDegraded") &&
		!pool.Spec.Paused && pool.Status.ObservedGeneration >= pool.Generation &&
		pool.Spec.Configuration.Name != "" && pool.Spec.Configuration.Name == pool.Status.Configuration.Name
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

func (r runner) pausePool(ctx context.Context, pool *poolState, owner string) error {
	fmt.Printf("Pausing mcp/%s\n", r.options.PoolName)
	annotations := pool.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[pauseOwner] = owner

	return r.patchPool(ctx, []patchOperation{
		{Op: "test", Path: "/metadata/resourceVersion", Value: pool.ResourceVersion},
		{Op: "add", Path: "/metadata/annotations", Value: annotations},
		{Op: "add", Path: "/spec/paused", Value: true},
	})
}

func (r runner) resumePool(ctx context.Context, owner string, required bool) error {
	pool, err := r.getPool(ctx)
	if err != nil {
		return err
	}
	if pool.Annotations[pauseOwner] != owner {
		if required {
			return fmt.Errorf("pause ownership changed; refusing to resume another caller's pause")
		}

		return nil
	}
	fmt.Printf("Resuming mcp/%s\n", r.options.PoolName)

	return r.patchPool(ctx, []patchOperation{
		{Op: "test", Path: "/metadata/annotations/nvidia-ci~1pause-owner", Value: owner},
		{Op: "add", Path: "/spec/paused", Value: false},
		{Op: "remove", Path: "/metadata/annotations/nvidia-ci~1pause-owner"},
	})
}

func (r runner) applyPaused(ctx context.Context, prepared *batch) (target string, err error) {
	pool, err := r.checkPoolReady(ctx)
	if err != nil {
		return "", err
	}
	selector, err := metav1.LabelSelectorAsSelector(pool.Spec.MachineConfigSelector)
	if err != nil {
		return "", fmt.Errorf("mcp/%s machineConfigSelector: %w", r.options.PoolName, err)
	}
	for _, resource := range prepared.resources {
		if isMachineConfig(resource) && !selector.Matches(labels.Set(resource.GetLabels())) {
			return "", fmt.Errorf("MachineConfig/%s is not selected by mcp/%s", resource.GetName(), r.options.PoolName)
		}
	}
	owner, paused := rand.Text(), false
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		// A failed pause client can still have committed its ownership token.
		if resumeErr := r.resumePool(cleanupCtx, owner, paused); resumeErr != nil {
			err = errors.Join(err, fmt.Errorf("could not resume mcp/%s; check the pool before retrying: %w",
				r.options.PoolName, resumeErr))
		}
	}()
	// Let an in-flight pause finish before cancellation starts cleanup.
	pauseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	err = r.pausePool(pauseCtx, pool, owner)
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
			return "", err
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
		if !slices.ContainsFunc(pool.Spec.Configuration.Source, func(source poolSource) bool { return source.Name == name }) {
			return "", fmt.Errorf("mcp/%s rendered target %s is missing MachineConfig/%s", r.options.PoolName, pool.Spec.Configuration.Name, name)
		}
	}

	return pool.Spec.Configuration.Name, nil
}

func (r runner) waitForRollout(ctx context.Context, target, previousTarget string) error {
	startErr := r.waitForPool(ctx, "condition=Updating=True", r.options.StartTimeout)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := r.waitForPool(ctx, "condition=Updated=True", r.options.RolloutTimeout); err != nil {
		return err
	}
	if target != "" {
		if err := r.waitForPool(ctx, "jsonpath={.status.configuration.name}="+target, r.options.RolloutTimeout); err != nil {
			return err
		}
	}
	pool, err := r.getSettledPool(ctx)
	if err != nil {
		return err
	}
	if startErr != nil && previousTarget != "" && pool.Spec.Configuration.Name == previousTarget {
		return fmt.Errorf("no new rendered configuration or rollout observed for mcp/%s: %w", r.options.PoolName, startErr)
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

	return r.waitForRollout(ctx, target, "")
}

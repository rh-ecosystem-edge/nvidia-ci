package manifests

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
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

func poolHasCurrentConfiguration(pool *poolState) bool {
	return meta.IsStatusConditionTrue(pool.Status.Conditions, "Updated") &&
		!meta.IsStatusConditionTrue(pool.Status.Conditions, "Updating") &&
		!pool.Spec.Paused && pool.Status.ObservedGeneration >= pool.Generation &&
		pool.Spec.Configuration.Name != "" && pool.Spec.Configuration.Name == pool.Status.Configuration.Name
}

func poolIsSettled(pool *poolState) bool {
	return poolHasCurrentConfiguration(pool) &&
		!meta.IsStatusConditionTrue(pool.Status.Conditions, "Degraded") &&
		!meta.IsStatusConditionTrue(pool.Status.Conditions, "RenderDegraded")
}

func poolResourceName(resource unstructured.Unstructured) string {
	return strings.ToLower(resource.GetKind()) + "." + resource.GroupVersionKind().Group + "/" + resource.GetName()
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

func targetsPool(resource unstructured.Unstructured, pool *poolState) (bool, error) {
	if resource.Object == nil {
		return false, nil
	}
	if isMachineConfig(resource) {
		selector, err := poolSelector(pool)
		return err == nil && selector.Matches(labels.Set(resource.GetLabels())), err
	}
	data, _, err := unstructured.NestedMap(resource.Object, "spec", "machineConfigPoolSelector")
	if err != nil || len(data) == 0 {
		return false, err
	}
	var selection metav1.LabelSelector
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(data, &selection); err != nil {
		return false, err
	}
	selector, err := metav1.LabelSelectorAsSelector(&selection)
	return err == nil && selector.Matches(labels.Set(pool.Labels)), err
}

func (r runner) readPoolInputs(ctx context.Context, resources []unstructured.Unstructured) (map[string]unstructured.Unstructured, *poolState, error) {
	if !slices.ContainsFunc(resources, isPoolConfiguration) {
		return nil, nil, nil
	}
	pool, err := r.getPool(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !poolHasCurrentConfiguration(pool) {
		return nil, nil, fmt.Errorf("mcp/%s must finish its current rollout before applying pool inputs", r.options.PoolName)
	}
	if _, err := poolSelector(pool); err != nil {
		return nil, nil, err
	}
	inputs := make(map[string]unstructured.Unstructured)
	for _, resource := range resources {
		if !isPoolConfiguration(resource) {
			continue
		}
		if _, err := targetsPool(resource, pool); err != nil {
			return nil, nil, err
		}
		name := poolResourceName(resource)
		data, err := r.oc(ctx, nil, commandGet, name, "--ignore-not-found", "-o", "json")
		if err != nil {
			return nil, nil, err
		}
		var current unstructured.Unstructured
		if len(data) > 0 {
			if err := json.Unmarshal(data, &current); err != nil {
				return nil, nil, err
			}
		}
		inputs[name] = current
	}

	return inputs, pool, nil
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
		{Op: "add", Path: "/metadata/annotations", Value: annotations},
		{Op: "add", Path: "/spec/paused", Value: paused},
	})
}

func (r runner) resumePool(ctx context.Context, owner, originalVersion string, required bool) error {
	pool, err := r.getPool(ctx)
	if err != nil {
		return err
	}
	if pool.Annotations[pauseOwner] != owner {
		if required {
			return fmt.Errorf("pause ownership changed; refusing to resume another caller's pause")
		}
		if pool.ResourceVersion == originalVersion {
			// Commit a new version so a delayed pause cannot pass its version test.
			fenceErr := r.setPoolPause(ctx, pool, owner, false)
			if err := r.resumePool(ctx, owner, originalVersion, true); err != nil {
				return errors.Join(fenceErr, err)
			}
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

func (r runner) waitForRollout(ctx context.Context, target string) error {
	if target != "" {
		fmt.Printf("Waiting for mcp/%s to converge to %s (timeout: %s)\n", r.options.PoolName, target, r.options.RolloutTimeout)
		return r.pollPool(ctx, r.options.RolloutTimeout, func(pool *poolState) (bool, error) {
			if pool.Spec.Configuration.Name != target {
				return false, fmt.Errorf("mcp/%s targets %s instead of captured target %s", r.options.PoolName, pool.Spec.Configuration.Name, target)
			}
			return poolIsSettled(pool), nil
		})
	}
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

func (r runner) pollPool(ctx context.Context, timeout time.Duration, ready func(*poolState) (bool, error)) error {
	err := wait.PollUntilContextTimeout(ctx, time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pool, err := r.getPool(ctx)
		if err != nil {
			return false, err
		}
		if condition := meta.FindStatusCondition(pool.Status.Conditions, "RenderDegraded"); condition != nil && condition.Status == metav1.ConditionTrue {
			return false, fmt.Errorf("mcp/%s render failed: %s", r.options.PoolName, condition.Message)
		}
		return ready(pool)
	})
	if err != nil {
		return fmt.Errorf("mcp/%s has not converged: %w", r.options.PoolName, err)
	}
	return nil
}

func (r runner) confirmRenderedInputs(ctx context.Context, pool *poolState) (err error) {
	selector, err := poolSelector(pool)
	if err != nil {
		return err
	}
	data, err := r.oc(ctx, nil, commandGet, "machineconfig", "-l", selector.String(), "-o", "json")
	if err != nil {
		return err
	}
	resources, err := decodeResources(data)
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		return fmt.Errorf("cannot find a MachineConfig selected by mcp/%s", r.options.PoolName)
	}
	marker := unstructured.Unstructured{Object: map[string]any{"apiVersion": "machineconfiguration.openshift.io/v1", "kind": machineConfigKind, "spec": map[string]any{}}}
	marker.SetName("zz-nvidia-ci-render-" + strings.ToLower(rand.Text()))
	marker.SetLabels(resources[0].GetLabels())
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		_, deleteErr := r.oc(cleanupCtx, nil, actionDelete, "machineconfig/"+marker.GetName(), "--ignore-not-found=true", "--wait=false")
		if deleteErr == nil && err == nil {
			deleteErr = r.pollPool(cleanupCtx, cleanupTimeout, func(pool *poolState) (bool, error) {
				return !pool.Spec.Configuration.hasSource(marker.GetName()) && poolIsSettled(pool), nil
			})
		}
		if deleteErr != nil {
			err = errors.Join(err, fmt.Errorf("remove render marker MachineConfig/%s: %w", marker.GetName(), deleteErr))
		}
	}()
	data, err = marker.MarshalJSON()
	if err != nil {
		return err
	}
	// MC informer events are ordered: observing this new source includes earlier MC writes.
	if _, err := r.oc(ctx, data, commandCreate, "-f", "-"); err != nil {
		return err
	}
	target, err := r.waitForRender(ctx, []string{marker.GetName()})
	if err != nil {
		return fmt.Errorf("render confirmation for mcp/%s: %w", r.options.PoolName, err)
	}
	return r.waitForRollout(ctx, target)
}

func (r runner) waitForGeneratedInput(ctx context.Context, resource unstructured.Unstructured) error {
	name := poolResourceName(resource)
	condition := fmt.Sprintf("--for=jsonpath={.status.observedGeneration}=%d", resource.GetGeneration())
	if _, err := r.oc(ctx, nil, commandWait, name, condition, "--timeout="+r.options.StartTimeout.String()); err != nil {
		return err
	}
	data, err := r.oc(ctx, nil, commandGet, name, "-o", "json")
	if err != nil {
		return err
	}
	var current unstructured.Unstructured
	if err := current.UnmarshalJSON(data); err != nil {
		return err
	}
	conditions, _, err := unstructured.NestedSlice(current.Object, "status", "conditions")
	if err != nil || len(conditions) == 0 {
		return fmt.Errorf("%s has no generation result: %v", name, err)
	}
	last, ok := conditions[len(conditions)-1].(map[string]any)
	observed, _, observedErr := unstructured.NestedInt64(current.Object, "status", "observedGeneration")
	if !ok || last["type"] != "Success" || last["status"] != "True" ||
		observedErr != nil || observed < resource.GetGeneration() || current.GetGeneration() != resource.GetGeneration() ||
		current.GetUID() != resource.GetUID() || !reflect.DeepEqual(current.Object["spec"], resource.Object["spec"]) {
		return fmt.Errorf("%s has not successfully generated the applied configuration", name)
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

	return r.waitForRollout(ctx, target)
}

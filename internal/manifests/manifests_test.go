package manifests

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const noRenderMessage = "render confirmation"

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	NewWithT(t).Expect(os.WriteFile(path, []byte(contents), 0o600)).To(Succeed())
}

func testRunner(t *testing.T, mode string) (runner, *fakeCluster) {
	t.Helper()
	directory := t.TempDir()
	for name, contents := range map[string]string{
		"00-first.yaml": `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: first
  labels:
    role: worker
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      files:
      - path: /etc/test.rules
        contents:
          source: data:text/plain;base64,dGVzdA==
`,
		"00-first.env-required": "RULES_BASE64\n",
		"10-second.yml":         "apiVersion: machineconfiguration.openshift.io/v1\nkind: MachineConfig\nmetadata:\n  name: second\n  labels:\n    role: worker\n",
		"20-settings.yaml":      "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n",
	} {
		writeFile(t, filepath.Join(directory, name), contents)
	}
	options := Options{
		Action: actionApply, Directory: directory, Kubeconfig: "/explicit/kubeconfig", PoolName: "worker",
		Variables:  map[string]string{"RULES_BASE64": "dGVzdA=="},
		WaitForMCP: true, BatchMCP: true, StartTimeout: time.Minute, RolloutTimeout: 20 * time.Millisecond,
	}
	cluster := &fakeCluster{mode: mode}
	cluster.pool.Name = options.PoolName
	cluster.pool.Labels = map[string]string{"pool": "worker"}
	cluster.pool.Generation, cluster.pool.ResourceVersion = 1, "1"
	cluster.pool.Spec.Configuration.Name = oldConfiguration
	cluster.pool.Spec.MachineConfigSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}}
	cluster.pool.Status.Configuration = cluster.pool.Spec.Configuration
	cluster.pool.Status.ObservedGeneration = 1
	cluster.pool.Status.Conditions = []metav1.Condition{{Type: "Updated", Status: metav1.ConditionTrue}}
	base := unstructured.Unstructured{Object: map[string]any{"apiVersion": "machineconfiguration.openshift.io/v1", "kind": machineConfigKind}}
	base.SetName("00-base")
	base.SetLabels(map[string]string{"role": "worker"})
	cluster.store(base)

	return runner{options: options, command: cluster.command}, cluster
}

func TestFreshBatchIncludesAdditionalManifests(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	writeFile(t, filepath.Join(r.options.Directory, "30-extra.yaml"),
		"apiVersion: machineconfiguration.openshift.io/v1\nkind: MachineConfig\nmetadata:\n  name: extra\n  labels:\n    role: worker\n"+
			"---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: extra-settings\n")
	g.Expect(r.run(t.Context())).To(Succeed())
	g.Expect(cluster.pool.Spec.Paused).To(BeFalse())
	g.Expect(cluster.waited).To(Equal(cluster.created))
	patches, starts, lastSource, capture, resume := 0, 0, -1, -1, -1
	for index, event := range cluster.events {
		switch event.args[0] {
		case commandPatch:
			patches++
			if event.paused {
				resume = index
			}
		case commandCreate, actionApply:
			g.Expect(event.paused).To(BeTrue())
			if event.args[0] == commandCreate {
				g.Expect(event.args).To(ContainElement("--save-config"))
			}
			if strings.Contains(string(event.input), `"name":"first"`) {
				g.Expect(string(event.input)).To(And(ContainSubstring("/etc/test.rules"),
					ContainSubstring("data:text/plain;base64,dGVzdA==")))
			}
		case commandWait:
			if strings.Contains(event.args[2], ".spec.configuration.source") {
				lastSource = index
			}
			if event.args[2] == "--for=condition=Updating=True" {
				starts++
			}
		case commandGet:
			if lastSource >= 0 && event.paused {
				capture = index
			}
		}
	}
	g.Expect(cluster.changed).To(Equal([]string{"first", "second", "settings", "extra", "extra-settings"}))
	if patches != 2 || starts != 0 || lastSource < 0 || lastSource >= capture || capture >= resume {
		t.Fatalf("bad ordering: patches=%d starts=%d source=%d capture=%d resume=%d",
			patches, starts, lastSource, capture, resume)
	}
}

func TestBatchFailuresPreservePauseOwnership(t *testing.T) {
	for _, test := range []struct{ mode, message string }{
		{"apply-fail", "oc create --save-config"},
		{"create-race", "oc create --save-config"},
		{"pause-error", "oc patch mcp/worker"},
		{"render-timeout", ".spec.configuration.source"},
		{"stale", "has not converged"},
		{"cancel", "context canceled"},
		{"cancel-apply", "context canceled"},
		{"already-paused", "is already paused"},
		{"foreign-owner", "oc patch mcp/worker"},
		{"version-conflict", "testing value /metadata/resourceVersion failed"},
		{"resume-error", "could not resume mcp/worker"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			g := NewWithT(t)
			mode := test.mode
			r, cluster := testRunner(t, mode)
			cluster.pool.Spec.Paused = mode == "already-paused"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if strings.HasPrefix(mode, "cancel") {
				cluster.cancel = cancel
			}
			g.Expect(r.run(ctx)).To(MatchError(ContainSubstring(test.message)))
			g.Expect(cluster.pool.Spec.Paused).To(Equal(slices.Contains(
				[]string{"already-paused", "foreign-owner", "resume-error"}, mode)))
		})
	}
}

func TestExistingConfigsUseSequentialWaits(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "existing")
	g.Expect(r.run(t.Context())).To(Succeed())
	starts, applies := 0, 0
	for _, event := range cluster.events {
		g.Expect(event.args[0]).NotTo(Equal(commandPatch))
		g.Expect(event.paused).To(BeFalse())
		if slices.Contains(event.args, "--for=condition=Updating=True") {
			starts++
		}
		if event.args[0] == actionApply {
			applies++
		}
	}
	g.Expect(applies).To(BeNumerically(">", 0))
	g.Expect(starts).To(Equal(1))
}

func TestRequiredInputsDoNotReadAmbientEnvironment(t *testing.T) {
	for _, key := range []string{"KUBECONFIG", "RULES_BASE64"} {
		t.Run(key, func(t *testing.T) {
			g := NewWithT(t)
			t.Setenv(key, "ambient-value-must-not-be-used")
			r, cluster := testRunner(t, "")
			if key == "KUBECONFIG" {
				r.options.Kubeconfig = ""
			} else {
				delete(r.options.Variables, key)
			}
			g.Expect(r.run(t.Context())).To(HaveOccurred())
			g.Expect(cluster.events).To(BeEmpty())
		})
	}
}

func TestRenderingUsesExplicitValuesAndFilter(t *testing.T) {
	g := NewWithT(t)
	t.Setenv("MISSING", "ambient-value-must-not-be-used")
	path := filepath.Join(t.TempDir(), "template.yaml")
	writeFile(t, path, "$ONE ${TWO} $MISSING ${UNCHANGED:-default}")
	for _, test := range []struct{ filter, expected string }{
		{"", "literal-$TWO two  ${UNCHANGED:-default}"},
		{"$ONE ${MISSING}", "literal-$TWO ${TWO}  ${UNCHANGED:-default}"},
		{"--help", "$ONE ${TWO} $MISSING ${UNCHANGED:-default}"},
	} {
		r := runner{options: Options{Variables: map[string]string{"ONE": "literal-$TWO", "TWO": "two"}, VariableFilter: test.filter}, command: executeCommand}
		data, err := r.renderManifest(t.Context(), path)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(data)).To(Equal(test.expected))
	}
	data, err := (runner{command: executeCommand}).renderManifest(t.Context(), path)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(data)).To(Equal("   ${UNCHANGED:-default}"))
}

func TestMissingEnvsubstFailsBeforeClusterOperations(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	r.command = func(ctx context.Context, input []byte, environment []string, stdout io.Writer, name string, args ...string) ([]byte, error) {
		if name == "envsubst" {
			return executeCommand(ctx, input, environment, stdout, name, args...)
		}

		return cluster.command(ctx, input, environment, stdout, name, args...)
	}
	t.Setenv("PATH", t.TempDir())
	g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring("envsubst")))
	g.Expect(cluster.events).To(BeEmpty())
}

func TestRenderFailurePreventsClusterChanges(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	cluster.renderErr = errSimulated
	err := r.run(t.Context())
	g.Expect(err).To(MatchError(ContainSubstring("render ")))
	g.Expect(err).To(MatchError(ContainSubstring("00-first.yaml")))
	g.Expect(err).To(MatchError(ContainSubstring(errSimulated.Error())))
	g.Expect(cluster.events).To(BeEmpty())
}

func TestKubeconfigListsFailBeforeClusterChanges(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	r.options.Kubeconfig = "/first/config" + string(os.PathListSeparator) + "/second/config"
	g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring("must name a single file")))
	g.Expect(cluster.events).To(BeEmpty())
}

func TestBatchChecksPoolSelectorBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name     string
		selector *metav1.LabelSelector
		message  string
	}{
		{"missing-selector", nil, "empty machineConfigSelector"},
		{"empty-selector", &metav1.LabelSelector{}, "empty machineConfigSelector"},
		{"wrong-pool", &metav1.LabelSelector{MatchLabels: map[string]string{"role": "master"}}, "is not selected by mcp/worker"},
		{"matching-expression", &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: "role", Operator: metav1.LabelSelectorOpIn, Values: []string{"worker"}},
		}}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			cluster.pool.Spec.MachineConfigSelector = test.selector
			err := r.run(t.Context())
			if test.message == "" {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(cluster.created).To(HaveLen(2))
				return
			}
			g.Expect(err).To(MatchError(ContainSubstring(test.message)))
			g.Expect(cluster.changed).To(BeEmpty())
			for _, event := range cluster.events {
				g.Expect(event.args[0]).NotTo(Equal(commandPatch))
			}
		})
	}
}

func TestCapturedRenderMustStillIncludeEverySource(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	cluster.beforePoolRead = func() {
		if len(cluster.waited) == 2 && len(cluster.pool.Spec.Configuration.Source) == 2 {
			cluster.pool.Spec.Configuration.Source = cluster.pool.Spec.Configuration.Source[1:]
		}
	}
	g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring("is missing MachineConfig/first")))
	g.Expect(cluster.waited).To(Equal(cluster.created))
	g.Expect(cluster.pool.Spec.Paused).To(BeFalse())
	for _, event := range cluster.events {
		g.Expect(event.args).NotTo(ContainElement("--for=condition=Updating=True"))
	}
}

func TestSequentialChangesRequireRenderEvidence(t *testing.T) {
	for _, test := range []struct {
		name, kind, message                                             string
		unchanged, degraded, rendered, initiallyDegraded, observedStart bool
	}{
		{name: "render-failure", kind: machineConfigKind, degraded: true, message: "render failed: invalid config"},
		{name: "unprocessed-change", kind: machineConfigKind, message: noRenderMessage},
		{name: "updating-but-old-render", kind: machineConfigKind, observedStart: true, message: noRenderMessage},
		{name: "unchanged-pending-render", kind: machineConfigKind, unchanged: true, message: noRenderMessage},
		{name: "unchanged-settled-render", kind: machineConfigKind, unchanged: true, rendered: true},
		{name: "fast-rollout", kind: machineConfigKind, rendered: true},
		{name: "repair-degraded-config", kind: machineConfigKind, initiallyDegraded: true, rendered: true},
		{name: "unchanged-but-degraded", kind: machineConfigKind, unchanged: true, degraded: true, message: "render failed: invalid config"},
		{name: "kubelet-not-rendered", kind: "KubeletConfig", message: noRenderMessage},
		{name: "runtime-not-rendered", kind: "ContainerRuntimeConfig", message: noRenderMessage},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			r.options.BatchMCP = false
			r.options.Directory = t.TempDir()
			contents := "apiVersion: machineconfiguration.openshift.io/v1\nkind: " + test.kind + "\nmetadata:\n  name: config\n  labels:\n    role: worker\n"
			if test.kind != machineConfigKind {
				contents += "spec:\n  machineConfigPoolSelector:\n    matchLabels:\n      pool: worker\n"
			}
			writeFile(t, filepath.Join(r.options.Directory, "config.yaml"), contents)
			if test.unchanged {
				resources, err := decodeResources([]byte(contents))
				g.Expect(err).NotTo(HaveOccurred())
				cluster.store(resources[0])
			}
			cluster.startErr = errSimulated
			if test.observedStart {
				cluster.startErr = nil
			}
			cluster.holdRender = true
			if test.initiallyDegraded {
				cluster.pool.Status.Conditions = append(cluster.pool.Status.Conditions,
					metav1.Condition{Type: "RenderDegraded", Status: metav1.ConditionTrue, Message: "old invalid config"})
			}
			cluster.afterApply = func() {
				if test.initiallyDegraded {
					meta.RemoveStatusCondition(&cluster.pool.Status.Conditions, "RenderDegraded")
				}
				if test.degraded {
					cluster.pool.Status.Conditions = append(cluster.pool.Status.Conditions,
						metav1.Condition{Type: "RenderDegraded", Status: metav1.ConditionTrue, Message: "invalid config"})
				}
				if test.rendered {
					cluster.holdRender = false
					cluster.pool.Spec.Configuration.Name = newConfiguration
					cluster.pool.Status.Configuration = cluster.pool.Spec.Configuration
				}
			}
			err := r.run(t.Context())
			g.Expect(cluster.changed).NotTo(BeEmpty())
			if test.message == "" {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(test.message)))
			}
		})
	}
}

func TestMixedFileOnlyRequiresRenderForPoolChanges(t *testing.T) {
	for _, test := range []struct {
		name        string
		poolChanged bool
	}{
		{"configmap-changed", false},
		{"machineconfig-changed", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			r.options.BatchMCP = false
			r.options.Directory = t.TempDir()
			contents := `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: mixed
  labels:
    role: worker
spec:
  kernelArguments: [new]
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: companion
data:
  value: new
`
			resources, err := decodeResources([]byte(contents))
			g.Expect(err).NotTo(HaveOccurred())
			if test.poolChanged {
				resources[0].Object["spec"] = map[string]any{"kernelArguments": []any{"old"}}
			} else {
				resources[1].Object["data"] = map[string]any{"value": "old"}
			}
			for _, resource := range resources {
				cluster.store(resource)
			}
			path := filepath.Join(r.options.Directory, "config.yaml")
			writeFile(t, path, contents)
			writeFile(t, sibling(path, ".sh"), "hook")
			hookRan := false
			cluster.hook = func() error { hookRan = true; return nil }
			cluster.holdRender, cluster.startErr = test.poolChanged, errSimulated
			cluster.reuseTarget = !test.poolChanged
			err = r.run(t.Context())
			if test.poolChanged {
				g.Expect(err).To(MatchError(ContainSubstring(noRenderMessage)))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
			g.Expect(hookRan).To(Equal(!test.poolChanged))
			g.Expect(cluster.changed).To(Equal([]string{"mixed", "companion"}))
		})
	}
}

func TestMetadataChangesUseActualRenderInputs(t *testing.T) {
	for _, test := range []struct {
		name        string
		prepare     func(*unstructured.Unstructured, *unstructured.Unstructured)
		admit       func(*unstructured.Unstructured)
		needsRender bool
	}{
		{name: "irrelevant-annotation"},
		{name: "irrelevant-label", prepare: func(_, after *unstructured.Unstructured) {
			after.SetLabels(map[string]string{"role": "worker", "example.com/report": "yes"})
		}},
		{name: "api-defaults", prepare: func(before, _ *unstructured.Unstructured) {
			before.Object["spec"].(map[string]any)["fips"] = false
		}, admit: func(resource *unstructured.Unstructured) { resource.Object["spec"].(map[string]any)["fips"] = false }},
		{name: "controller-annotation", needsRender: true, prepare: func(before, after *unstructured.Unstructured) {
			before.SetAnnotations(map[string]string{"machineconfiguration.openshift.io/generated-by-controller-version": "old"})
			after.SetAnnotations(map[string]string{"machineconfiguration.openshift.io/generated-by-controller-version": "new"})
		}},
		{name: "removed-controller-annotation", needsRender: true, prepare: func(before, _ *unstructured.Unstructured) {
			before.SetAnnotations(map[string]string{"machineconfiguration.openshift.io/mc-name-suffix": "old"})
		}},
		{name: "selection-label", needsRender: true, prepare: func(_, after *unstructured.Unstructured) {
			after.SetLabels(map[string]string{"role": "master"})
		}},
		{name: "replaced-object", needsRender: true, admit: func(resource *unstructured.Unstructured) { resource.SetUID("replacement") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			r.options.BatchMCP = false
			r.options.Directory = t.TempDir()
			resources, err := decodeResources([]byte(`{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"MachineConfig",
"metadata":{"name":"metadata-only","labels":{"role":"worker"},"annotations":{"example.com/note":"new"}},
"spec":{"kernelArguments":["preserved"]}}`))
			g.Expect(err).NotTo(HaveOccurred())
			desired := resources[0]
			before := desired.DeepCopy()
			before.SetUID("original")
			before.SetAnnotations(map[string]string{"example.com/note": "old"})
			if test.prepare != nil {
				test.prepare(before, &desired)
			}
			cluster.store(*before)
			cluster.mutateApplied = test.admit
			cluster.holdRender, cluster.startErr = test.needsRender, errSimulated
			cluster.reuseTarget = !test.needsRender
			data, err := desired.MarshalJSON()
			g.Expect(err).NotTo(HaveOccurred())
			writeFile(t, filepath.Join(r.options.Directory, "config.yaml"), string(data))
			err = r.run(t.Context())
			if test.needsRender {
				g.Expect(err).To(MatchError(ContainSubstring(noRenderMessage)))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
			g.Expect(cluster.changed).To(Equal([]string{"metadata-only"}))
		})
	}
}

func TestEarlierRolloutCannotSatisfyLaterApply(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "existing")
	cluster.pool.Spec.Configuration.Name = "rendered-earlier"
	cluster.pool.Status.Conditions = []metav1.Condition{
		{Type: "Updated", Status: metav1.ConditionFalse}, {Type: "Updating", Status: metav1.ConditionTrue},
	}
	g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring("must finish its current rollout before applying pool inputs")))
	g.Expect(cluster.changed).To(BeEmpty())
}

func TestMissingApplyResponseCannotConfirmNoChange(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "existing")
	r.command = func(ctx context.Context, input []byte, environment []string, stdout io.Writer, name string, args ...string) ([]byte, error) {
		output, err := cluster.command(ctx, input, environment, stdout, name, args...)
		if name == "oc" && args[0] == actionApply {
			return nil, err
		}

		return output, err
	}
	g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring("apply response is missing machineconfig.machineconfiguration.openshift.io/first")))
	g.Expect(cluster.changed).To(Equal([]string{"first"}))
}

func TestBatchRejectsNamesInPreviousRender(t *testing.T) {
	for _, statusOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "spec", true: "status"}[statusOnly], func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			configuration := &cluster.pool.Spec.Configuration
			if statusOnly {
				configuration = &cluster.pool.Status.Configuration
			}
			configuration.Source = []poolSource{{Name: "first"}}
			g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring("refusing to batch a reused name")))
			g.Expect(cluster.changed).To(BeEmpty())
			for _, event := range cluster.events {
				g.Expect(event.args[0]).NotTo(Equal(commandPatch))
			}
		})
	}
}

func TestBatchFinalSnapshotMustMatchCapturedTarget(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	cluster.beforePoolRead = func() {
		if !cluster.pool.Spec.Paused && len(cluster.waited) == 2 {
			cluster.pool.Spec.Configuration.Name = "rendered-unrelated"
			cluster.pool.Status.Configuration = cluster.pool.Spec.Configuration
		}
	}
	g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring("instead of captured target " + newConfiguration)))
	g.Expect(cluster.pool.Spec.Paused).To(BeFalse())
}

func TestCleanupKeepsCancellationAndResumeError(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "resume-error")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cluster.afterApply = cancel
	err := r.run(ctx)
	g.Expect(errors.Is(err, context.Canceled)).To(BeTrue())
	g.Expect(errors.Is(err, errSimulated)).To(BeTrue())
	g.Expect(err).To(MatchError(ContainSubstring("check the pool before retrying")))
	g.Expect(cluster.pool.Spec.Paused).To(BeTrue())
}

func TestRenderConfirmationPrecedesHooks(t *testing.T) {
	for _, test := range []struct {
		name                          string
		unchanged, unrelated, confirm bool
	}{
		{name: "retry-before-render", unchanged: true},
		{name: "unrelated-concurrent-render", unrelated: true},
		{name: "effective-no-op", confirm: true},
		{name: "unchanged-and-confirmed", unchanged: true, confirm: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			r.options.Directory, r.options.BatchMCP = t.TempDir(), false
			contents := `{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"MachineConfig","metadata":{"name":"input","labels":{"role":"worker"}},"spec":{"kernelArguments":["requested"]}}`
			if test.unchanged {
				resources, err := decodeResources([]byte(contents))
				g.Expect(err).NotTo(HaveOccurred())
				cluster.store(resources[0])
			}
			path := filepath.Join(r.options.Directory, "input.yaml")
			writeFile(t, path, contents)
			writeFile(t, sibling(path, ".sh"), "hook")
			hookRan := false
			cluster.hook = func() error { hookRan = true; return nil }
			cluster.holdRender, cluster.reuseTarget = !test.confirm, true
			cluster.afterApply = func() {
				if test.unrelated {
					cluster.pool.Spec.Configuration.Name = "rendered-concurrent"
					cluster.pool.Status.Configuration = cluster.pool.Spec.Configuration
				}
			}
			err := r.run(t.Context())
			if test.confirm {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(cluster.pool.Spec.Configuration.Name).To(Equal(oldConfiguration))
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(noRenderMessage)))
			}
			g.Expect(hookRan).To(Equal(test.confirm))
			for _, resource := range cluster.objects {
				g.Expect(resource.GetName()).NotTo(HavePrefix("zz-nvidia-ci-render-"))
			}
		})
	}
}

func TestGeneratedInputsRespectPoolSelection(t *testing.T) {
	for _, kind := range []string{"KubeletConfig", "ContainerRuntimeConfig"} {
		for _, test := range []struct {
			name, before, after string
			pending, confirm    bool
		}{
			{name: "other-pool", before: "master", after: "master"},
			{name: "this-pool", before: "worker", after: "worker", confirm: true},
			{name: "leaving-pool", before: "worker", after: "master", confirm: true},
			{name: "pending-generation", before: "worker", after: "worker", pending: true},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				g := NewWithT(t)
				r, cluster := testRunner(t, "")
				r.options.Directory = t.TempDir()
				resource := unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "machineconfiguration.openshift.io/v1", "kind": kind,
					"metadata": map[string]any{"name": "generated"},
					"spec": map[string]any{"machineConfigPoolSelector": map[string]any{"matchExpressions": []any{map[string]any{
						"key": "pool", "operator": "In", "values": []any{test.before},
					}}}},
				}}
				resource.SetGeneration(1)
				cluster.store(resource)
				resource.Object["spec"] = map[string]any{"machineConfigPoolSelector": map[string]any{"matchLabels": map[string]any{"pool": test.after}}}
				data, err := resource.MarshalJSON()
				g.Expect(err).NotTo(HaveOccurred())
				writeFile(t, filepath.Join(r.options.Directory, "config.yaml"), string(data))
				cluster.holdGenerated = test.pending
				err = r.run(t.Context())
				if cluster.holdGenerated {
					g.Expect(err).To(MatchError(ContainSubstring("status.observedGeneration")))
				} else {
					g.Expect(err).NotTo(HaveOccurred())
				}
				confirmed := false
				for _, event := range cluster.events {
					confirmed = confirmed || event.args[0] == commandCreate
				}
				g.Expect(confirmed).To(Equal(test.confirm))
			})
		}
	}
}

func TestLatePauseCannotOutliveCleanup(t *testing.T) {
	for _, lateWins := range []bool{false, true} {
		t.Run(map[bool]string{false: "fence-first", true: "pause-after-cleanup-read"}[lateWins], func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			var pending string
			attemptedLateCommit := false
			r.command = func(ctx context.Context, input []byte, environment []string, stdout io.Writer, name string, args ...string) ([]byte, error) {
				if name == "oc" && args[0] == commandPatch && pending == "" {
					pending = args[slices.Index(args, "-p")+1]
					return nil, errSimulated
				}
				output, err := cluster.command(ctx, input, environment, stdout, name, args...)
				if name == "oc" && pending != "" && !attemptedLateCommit &&
					((lateWins && args[0] == commandGet) || (!lateWins && args[0] == commandPatch)) {
					attemptedLateCommit = true
					lateErr := cluster.patch(pending)
					if lateWins {
						g.Expect(lateErr).NotTo(HaveOccurred())
					} else {
						g.Expect(lateErr).To(MatchError(ContainSubstring("resourceVersion")))
					}
				}
				return output, err
			}
			err := r.run(t.Context())
			g.Expect(errors.Is(err, errSimulated)).To(BeTrue())
			g.Expect(err.Error()).NotTo(ContainSubstring("could not resume"))
			g.Expect(attemptedLateCommit).To(BeTrue())
			g.Expect(cluster.pool.Spec.Paused).To(BeFalse())
			g.Expect(cluster.pool.Annotations).NotTo(HaveKey(pauseOwner))
			g.Expect(cluster.changed).To(BeEmpty())
		})
	}
}

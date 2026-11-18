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
)

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
		WaitForMCP: true, BatchMCP: true, StartTimeout: time.Minute, RolloutTimeout: time.Hour,
	}
	cluster := &fakeCluster{mode: mode}
	cluster.pool.Generation, cluster.pool.ResourceVersion = 1, "1"
	cluster.pool.Spec.Configuration.Name = oldConfiguration
	cluster.pool.Spec.MachineConfigSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}}
	cluster.pool.Status.Configuration = cluster.pool.Spec.Configuration
	cluster.pool.Status.ObservedGeneration = 1
	cluster.pool.Status.Conditions = []metav1.Condition{{Type: "Updated", Status: metav1.ConditionTrue}}

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
		case "get":
			if lastSource >= 0 && event.paused {
				capture = index
			}
		}
	}
	g.Expect(cluster.changed).To(Equal([]string{"first", "second", "settings", "extra", "extra-settings"}))
	if patches != 2 || starts != 1 || lastSource < 0 || lastSource >= capture || capture >= resume {
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
	g.Expect(starts).To(Equal(applies))
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
		matches  bool
	}{
		{"missing-selector", nil, false},
		{"wrong-pool", &metav1.LabelSelector{MatchLabels: map[string]string{"role": "master"}}, false},
		{"matching-expression", &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: "role", Operator: metav1.LabelSelectorOpIn, Values: []string{"worker"}},
		}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			cluster.pool.Spec.MachineConfigSelector = test.selector
			err := r.run(t.Context())
			if test.matches {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(cluster.created).To(HaveLen(2))
				return
			}
			g.Expect(err).To(MatchError(ContainSubstring("is not selected by mcp/worker")))
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

func TestSequentialNoStartRequiresRenderEvidence(t *testing.T) {
	for _, test := range []struct {
		name, kind, message                              string
		unchanged, degraded, rendered, initiallyDegraded bool
	}{
		{name: "render-failure", kind: machineConfigKind, degraded: true, message: "render failed: invalid config"},
		{name: "unprocessed-change", kind: machineConfigKind, message: "no new rendered configuration or rollout observed"},
		{name: "unchanged", kind: machineConfigKind, unchanged: true},
		{name: "fast-rollout", kind: machineConfigKind, rendered: true},
		{name: "repair-degraded-config", kind: machineConfigKind, initiallyDegraded: true, rendered: true},
		{name: "unchanged-but-degraded", kind: machineConfigKind, unchanged: true, degraded: true, message: "render failed: invalid config"},
		{name: "kubelet-not-rendered", kind: "KubeletConfig", message: "no new rendered configuration or rollout observed"},
		{name: "runtime-not-rendered", kind: "ContainerRuntimeConfig", message: "no new rendered configuration or rollout observed"},
		{name: "mixed-changed-and-unchanged", kind: machineConfigKind, message: "no new rendered configuration or rollout observed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			r.options.BatchMCP = false
			r.options.Directory = t.TempDir()
			contents := "apiVersion: machineconfiguration.openshift.io/v1\nkind: " + test.kind + "\nmetadata:\n  name: config\n"
			if test.name == "mixed-changed-and-unchanged" {
				contents += "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: companion\n"
			}
			writeFile(t, filepath.Join(r.options.Directory, "config.yaml"), contents)
			cluster.startErr = errSimulated
			cluster.applyUnchanged = test.unchanged
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
					cluster.pool.Spec.Configuration.Name = newConfiguration
					cluster.pool.Status.Configuration = cluster.pool.Spec.Configuration
				}
			}
			if test.name == "mixed-changed-and-unchanged" {
				r.command = func(ctx context.Context, input []byte, environment []string, stdout io.Writer, name string, args ...string) ([]byte, error) {
					output, err := cluster.command(ctx, input, environment, stdout, name, args...)
					if name == "oc" && args[0] == actionApply {
						output = []byte("machineconfig/config configured\nconfigmap/companion unchanged\n")
					}
					return output, err
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

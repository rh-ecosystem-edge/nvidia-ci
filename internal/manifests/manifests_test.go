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
		WaitForMCP: true, BatchMCP: true, StartTimeout: time.Minute, RolloutTimeout: 20 * time.Millisecond,
	}
	cluster := &fakeCluster{mode: mode}
	cluster.pool.Name = options.PoolName
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
		"# empty document\n---\nnull\n---\napiVersion: machineconfiguration.openshift.io/v1\nkind: MachineConfig\nmetadata:\n  name: extra\n  labels:\n    role: worker\n"+
			"---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: extra-settings\n---\nnull\n---\n~\n")
	g.Expect(r.run(t.Context())).To(Succeed())
	g.Expect(cluster.pool.Spec.Paused).To(BeFalse())
	g.Expect(cluster.waited).To(Equal(cluster.created))
	var steps []string
	for _, event := range cluster.events {
		switch event.args[0] {
		case commandPatch:
			steps = append(steps, commandPatch)
		case commandCreate, actionApply:
			steps = append(steps, event.args[0])
			g.Expect(event.paused).To(BeTrue())
			if event.args[0] == commandCreate {
				g.Expect(event.args).To(ContainElement("--save-config"))
			}
			if strings.Contains(string(event.input), `"name":"first"`) {
				g.Expect(string(event.input)).To(And(ContainSubstring("/etc/test.rules"),
					ContainSubstring("data:text/plain;base64,dGVzdA==")))
			}
		case commandWait:
			g.Expect(event.args).NotTo(ContainElement("--for=condition=Updating=True"))
			if strings.Contains(event.args[2], ".spec.configuration.source") {
				steps = append(steps, commandWait)
			}
		case commandGet:
			if event.paused {
				steps = append(steps, commandGet)
			}
		}
	}
	g.Expect(cluster.changed).To(Equal([]string{firstConfigName, "second", "settings", "extra", "extra-settings"}))
	g.Expect(steps).To(Equal([]string{
		commandPatch, commandCreate, commandCreate, actionApply, commandCreate, actionApply,
		commandWait, commandWait, commandWait, commandGet, commandGet, commandPatch,
	}))
}

func TestBatchFailuresPreservePauseOwnership(t *testing.T) {
	for _, test := range []struct{ mode, message string }{
		{"apply-fail", "oc create --save-config"},
		{"pause-error", "oc patch mcp/worker"},
		{"render-timeout", ".spec.configuration.source"},
		{"stale", "has not converged"},
		{"cancel", "context canceled"},
		{alreadyPaused, "is already paused"},
		{"foreign-owner", "oc patch mcp/worker"},
		{"version-conflict", "testing value /metadata/resourceVersion failed"},
		{"resume-error", "could not resume mcp/worker"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			g := NewWithT(t)
			mode := test.mode
			r, cluster := testRunner(t, mode)
			cluster.pool.Spec.Paused = mode == alreadyPaused
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancel" {
				cluster.cancel = cancel
			}
			g.Expect(r.run(ctx)).To(MatchError(ContainSubstring(test.message)))
			g.Expect(cluster.pool.Spec.Paused).To(Equal(slices.Contains(
				[]string{alreadyPaused, "foreign-owner", "resume-error"}, mode)))
		})
	}
}

func TestInvalidInputsFailBeforeClusterChanges(t *testing.T) {
	t.Setenv("KUBECONFIG", "ambient-value-must-not-be-used")
	t.Setenv("RULES_BASE64", "ambient-value-must-not-be-used")
	for _, test := range []struct {
		name, message string
		configure     func(*runner)
	}{
		{"missing-kubeconfig", "kubeconfig must be supplied", func(r *runner) { r.options.Kubeconfig = "" }},
		{"missing-variable", "missing required variable RULES_BASE64", func(r *runner) { delete(r.options.Variables, "RULES_BASE64") }},
		{"kubeconfig-list", "must name a single file", func(r *runner) { r.options.Kubeconfig = "/first" + string(os.PathListSeparator) + "/second" }},
		{"invalid-document", "Object 'Kind' is missing", func(r *runner) {
			writeFile(t, filepath.Join(r.options.Directory, "00-first.yaml"), "metadata: {name: invalid}")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			test.configure(&r)
			g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring(test.message)))
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

func TestBatchChecksPoolSelectorBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name     string
		selector *metav1.LabelSelector
		message  string
	}{
		{"missing-selector", nil, "empty machineConfigSelector"},
		{"empty-selector", &metav1.LabelSelector{}, "empty machineConfigSelector"},
		{"wrong-pool", &metav1.LabelSelector{MatchLabels: map[string]string{"role": "master"}}, "is not selected by mcp/worker"},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			cluster.pool.Spec.MachineConfigSelector = test.selector
			g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring(test.message)))
			g.Expect(cluster.changed).To(BeEmpty())
			for _, event := range cluster.events {
				g.Expect(event.args[0]).NotTo(Equal(commandPatch))
			}
		})
	}
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
			configuration.Source = []poolSource{{Name: firstConfigName}}
			g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring("refusing to batch a reused name")))
			g.Expect(cluster.changed).To(BeEmpty())
			for _, event := range cluster.events {
				g.Expect(event.args[0]).NotTo(Equal(commandPatch))
			}
		})
	}
}

func TestBatchRejectsChangedRender(t *testing.T) {
	for _, test := range []struct {
		name, message string
		paused        bool
		change        func(*poolConfiguration)
	}{
		{"capture-source", "is missing MachineConfig/first", true, func(c *poolConfiguration) { c.Source = c.Source[1:] }},
		{"rollout-target", "instead of captured target " + newConfiguration, false, func(c *poolConfiguration) { c.Name = "rendered-unrelated" }},
		{"rollout-source", "lost MachineConfig/second", false, func(c *poolConfiguration) { c.Source = c.Source[:1] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			cluster.beforePoolRead = func() {
				if len(cluster.waited) == 2 && cluster.pool.Spec.Paused == test.paused {
					test.change(&cluster.pool.Spec.Configuration)
					cluster.beforePoolRead = nil
				}
			}
			g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring(test.message)))
			g.Expect(cluster.waited).To(Equal(cluster.created))
			g.Expect(cluster.pool.Spec.Paused).To(BeFalse())
		})
	}
}

func TestBatchWaitsForPoolStatus(t *testing.T) {
	for _, test := range []struct {
		name                string
		renderError, clears bool
	}{
		{"status-sources", false, true},
		{"stale-render-error", true, true},
		{"persistent-render-error", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			cluster.pool.Spec.Configuration.Source = []poolSource{{Name: firstConfigName}}
			if test.clears {
				r.options.RolloutTimeout = 2 * time.Second
			}
			if test.renderError {
				cluster.pool.Status.Configuration = cluster.pool.Spec.Configuration
				cluster.pool.Status.Conditions = append(cluster.pool.Status.Conditions,
					metav1.Condition{Type: "RenderDegraded", Status: metav1.ConditionTrue, Message: "previous render error"})
			}
			reads := 0
			cluster.beforePoolRead = func() {
				reads++
				if test.clears && reads == 2 {
					cluster.pool.Status.Configuration = cluster.pool.Spec.Configuration
					cluster.pool.Status.Conditions = cluster.pool.Status.Conditions[:1]
				}
			}
			err := r.waitForRollout(t.Context(), oldConfiguration, []string{firstConfigName})
			if test.clears {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(reads).To(Equal(2))
			} else {
				g.Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
				g.Expect(err).To(MatchError(ContainSubstring("previous render error")))
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

func TestLatePauseCannotOutliveCleanup(t *testing.T) {
	for _, test := range []struct {
		name                string
		lateWins, failFence bool
	}{
		{"fence-first", false, false},
		{"pause-after-cleanup-read", true, false},
		{"retry-failed-fence", false, true},
		{"pause-after-failed-fence", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			var pending string
			attemptedLateCommit, fenceFailed := false, false
			r.command = func(ctx context.Context, input []byte, environment []string, stdout io.Writer, name string, args ...string) ([]byte, error) {
				if name == "oc" && args[0] == commandPatch && pending == "" {
					pending = args[slices.Index(args, "-p")+1]
					return nil, errSimulated
				}
				if name == "oc" && args[0] == commandPatch && test.failFence && !fenceFailed {
					fenceFailed = true
					return nil, errSimulated
				}
				output, err := cluster.command(ctx, input, environment, stdout, name, args...)
				if name == "oc" && pending != "" && !attemptedLateCommit && (!test.failFence || fenceFailed) &&
					((test.lateWins && args[0] == commandGet) || (!test.lateWins && args[0] == commandPatch)) {
					attemptedLateCommit = true
					lateErr := cluster.patch(pending)
					if test.lateWins {
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
			g.Expect(fenceFailed).To(Equal(test.failFence))
			g.Expect(cluster.pool.Spec.Paused).To(BeFalse())
			g.Expect(cluster.pool.Annotations).NotTo(HaveKey(pauseOwner))
			g.Expect(cluster.changed).To(BeEmpty())
		})
	}
}

func TestCleanupReportsUnconfirmedPauseAtDeadline(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	r.command = func(ctx context.Context, input []byte, environment []string, stdout io.Writer, name string, args ...string) ([]byte, error) {
		if name == "oc" && args[0] == commandPatch {
			return nil, errSimulated
		}
		return cluster.command(ctx, input, environment, stdout, name, args...)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err := r.resumePool(ctx, "our-owner", cluster.pool.ResourceVersion, false)
	g.Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
	g.Expect(errors.Is(err, errSimulated)).To(BeTrue())
	g.Expect(err).To(MatchError(ContainSubstring("pause outcome could not be confirmed")))
}

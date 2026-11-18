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
	cluster.pool.Labels = map[string]string{"pool": "worker"}
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
	r, cluster := testRunner(t, "")
	cluster.existing = "second"
	g.Expect(r.run(t.Context())).To(Succeed())
	starts, applies := 0, 0
	for _, event := range cluster.events {
		g.Expect(event.args[0]).NotTo(BeElementOf(commandPatch, commandCreate))
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

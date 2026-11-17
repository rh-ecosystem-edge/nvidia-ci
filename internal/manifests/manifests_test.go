package manifests

import (
	"context"
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
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      files:
      - path: /etc/test.rules
        contents:
          source: data:text/plain;base64,$RULES_BASE64
`,
		"00-first.env-required": "RULES_BASE64\n",
		"10-second.yml":         "apiVersion: machineconfiguration.openshift.io/v1\nkind: MachineConfig\nmetadata:\n  name: second\n",
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
	cluster.pool.Status.Configuration = cluster.pool.Spec.Configuration
	cluster.pool.Status.ObservedGeneration = 1
	cluster.pool.Status.Conditions = []metav1.Condition{{Type: "Updated", Status: metav1.ConditionTrue}}

	return runner{options: options, command: cluster.command}, cluster
}

func TestFreshBatchIncludesAdditionalManifests(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	writeFile(t, filepath.Join(r.options.Directory, "30-extra.yaml"),
		"apiVersion: machineconfiguration.openshift.io/v1\nkind: MachineConfig\nmetadata:\n  name: extra\n"+
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
		case "wait":
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
	for _, mode := range []string{
		"apply-fail", "create-race", "pause-error", "render-timeout", "stale", "cancel", "cancel-apply",
		"already-paused", "foreign-owner", "version-conflict", "resume-error",
	} {
		t.Run(mode, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, mode)
			cluster.pool.Spec.Paused = mode == "already-paused"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if strings.HasPrefix(mode, "cancel") {
				cluster.cancel = cancel
			}
			g.Expect(r.run(ctx)).To(HaveOccurred())
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
		data, err := renderManifest(t.Context(), path, map[string]string{"ONE": "literal-$TWO", "TWO": "two"}, test.filter)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(data)).To(Equal(test.expected))
	}
	data, err := renderManifest(t.Context(), path, nil, "")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(data)).To(Equal("   ${UNCHANGED:-default}"))
}

func TestMissingEnvsubstFailsBeforeClusterOperations(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	t.Setenv("PATH", t.TempDir())
	g.Expect(r.run(t.Context())).To(MatchError(ContainSubstring("envsubst")))
	g.Expect(cluster.events).To(BeEmpty())
}

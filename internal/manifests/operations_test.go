package manifests

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDeleteUsesReverseOrderWithoutPoolChanges(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	r.options.Action = actionDelete
	r.options.PoolName, r.options.StartTimeout, r.options.RolloutTimeout = "", 0, 0
	g.Expect(r.run(t.Context())).To(Succeed())
	for _, event := range cluster.events {
		g.Expect(event.args[0]).To(Equal(actionDelete))
	}
	g.Expect(cluster.changed).To(Equal([]string{"settings", "second", "first"}))
}

func TestHooksUseSelectedKubeconfig(t *testing.T) {
	for _, ambient := range []string{"/other/cluster", ""} {
		t.Run("ambient="+ambient, func(t *testing.T) {
			g := NewWithT(t)
			t.Setenv("KUBECONFIG", ambient)
			if ambient == "" {
				g.Expect(os.Unsetenv("KUBECONFIG")).To(Succeed())
			}
			r, cluster := testRunner(t, "")
			r.options.WaitForMCP = false
			result := filepath.Join(t.TempDir(), "hook-result")
			t.Setenv("HOOK_RESULT", result)
			writeFile(t, filepath.Join(r.options.Directory, "00-first.sh"),
				`printf '%s' "$KUBECONFIG" > "$HOOK_RESULT"`)
			r.command = func(ctx context.Context, input []byte, environment []string, stdout io.Writer, name string, args ...string) ([]byte, error) {
				if name == "bash" {
					g.Expect(stdout).To(Equal(os.Stdout))

					return executeCommand(ctx, input, environment, stdout, name, args...)
				}

				return cluster.command(ctx, input, environment, stdout, name, args...)
			}
			g.Expect(r.run(t.Context())).To(Succeed())
			data, err := os.ReadFile(result)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(data)).To(Equal(r.options.Kubeconfig))
		})
	}
}

func TestStreamingCommandCancellationDoesNotWaitForBackgroundChild(t *testing.T) {
	g := NewWithT(t)
	read, write, err := os.Pipe()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = read.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := executeCommand(ctx, nil, nil, write, "bash", "-c", `sleep 30 & printf '%s\n' "$!"; wait`)
		_ = write.Close()
		done <- err
	}()
	line, err := bufio.NewReader(read).ReadString('\n')
	g.Expect(err).NotTo(HaveOccurred())
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	g.Expect(err).NotTo(HaveOccurred())
	child, err := os.FindProcess(pid)
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = child.Kill() })
	cancel()
	select {
	case err := <-done:
		g.Expect(err).To(MatchError(ContainSubstring(context.Canceled.Error())))
	case <-time.After(2 * time.Second):
		t.Fatal("canceled hook is still waiting for its background child")
	}
	g.Expect(read.SetReadDeadline(time.Now().Add(time.Second))).To(Succeed())
	_, err = read.Read(make([]byte, 1))
	g.Expect(err).To(MatchError(io.EOF))
}

func TestUnsupportedKindsUseSequentialWaits(t *testing.T) {
	for _, test := range []struct{ name, resource string }{
		{"kubelet", `{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"KubeletConfig","metadata":{"name":"kubelet"},"spec":{"machineConfigPoolSelector":{"matchLabels":{"pool":"worker"}}}}`},
		{"runtime", `{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"ContainerRuntimeConfig","metadata":{"name":"runtime"},"spec":{"machineConfigPoolSelector":{"matchLabels":{"pool":"worker"}}}}`},
		{"custom", `{"apiVersion":"example.com/v1","kind":"MachineConfig","metadata":{"name":"custom"}}`},
		{"mixed-list", `{"apiVersion":"v1","kind":"List","items":[
{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"MachineConfig","metadata":{"name":"first","labels":{"role":"worker"}}},
{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"KubeletConfig","metadata":{"name":"kubelet"},"spec":{"machineConfigPoolSelector":{"matchLabels":{"pool":"worker"}}}}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			r.options.Directory = t.TempDir()
			writeFile(t, filepath.Join(r.options.Directory, "config.yaml"), test.resource)
			g.Expect(r.run(t.Context())).To(Succeed())
			applies, waits := 0, 0
			for _, event := range cluster.events {
				g.Expect(event.args[0]).NotTo(Equal(commandPatch))
				if event.args[0] == commandCreate {
					g.Expect(string(event.input)).To(ContainSubstring("zz-nvidia-ci-render-"))
				}
				switch event.args[0] {
				case actionApply:
					applies++
				case commandWait:
					waits++
				}
			}
			g.Expect(applies).To(Equal(1))
			g.Expect(waits).To(Equal(2))
		})
	}
}

func TestHooksRunBeforeRenderingTheNextFile(t *testing.T) {
	g := NewWithT(t)
	r, cluster := testRunner(t, "")
	r.options.Directory = t.TempDir()
	first := filepath.Join(r.options.Directory, "00-first.yaml")
	later := filepath.Join(r.options.Directory, "10-later.yaml")
	writeFile(t, first, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: first\n")
	writeFile(t, later, "invalid until the hook runs")
	writeFile(t, sibling(first, ".sh"), "hook")
	cluster.hook = func() error {
		return os.WriteFile(later, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: after-hook\n"), 0o600)
	}
	g.Expect(r.run(t.Context())).To(Succeed())
	for _, event := range cluster.events {
		g.Expect(event.args[0]).NotTo(Equal(commandPatch))
	}
	g.Expect(cluster.changed).To(Equal([]string{"first", "after-hook"}))
}

func TestUnrelatedResourcesAndDisabledWaitsDoNotTouchPool(t *testing.T) {
	g := NewWithT(t)
	for _, wait := range []bool{true, false} {
		r, cluster := testRunner(t, "")
		r.options.WaitForMCP = wait
		if !wait {
			r.options.PoolName, r.options.StartTimeout, r.options.RolloutTimeout = "", 0, 0
		}
		if wait {
			r.options.Directory = t.TempDir()
			writeFile(t, filepath.Join(r.options.Directory, "config.yaml"),
				"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: unrelated\n")
		}
		g.Expect(r.run(t.Context())).To(Succeed())
		for _, event := range cluster.events {
			g.Expect([]string{actionApply, commandCreate}).To(ContainElement(event.args[0]))
		}
	}
}

func TestListsCannotHideMachineConfigs(t *testing.T) {
	g := NewWithT(t)
	resources, err := decodeResources([]byte(`{"apiVersion":"v1","kind":"List","items":[
{"apiVersion":"v1","kind":"List","items":[{"apiVersion":"machineconfiguration.openshift.io/v1",
"kind":"MachineConfig","metadata":{"name":"nested"},"spec":{"kernelArguments":["test"]}}]},
{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"sibling"}}]}`))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resources).To(HaveLen(2))
	g.Expect(isMachineConfig(resources[0])).To(BeTrue())
	g.Expect(resources[0].GetName()).To(Equal("nested"))
	g.Expect(resources[1].GetName()).To(Equal("sibling"))
	arguments, _, err := unstructured.NestedStringSlice(resources[0].Object, "spec", "kernelArguments")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(arguments).To(Equal([]string{"test"}))
}

func TestTypedListsInheritMissingItemTypes(t *testing.T) {
	g := NewWithT(t)
	resources, err := decodeResources([]byte(`{"apiVersion":"v1","kind":"List","items":[
{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"MachineConfigList","items":[
{"metadata":{"name":"inherited"},"spec":{"kernelArguments":["preserved"]}},
{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"explicit"}}]}]}`))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resources).To(HaveLen(2))
	g.Expect(isMachineConfig(resources[0])).To(BeTrue())
	g.Expect(resources[0].GetName()).To(Equal("inherited"))
	arguments, _, err := unstructured.NestedStringSlice(resources[0].Object, "spec", "kernelArguments")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(arguments).To(Equal([]string{"preserved"}))
	g.Expect(resources[1].GetAPIVersion()).To(Equal("v1"))
	g.Expect(resources[1].GetKind()).To(Equal("ConfigMap"))
	g.Expect(resources[1].GetName()).To(Equal("explicit"))
}

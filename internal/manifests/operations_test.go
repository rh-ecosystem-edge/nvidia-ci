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
	g.Expect(cluster.changed).To(Equal([]string{"settings", "second", firstConfigName}))
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
			result := filepath.Join(t.TempDir(), "hook-result")
			t.Setenv("HOOK_RESULT", result)
			later := filepath.Join(r.options.Directory, "10-second.yml")
			t.Setenv("HOOK_MANIFEST", later)
			writeFile(t, later, "invalid until the hook runs")
			writeFile(t, filepath.Join(r.options.Directory, "00-first.sh"),
				`printf '%s' "$KUBECONFIG" > "$HOOK_RESULT"
printf '%s' '{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"after-hook"}}' > "$HOOK_MANIFEST"`)
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
			g.Expect(cluster.changed).To(Equal([]string{firstConfigName, "after-hook", "settings"}))
			for _, event := range cluster.events {
				g.Expect(event.args[0]).NotTo(BeElementOf(commandPatch, commandCreate))
			}
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

func TestIneligibleRunsUseSequentialWaits(t *testing.T) {
	for _, test := range []struct{ name, resource string }{
		{"existing", ""},
		{"kubelet", `{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"KubeletConfig","metadata":{"name":"kubelet"},"spec":{"machineConfigPoolSelector":{"matchLabels":{"pool":"worker"}}}}`},
		{"runtime", `{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"ContainerRuntimeConfig","metadata":{"name":"runtime"},"spec":{"machineConfigPoolSelector":{"matchLabels":{"pool":"worker"}}}}`},
		{"custom", `{"apiVersion":"example.com/v1","kind":"MachineConfig","metadata":{"name":"custom"}}`},
		{"list", `{"apiVersion":"v1","kind":"List","items":[
{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"MachineConfig","metadata":{"name":"listed"}}]}`},
		{"typed-list", `{"apiVersion":"machineconfiguration.openshift.io/v1","kind":"MachineConfigList","items":[{"metadata":{"name":"listed"}}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			r, cluster := testRunner(t, "")
			expectedApplies := 3
			if test.resource == "" {
				cluster.existing = "second"
			} else {
				expectedApplies++
				writeFile(t, filepath.Join(r.options.Directory, "30-unsupported.yaml"), test.resource)
			}
			g.Expect(r.run(t.Context())).To(Succeed())
			var inputs []string
			for index, event := range cluster.events {
				g.Expect(event.args[0]).NotTo(BeElementOf(commandPatch, commandCreate))
				g.Expect(event.paused).To(BeFalse())
				if event.args[0] == actionApply {
					inputs = append(inputs, string(event.input))
					g.Expect(cluster.events[index+1].args).To(ContainElement("--for=condition=Updating=True"))
					g.Expect(cluster.events[index+2].args).To(ContainElement("--for=condition=Updated=True"))
				}
			}
			g.Expect(inputs).To(HaveLen(expectedApplies))
			if test.resource != "" {
				g.Expect(inputs[len(inputs)-1]).To(Equal(test.resource))
			}
		})
	}
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

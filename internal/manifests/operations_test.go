package manifests

import (
	"os"
	"path/filepath"
	"testing"

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

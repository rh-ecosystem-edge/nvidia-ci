package nvidianetwork

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStagingDockerConfigJSONKeepsOnlyStagingRegistry(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(dockerConfigJSON{
		Auths: map[string]dockerAuthEntry{
			"quay.io": {Auth: "cXVheQ=="},
			"registry.stage.redhat.io": {
				Auth: "c3RhZ2U=",
			},
			"https://registry.stage.redhat.io/v2/": {
				Username: "user",
				Password: "pass",
			},
			"registry.redhat.io": {Auth: "cHJvZA=="},
		},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	scoped, err := stagingDockerConfigJSON(raw)
	if err != nil {
		t.Fatalf("stagingDockerConfigJSON returned error: %v", err)
	}

	var got dockerConfigJSON
	if err := json.Unmarshal(scoped, &got); err != nil {
		t.Fatalf("unmarshal scoped secret: %v", err)
	}
	if _, ok := got.Auths["quay.io"]; ok {
		t.Fatal("scoped pull-secret still contains quay.io")
	}
	if _, ok := got.Auths["registry.redhat.io"]; ok {
		t.Fatal("scoped pull-secret still contains registry.redhat.io")
	}
	if _, ok := got.Auths["registry.stage.redhat.io"]; !ok {
		t.Fatal("scoped pull-secret missing registry.stage.redhat.io")
	}
	if _, ok := got.Auths["https://registry.stage.redhat.io/v2/"]; !ok {
		t.Fatal("scoped pull-secret missing https://registry.stage.redhat.io/v2/")
	}
}

func TestStagingDockerConfigJSONRequiresStagingAuth(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(dockerConfigJSON{
		Auths: map[string]dockerAuthEntry{
			"quay.io": {Auth: "cXVheQ=="},
		},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	_, err = stagingDockerConfigJSON(raw)
	if err == nil {
		t.Fatal("expected error when staging registry auth is missing")
	}
	if !strings.Contains(err.Error(), precompiledOFEDRegistry) {
		t.Errorf("error = %q, want it to mention %s", err.Error(), precompiledOFEDRegistry)
	}
}

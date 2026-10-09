package nvidianetwork

import "testing"

func TestNodeOSTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		labels         map[string]string
		clusterVersion string
		want           string
		wantErr        bool
	}{
		{
			name: "rhel release labels",
			labels: map[string]string{
				nodeOSReleaseIDLabel:        "rhel",
				nodeOSReleaseVersionIDLabel: "9.8",
			},
			clusterVersion: "4.22.15",
			want:           "rhel9.8",
		},
		{
			name: "legacy rhcos release labels",
			labels: map[string]string{
				nodeOSReleaseIDLabel:        "rhcos",
				nodeOSReleaseVersionIDLabel: "4.16",
			},
			clusterVersion: "4.22.15",
			want:           "rhcos4.16",
		},
		{
			name:           "missing labels fall back to openshift minor",
			clusterVersion: "4.22.15-multi",
			want:           "rhcos4.22",
		},
		{
			name: "partial labels fall back to openshift minor",
			labels: map[string]string{
				nodeOSReleaseIDLabel: "rhel",
			},
			clusterVersion: "4.22.15",
			want:           "rhcos4.22",
		},
		{
			name:    "no labels or usable openshift minor",
			labels:  map[string]string{},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := NodeOSTag(test.labels, test.clusterVersion)
			if test.wantErr {
				if err == nil {
					t.Fatal("NodeOSTag() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NodeOSTag() error = %v", err)
			}
			if got != test.want {
				t.Errorf("NodeOSTag() = %q, want %q", got, test.want)
			}
		})
	}
}

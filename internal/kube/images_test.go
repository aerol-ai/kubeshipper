package kube

import "testing"

func TestTrackedImageForWatch(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{name: "plain tag", image: "ghcr.io/acme/agent:latest", want: "ghcr.io/acme/agent:latest"},
		{name: "tag with digest", image: "ghcr.io/acme/agent:latest@" + oldDigest, want: "ghcr.io/acme/agent:latest"},
		{name: "digest only", image: "ghcr.io/acme/agent@" + oldDigest, want: "ghcr.io/acme/agent@" + oldDigest},
	}
	for _, tc := range tests {
		if got := trackedImageForWatch(tc.image); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPinImageToDigest(t *testing.T) {
	tests := []struct {
		name   string
		image  string
		digest string
		want   string
	}{
		{name: "explicit tag", image: "ghcr.io/acme/agent:latest", digest: newDigest, want: "ghcr.io/acme/agent:latest@" + newDigest},
		{name: "implicit latest", image: "ghcr.io/acme/agent", digest: newDigest, want: "ghcr.io/acme/agent:latest@" + newDigest},
		{name: "digest only", image: "ghcr.io/acme/agent@" + oldDigest, digest: newDigest, want: "ghcr.io/acme/agent@" + newDigest},
	}
	for _, tc := range tests {
		if got := pinImageToDigest(tc.image, tc.digest); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

const (
	oldDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	newDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

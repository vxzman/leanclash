//go:build !container

package systemd

import (
	"testing"

	"leanclash/internal/service"
)

func TestClientImplementsInstanceLister(t *testing.T) {
	var _ service.InstanceLister = (*Client)(nil)
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"mihomo@tun":         "mihomo@tun.service",
		"mihomo@tproxy":      "mihomo@tproxy.service",
		"mihomo@tun.service": "mihomo@tun.service",
		"foo.socket":         "foo.socket",
		"plain-service":      "plain-service.service",
		"":                   "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

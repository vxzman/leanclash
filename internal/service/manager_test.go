package service

import "testing"

func TestMihomoInstance(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"mihomo@tun.service", "tun", true},
		{"mihomo@tun", "tun", true},
		{"mihomo@redir-tproxy.service", "redir-tproxy", true},
		{"mihomo@redir-tproxy", "redir-tproxy", true},
		{"mihomo@.service", "", false},
		{"other.service", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := MihomoInstance(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("MihomoInstance(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

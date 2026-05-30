package safenet

import (
	"strings"
	"testing"
)

// The existing IP-block tests use IPv4 literals only. validateIP relies on
// net.IP classification that also covers IPv6, and ValidateURL accepts
// bracketed IPv6 literals via url.Hostname(); these lock in that IPv6
// loopback / unique-local / link-local / multicast / unspecified / mapped
// cloud-metadata addresses are blocked.
func TestValidateURLBlocksIPv6Addresses(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"loopback ::1", "http://[::1]/meta"},
		{"unique-local fc00::/7", "https://[fc00::1]/"},
		{"link-local fe80::/10", "http://[fe80::1]/"},
		{"link-local multicast ff02::1", "http://[ff02::1]/"},
		{"unspecified ::", "http://[::]/"},
		{"v4-mapped cloud metadata", "http://[::ffff:169.254.169.254]/latest/meta-data/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateURL(tc.url)
			if err == nil {
				t.Fatalf("ValidateURL(%q) = nil, want blocked", tc.url)
			}
			if !strings.Contains(err.Error(), "blocked address") {
				t.Fatalf("ValidateURL(%q) error = %q, want a blocked-address error", tc.url, err.Error())
			}
		})
	}
}

func TestValidateURLAllowsPublicIPv6(t *testing.T) {
	// A public IPv6 literal (Google public DNS) must pass the literal-IP guard.
	if err := ValidateURL("https://[2001:4860:4860::8888]/"); err != nil {
		t.Fatalf("ValidateURL(public IPv6) = %v, want nil", err)
	}
}

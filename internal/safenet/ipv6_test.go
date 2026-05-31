package safenet

import (
	"net"
	"net/netip"
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

// An IPv6 literal can wrap a private/metadata IPv4 via IPv4-compatible, 6to4,
// or a non-well-known NAT64 prefix. net.IP's classifiers do NOT see the
// embedded v4 (To4() is nil for these forms), so validateIP decodes and
// re-validates the embedded address. These are literal IPs (no DNS stub).
func TestValidateURLBlocksEmbeddedV4(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"ipv4-compatible metadata", "http://[::169.254.169.254]/"},
		{"ipv4-compatible private", "http://[::10.0.0.1]/"},
		{"6to4 metadata", "http://[2002:a9fe:a9fe::]/"},
		{"6to4 private 10.0.0.1", "http://[2002:0a00:0001::]/"},
		// NAT64 well-known prefix metadata: blocked wholesale by the
		// 64:ff9b::/96 prefix table entry (NSP forms are intentionally not
		// decoded — see TestValidateURLAllowsEmbeddedV4Public).
		{"nat64 well-known metadata", "http://[64:ff9b::a9fe:a9fe]/"},
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

// The embedded-v4 decode must not over-block: a v6 literal whose embedded v4 is
// public, and a generic public v6 (no embedded v4 at all), must still pass.
func TestValidateURLAllowsEmbeddedV4Public(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"6to4 of public 93.184.216.34", "http://[2002:5db8:d822::]/"},
		{"ipv4-compatible of public 93.184.216.34", "http://[::5db8:d822]/"},
		{"generic public v6 (no embedded v4)", "http://[2001:4860:4860::8888]/"},
		// NAT64 network-specific-prefix literals must NOT be rejected — the
		// embedded-v4 position is unknowable from the literal, so decoding any
		// fixed layout would over-block legitimate public translations (Codex P2).
		// These are left to best-effort; both a public-looking and a /48-encoded
		// form must pass the local guard.
		{"nat64 nsp form (low-32 public)", "http://[64:ff9b:1::808:808]/"},
		{"nat64 nsp /48-encoded public 8.8.8.8", "http://[64:ff9b:1:808:8:800::]/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateURL(tc.url); err != nil {
				t.Fatalf("ValidateURL(%q) = %v, want allowed", tc.url, err)
			}
		})
	}
}

func TestEmbeddedV4CandidatesUnit(t *testing.T) {
	cases := []struct {
		addr string
		want string // expected single candidate, or "" for none
	}{
		{"::169.254.169.254", "169.254.169.254"},
		{"2002:a9fe:a9fe::", "169.254.169.254"},
		{"64:ff9b:1::a9fe:a9fe", ""}, // NAT64 NSP not decoded (WKP handled by the prefix table)
		{"2001:4860:4860::8888", ""}, // public v6: no candidate, proves no over-block
	}
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			got := embeddedV4Candidates(netip.MustParseAddr(tc.addr))
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("embeddedV4Candidates(%s) = %v, want none", tc.addr, got)
				}
				return
			}
			// NAT64 emits several candidates (one per RFC 6052 prefix length);
			// the expected v4 must be among them.
			found := false
			for _, ip := range got {
				if ip.Equal(net.ParseIP(tc.want)) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("embeddedV4Candidates(%s) = %v, want to contain %s", tc.addr, got, tc.want)
			}
		})
	}
}

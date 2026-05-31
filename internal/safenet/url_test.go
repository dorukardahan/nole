package safenet

import (
	"errors"
	"net"
	"testing"
)

func withLookupIP(t *testing.T, fn func(string) ([]net.IP, error)) {
	t.Helper()
	old := lookupIP
	lookupIP = fn
	t.Cleanup(func() { lookupIP = old })
}

func TestValidateURLAcceptsPublicHTTPS(t *testing.T) {
	withLookupIP(t, func(host string) ([]net.IP, error) {
		if host != "example.com" {
			t.Fatalf("unexpected lookup host %q", host)
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	if err := ValidateURL("https://example.com/page"); err != nil {
		t.Fatalf("expected public URL to be allowed: %v", err)
	}
}

func TestValidateURLAcceptsPublicHTTP(t *testing.T) {
	withLookupIP(t, func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	if err := ValidateURL("http://example.com"); err != nil {
		t.Fatalf("expected public HTTP URL to be allowed: %v", err)
	}
}

func TestValidateURLBlocksHostResolvingToPrivateIP(t *testing.T) {
	withLookupIP(t, func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.10")}, nil
	})
	if err := ValidateURL("https://internal.example"); err == nil {
		t.Fatal("expected hostname resolving to private IP to be blocked")
	}
}

func TestValidateURLBlocksDNSFailures(t *testing.T) {
	withLookupIP(t, func(host string) ([]net.IP, error) {
		return nil, errors.New("no such host")
	})
	if err := ValidateURL("https://does-not-resolve.example"); err == nil {
		t.Fatal("expected DNS failure to be blocked")
	}
}

func TestValidateURLBlocksFileScheme(t *testing.T) {
	err := ValidateURL("file:///etc/passwd")
	if err == nil {
		t.Fatal("expected file:// to be blocked")
	}
}

func TestValidateURLBlocksFTP(t *testing.T) {
	err := ValidateURL("ftp://example.com/file")
	if err == nil {
		t.Fatal("expected ftp:// to be blocked")
	}
}

func TestValidateURLBlocksLocalhost(t *testing.T) {
	err := ValidateURL("http://localhost:8080/secret")
	if err == nil {
		t.Fatal("expected localhost to be blocked")
	}
}

func TestValidateURLBlocksLoopbackIP(t *testing.T) {
	err := ValidateURL("http://127.0.0.1/admin")
	if err == nil {
		t.Fatal("expected 127.0.0.1 to be blocked")
	}
}

func TestValidateURLBlocksPrivateIP10(t *testing.T) {
	err := ValidateURL("http://10.0.0.1/internal")
	if err == nil {
		t.Fatal("expected 10.x to be blocked")
	}
}

func TestValidateURLBlocksPrivateIP172(t *testing.T) {
	err := ValidateURL("http://172.16.0.1/internal")
	if err == nil {
		t.Fatal("expected 172.16.x to be blocked")
	}
}

func TestValidateURLBlocksPrivateIP192(t *testing.T) {
	err := ValidateURL("http://192.168.1.1/router")
	if err == nil {
		t.Fatal("expected 192.168.x to be blocked")
	}
}

func TestValidateURLBlocksCloudMetadata(t *testing.T) {
	err := ValidateURL("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("expected cloud metadata IP to be blocked")
	}
}

func TestValidateURLBlocksEmptyHost(t *testing.T) {
	err := ValidateURL("http:///path")
	if err == nil {
		t.Fatal("expected empty host to be blocked")
	}
}

func TestValidateIPRejectsUnspecified(t *testing.T) {
	err := validateIP(net.IPv4zero)
	if err == nil {
		t.Fatal("expected 0.0.0.0 to be blocked")
	}
}

func TestValidateURLBlocksMulticastIP(t *testing.T) {
	err := ValidateURL("http://239.1.2.3/stream")
	if err == nil {
		t.Fatal("expected multicast IP to be blocked")
	}
}

func TestValidateURLBlocksExtraReservedRanges(t *testing.T) {
	// Audit #3: ranges that Go's net.IP classifiers (IsPrivate etc.) do NOT
	// cover but are still unsafe SSRF targets. These are literal IPs, so they
	// hit the deterministic literal-IP guard and need no DNS stub.
	cases := []struct {
		name string
		url  string
	}{
		{"CGNAT 100.64.0.0/10", "http://100.64.0.1/"},
		{"this-network 0.0.0.0/8", "http://0.1.2.3/"},
		{"IETF protocol 192.0.0.0/24", "http://192.0.0.10/"},
		{"benchmarking 198.18.0.0/15", "http://198.18.0.1/"},
		{"benchmarking 198.19.x", "http://198.19.255.255/"},
		{"NAT64 64:ff9b::/96", "http://[64:ff9b::1]/"},
		{"documentation 2001:db8::/32", "http://[2001:db8::1]/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateURL(tc.url); err == nil {
				t.Fatalf("ValidateURL(%q) = nil, want blocked", tc.url)
			}
		})
	}
}

func TestValidateURLAllowsPublicIPsOutsideExtraRanges(t *testing.T) {
	// Audit #3 must not false-positive on real public addresses. These are
	// literal IPs, so no DNS stub is needed.
	cases := []struct {
		name string
		url  string
	}{
		{"example.com IP 93.184.216.34", "http://93.184.216.34/"},
		{"google dns 8.8.8.8", "http://8.8.8.8/"},
		{"cloudflare dns 1.1.1.1", "http://1.1.1.1/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateURL(tc.url); err != nil {
				t.Fatalf("ValidateURL(%q) = %v, want allowed", tc.url, err)
			}
		})
	}
}

func TestValidateURLBlocksNumericHostParserDifferential(t *testing.T) {
	// Audit #4: numeric IP attempts that fail strict net.ParseIP but resolve
	// to 127.0.0.1 under libc/inet_aton. The block is pre-DNS, so stub lookupIP
	// to fail loudly if any case wrongly falls through to resolution.
	withLookupIP(t, func(host string) ([]net.IP, error) {
		t.Fatalf("numeric host %q must be blocked before DNS, but lookupIP was called", host)
		return nil, nil
	})
	cases := []struct {
		name string
		url  string
	}{
		{"octal dotted 0177.0.0.1", "http://0177.0.0.1/"},
		{"hex label 0x7f.0.0.1", "http://0x7f.0.0.1/"},
		{"single octal 017700000001", "http://017700000001/"},
		{"non-octal-decimal 08.0.0.1", "http://08.0.0.1/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateURL(tc.url); err == nil {
				t.Fatalf("ValidateURL(%q) = nil, want blocked", tc.url)
			}
		})
	}
}

func TestValidateURLAllowsTrickyHostnames(t *testing.T) {
	// Audit #4 must not block real hostnames that merely contain digits. Each
	// resolves to a public IP via the stubbed resolver.
	hosts := []string{"3com.com", "123abc.com", "a1.b2.c3", "sub.example.com"}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			called := false
			withLookupIP(t, func(h string) ([]net.IP, error) {
				called = true
				if h != host {
					t.Fatalf("unexpected lookup host %q, want %q", h, host)
				}
				return []net.IP{net.ParseIP("93.184.216.34")}, nil
			})
			if err := ValidateURL("http://" + host + "/"); err != nil {
				t.Fatalf("ValidateURL(%q) = %v, want allowed", host, err)
			}
			if !called {
				t.Fatalf("expected %q to reach DNS resolution, but lookupIP was not called", host)
			}
		})
	}
}

func TestIsBlockedHost(t *testing.T) {
	tests := []struct {
		host    string
		blocked bool
	}{
		{"localhost", true},
		{"LOCALHOST", true},
		{"localhost.", true},
		{"localhost.localdomain", true},
		{"example.com", false},
		{"", false},
	}
	for _, tt := range tests {
		got := IsBlockedHost(tt.host)
		if got != tt.blocked {
			t.Errorf("IsBlockedHost(%q) = %v, want %v", tt.host, got, tt.blocked)
		}
	}
}

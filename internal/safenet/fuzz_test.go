package safenet

import (
	"context"
	"net"
	"net/url"
	"testing"
)

// FuzzValidateURL exercises the SSRF preflight against arbitrary input. It runs
// the seed corpus deterministically under plain `go test` (the gate) and the
// mutation engine under `go test -fuzz=FuzzValidateURL`.
//
// The resolver is stubbed so every iteration is hermetic (no real DNS) and we
// can assert the pre-DNS fail-closed contract: any host that IsBlockedHost
// flags, or a literal IP that validateIP rejects, or a looksLikeNumericIP
// parser-differential host, MUST yield a non-nil error AND must never reach the
// resolver. This guards against a future refactor leaking a blocked host into
// lookupIP.
func FuzzValidateURL(f *testing.F) {
	seeds := []string{
		"https://example.com",
		"http://example.com/path?a=1&b=2",
		"http://127.0.0.1",
		"https://169.254.169.254/latest/meta-data/",
		"http://0177.0.0.1",
		"http://0x7f.0.0.1",
		"http://[::1]",
		"http://[::169.254.169.254]",
		"http://[2002:a9fe:a9fe::]",
		"http://[64:ff9b:1::a9fe:a9fe]",
		"ftp://example.com",
		"http://localhost",
		"http://100.64.0.1",
		"not a url",
		"",
		"http://",
		"https://xn--r8jz45g.xn--zckzah",
		"http://[fe80::1%25eth0]",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		// Hermetic resolver: never hit real DNS; record if it was reached.
		var resolved bool
		old := lookupIP
		lookupIP = func(context.Context, string) ([]net.IP, error) {
			resolved = true
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		defer func() { lookupIP = old }()

		err := ValidateURL(raw) // must never panic

		// Re-derive the pre-DNS blocked condition exactly as ValidateURL does,
		// using the real helpers so this stays in lock-step with the guard.
		u, perr := url.Parse(raw)
		if perr != nil {
			return
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return
		}
		host := u.Hostname()
		if host == "" {
			return
		}

		blockedPreDNS := IsBlockedHost(host)
		if ip := net.ParseIP(host); ip != nil {
			if validateIP(ip) != nil {
				blockedPreDNS = true
			}
		} else if looksLikeNumericIP(host) {
			blockedPreDNS = true
		}

		if blockedPreDNS {
			if err == nil {
				t.Fatalf("ValidateURL(%q) = nil, want blocked (fail-closed)", raw)
			}
			if resolved {
				t.Fatalf("ValidateURL(%q) reached the resolver for a pre-DNS-blocked host", raw)
			}
		}
	})
}

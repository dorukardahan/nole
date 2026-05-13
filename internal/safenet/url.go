package safenet

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

var lookupIP = net.LookupIP

// ValidateURL checks that a URL is safe to fetch (no SSRF targets).
// Blocks non-http(s) schemes, obvious local hostnames, loopback, private IPs,
// link-local addresses, unspecified addresses, and cloud metadata IPs.
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q: only http and https are allowed", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}
	if IsBlockedHost(host) {
		return fmt.Errorf("blocked host %q", host)
	}

	// Literal IPs can be checked without DNS, keeping the guard deterministic.
	if ip := net.ParseIP(host); ip != nil {
		if err := validateIP(ip); err != nil {
			return fmt.Errorf("host %q is a blocked address: %w", host, err)
		}
		return nil
	}

	// Hostnames are resolved and every answer must be public. Blocking on DNS
	// failures is safer than letting providers fetch ambiguous targets.
	ips, err := lookupIP(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		if err := validateIP(ip); err != nil {
			return fmt.Errorf("host %q resolves to blocked address: %w", host, err)
		}
	}

	return nil
}

func validateIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("loopback address %s is not allowed", ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("private address %s is not allowed", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local address %s is not allowed", ip)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified address %s is not allowed", ip)
	}
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("cloud metadata address %s is not allowed", ip)
	}
	return nil
}

// IsBlockedHost performs a quick check on hostname without DNS resolution.
// Useful for rejecting obvious internal hostnames before resolver lookup.
func IsBlockedHost(host string) bool {
	lower := strings.TrimSuffix(strings.ToLower(host), ".")
	blocked := []string{"localhost", "localhost.localdomain"}
	for _, b := range blocked {
		if lower == b {
			return true
		}
	}
	return false
}

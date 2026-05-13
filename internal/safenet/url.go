package safenet

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateURL checks that a URL is safe to fetch (no SSRF targets).
// Blocks: file:// scheme, loopback, private IPs, link-local, cloud metadata.
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

	// Resolve hostname to IP(s)
	ips, err := net.LookupIP(host)
	if err != nil {
		// If resolution fails, block to be safe
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
	// Loopback (127.0.0.0/8, ::1)
	if ip.IsLoopback() {
		return fmt.Errorf("loopback address %s is not allowed", ip)
	}
	// Private (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7)
	if ip.IsPrivate() {
		return fmt.Errorf("private address %s is not allowed", ip)
	}
	// Link-local (169.254.0.0/16, fe80::/10)
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local address %s is not allowed", ip)
	}
	// Unspecified (0.0.0.0, ::)
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified address %s is not allowed", ip)
	}
	// Cloud metadata (169.254.169.254)
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("cloud metadata address %s is not allowed", ip)
	}
	return nil
}

// IsBlockedHost performs a quick check on hostname without DNS resolution.
// Useful for cases where you want to reject obvious internal hostnames.
func IsBlockedHost(host string) bool {
	lower := strings.ToLower(host)
	blocked := []string{"localhost", "localhost.localdomain"}
	for _, b := range blocked {
		if lower == b {
			return true
		}
	}
	return false
}

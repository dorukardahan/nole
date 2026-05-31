package safenet

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

var lookupIP = net.LookupIP

// extraBlockedPrefixes covers ranges that Go's net.IP classifiers (IsPrivate,
// IsLoopback, IsLinkLocal*, IsMulticast, IsUnspecified) do NOT reject but that
// are still unsafe SSRF targets: shared/CGNAT space, "this network", IETF
// protocol assignments, benchmarking ranges, NAT64, and documentation ranges.
// They are checked alongside the existing net.IP classifiers in validateIP.
var extraBlockedPrefixes = func() []netip.Prefix {
	cidrs := []string{
		"100.64.0.0/10", // RFC 6598 shared address space (CGNAT)
		"0.0.0.0/8",     // RFC 1122 "this network"
		"192.0.0.0/24",  // RFC 6890 IETF protocol assignments
		"198.18.0.0/15", // RFC 2544 benchmarking
		"64:ff9b::/96",  // RFC 6052 NAT64 well-known prefix
		"2001:db8::/32", // RFC 3849 documentation
	}
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		prefixes = append(prefixes, netip.MustParsePrefix(c))
	}
	return prefixes
}()

// ValidateURL performs a local, best-effort URL preflight before Nólë asks a
// provider to fetch a URL. It blocks non-http(s) schemes, obvious local
// hostnames, loopback, private IPs, link-local addresses, multicast,
// unspecified addresses, and cloud metadata IPs. This is not a complete SSRF
// sandbox: remote providers resolve and fetch URLs from their own networks, so
// split-horizon DNS or DNS rebinding can still differ from this local check.
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

	// Parser-differential SSRF guard: hosts like 0177.0.0.1, 0x7f.0.0.1, or
	// 017700000001 fail Go's strict net.ParseIP but are read as 127.0.0.1 by
	// libc/inet_aton-style resolvers. Such all-numeric (decimal, octal, or
	// hex) dotted strings are never valid DNS names per RFC 3696/952, so we
	// block them before they can fall through to lookupIP and resolve as a
	// public-looking name.
	if looksLikeNumericIP(host) {
		return fmt.Errorf("host %q is a malformed or ambiguous numeric IP", host)
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
	if ip.IsMulticast() {
		return fmt.Errorf("multicast address %s is not allowed", ip)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified address %s is not allowed", ip)
	}
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("cloud metadata address %s is not allowed", ip)
	}
	if addr, ok := netip.AddrFromSlice(ip); ok {
		addr = addr.Unmap()
		for _, p := range extraBlockedPrefixes {
			if p.Contains(addr) {
				return fmt.Errorf("reserved address %s (%s) is not allowed", ip, p)
			}
		}
		// A genuine IPv6 literal can smuggle an IPv4 address that the classifiers
		// and prefix table above do not reject on the v6 form: IPv4-compatible
		// (::a.b.c.d), 6to4 (2002::/16), and NAT64 with a network-specific prefix
		// (RFC 6052/8215, beyond the well-known 64:ff9b::/96 already in the
		// table). Decode any embedded IPv4 and re-validate it so a v6 wrapper
		// around 169.254.169.254 / 10.0.0.1 / etc. is blocked too. v4-mapped
		// (::ffff:a.b.c.d) is already collapsed by Unmap/net.ParseIP, so Is6()
		// skips it here.
		if addr.Is6() {
			for _, v4 := range embeddedV4Candidates(addr) {
				if err := validateIP(v4); err != nil {
					return fmt.Errorf("IPv6 %s embeds blocked IPv4 %s: %w", ip, v4, err)
				}
			}
		}
	}
	return nil
}

// embeddedV4Candidates extracts candidate IPv4 addresses embedded in an IPv6
// address via the well-known transitional encodings (IPv4-compatible, 6to4,
// NAT64). It returns 4-byte net.IPs (possibly none); a generic public IPv6
// matches no pattern and yields an empty slice, so genuine v6 hosts are never
// affected. The branches are disjoint by leading bytes.
func embeddedV4Candidates(addr netip.Addr) []net.IP {
	b := addr.As16()
	add := func(p0, p1, p2, p3 byte) []net.IP {
		return []net.IP{net.IPv4(p0, p1, p2, p3).To4()}
	}
	switch {
	// IPv4-compatible ::a.b.c.d (RFC 4291, top 96 bits zero, NOT ::ffff:); the
	// embedded v4 is in the low 32 bits. Loopback/unspecified ::/:: forms are
	// already rejected by the classifiers before this point.
	case isZeroBytes(b[0:12]):
		return add(b[12], b[13], b[14], b[15])
	// 6to4 2002::/16 (RFC 3056): embedded v4 in bytes 2..5.
	case b[0] == 0x20 && b[1] == 0x02:
		return add(b[2], b[3], b[4], b[5])
	// NAT64 64:ff9b::/32 (RFC 6052 well-known + RFC 8215 local-use). The
	// well-known /96 already sits in extraBlockedPrefixes; this additionally
	// catches network-specific-prefix forms. Per RFC 6052 §2.2 the embedded v4
	// position depends on the translation prefix length and always skips byte 8
	// (the reserved "u" octet), so the v4 is NOT necessarily in the low 32 bits.
	// Emit every standard layout (/32../96) and let validateIP re-validate each,
	// so an embedded private/metadata v4 is caught regardless of prefix length.
	case b[0] == 0x00 && b[1] == 0x64 && b[2] == 0xff && b[3] == 0x9b:
		return []net.IP{
			net.IPv4(b[4], b[5], b[6], b[7]).To4(),     // /32
			net.IPv4(b[5], b[6], b[7], b[9]).To4(),     // /40 (skip u-octet b[8])
			net.IPv4(b[6], b[7], b[9], b[10]).To4(),    // /48
			net.IPv4(b[7], b[9], b[10], b[11]).To4(),   // /56
			net.IPv4(b[9], b[10], b[11], b[12]).To4(),  // /64
			net.IPv4(b[12], b[13], b[14], b[15]).To4(), // /96
		}
	}
	return nil
}

func isZeroBytes(bs []byte) bool {
	for _, x := range bs {
		if x != 0 {
			return false
		}
	}
	return true
}

// looksLikeNumericIP reports whether host is a dotted string whose every label
// is an all-decimal, octal (leading 0), or 0x-hex digit run. Such strings are
// numeric IP attempts that failed strict net.ParseIP but may still be resolved
// as an IP by libc/inet_aton backends; they are never valid DNS names per
// RFC 3696/952. Real hostnames (3com.com, 123abc.com, a1.b2.c3) contain at
// least one non-numeric label and are not matched.
func looksLikeNumericIP(host string) bool {
	// Strip a single trailing dot (FQDN form) so the numeric-IP forms this guard
	// blocks are also caught when written as "0177.0.0.1." etc.
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !isNumericLabel(label) {
			return false
		}
	}
	return true
}

// isNumericLabel reports whether a single dot-separated label is a pure numeric
// run: decimal digits, an octal form (leading 0 then octal digits), or a 0x/0X
// hex form. A label containing any letter outside a valid hex run (e.g. "3com",
// "123abc") is not numeric.
func isNumericLabel(label string) bool {
	if label == "" {
		return false
	}
	// 0x / 0X hex form: at least one hex digit after the prefix.
	if len(label) > 2 && label[0] == '0' && (label[1] == 'x' || label[1] == 'X') {
		for _, c := range label[2:] {
			if !isHexDigit(c) {
				return false
			}
		}
		return true
	}
	// Decimal or octal form: digits only. Octal (leading 0) is a strict subset
	// of decimal digits here, so a single digit-only check covers both.
	for _, c := range label {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isHexDigit(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
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

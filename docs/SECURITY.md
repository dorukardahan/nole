# Nólë Security Notes

## URL extraction preflight

Nólë performs a local, best-effort URL preflight before dispatching `extract` requests to remote providers. The guard rejects non-HTTP(S) schemes, obvious local hostnames, loopback, private, link-local, multicast, unspecified, and cloud metadata IP addresses.

This is a risk-reduction layer, not a complete SSRF sandbox. Extraction providers fetch URLs from their own infrastructure, so their DNS view and network reachability can differ from the machine running Nólë. Split-horizon DNS, DNS rebinding, provider-side redirects, and provider-specific fetch behavior are outside the guarantee of this local preflight. Keep provider credentials least-privileged and avoid treating remote extraction as a way to fetch untrusted internal URLs.

## Benchmark credentials

Benchmark helpers must not put real API keys in process arguments. Prefer Python stdlib HTTP requests or stdin-based request bodies over `curl -d <body-with-secret>` so process listings and shell telemetry do not expose provider keys.

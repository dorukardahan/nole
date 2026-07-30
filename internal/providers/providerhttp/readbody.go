package providerhttp

import (
	"encoding/json"
	"fmt"
	"io"
)

// Response body size caps. Providers stream untrusted, attacker-influenceable
// bodies (a compromised or hostile upstream can advertise any Content-Length or
// none at all), so an unbounded io.ReadAll / json.Decode is an OOM DoS vector.
// These ceilings bound the worst case while staying comfortably above any
// legitimate provider payload.
const (
	// MaxSearchResponseBytes caps search-style responses (Brave/Tavily/Firecrawl
	// JSON, DDG HTML). 16 MiB is far above any real search result page.
	MaxSearchResponseBytes int64 = 16 << 20
	// MaxExtractResponseBytes caps extract/scrape responses, which carry full
	// page content and so warrant a larger ceiling than search.
	MaxExtractResponseBytes int64 = 64 << 20
)

// ReadAllLimited reads at most max bytes from r and returns the bytes read. If
// the source has more than max bytes available, it returns a redaction-safe
// error that records only the limit — never any body content, which can echo
// API keys, auth headers, or private URLs.
//
// It reads max+1 bytes so the overflow is detected exactly at the boundary: a
// body of precisely max bytes succeeds, one byte more fails.
func ReadAllLimited(r io.Reader, max int64) ([]byte, error) {
	if max < 0 {
		max = 0
	}
	// Read one extra byte to distinguish "exactly max" from "more than max".
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return data, err
	}
	if int64(len(data)) > max {
		// Drop the overflow byte from the returned slice; callers should treat
		// the error as fatal, but keeping the slice at the cap avoids surprising
		// a caller that ignores the error.
		return data[:max], fmt.Errorf("response body exceeded %d bytes", max)
	}
	return data, nil
}

// DecodeJSONLimited JSON-decodes at most max bytes from r into v. The reader is
// wrapped in an io.LimitReader so a hostile or runaway body cannot exhaust
// memory. When the body is larger than max the truncated stream almost always
// yields an "unexpected EOF" from the decoder; this function detects the
// truncation case and returns a clear, redaction-safe "too_large" error instead
// of the raw decode error so callers can distinguish an oversized payload from a
// genuinely malformed one. No body content is ever placed in the error.
func DecodeJSONLimited(r io.Reader, max int64, v any) error {
	if max < 0 {
		max = 0
	}
	// Wrap in a LimitReader of max+1 so we can tell "the body fit within max"
	// from "the body was truncated at the cap".
	counter := &countingReader{r: io.LimitReader(r, max+1)}
	dec := json.NewDecoder(counter)
	if err := dec.Decode(v); err != nil {
		if counter.n > max {
			return fmt.Errorf("response body too_large: exceeded %d bytes (truncated before decode completed)", max)
		}
		return err
	}
	// A second decode must reach EOF. json.Decoder.Decode accepts a valid JSON
	// prefix and otherwise ignores malformed bytes or another top-level value;
	// treating that as success lets partial provider payloads fake-green. Keep
	// the same bounded reader and return only parser metadata, never body data.
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if counter.n > max {
			return fmt.Errorf("response body too_large: exceeded %d bytes (trailing data crossed the limit)", max)
		}
		if err == nil {
			return fmt.Errorf("response contained multiple JSON values")
		}
		return err
	}
	// A successful top-level Decode means the value fit within the cap. We do
	// NOT reject here on counter.n > max: json.Decoder can buffer one trailing
	// byte (e.g. an API's trailing '\n') past a value sitting exactly at the
	// cap, which would spuriously fail an otherwise-valid response. The
	// in-Decode branch above still catches a genuinely oversized (truncated
	// mid-value) body, and LimitReader(r, max+1) remains the OOM bound.
	return nil
}

// countingReader tracks how many bytes have been consumed from the underlying
// reader so DecodeJSONLimited can tell truncation apart from malformed JSON.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

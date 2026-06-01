package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// saeFake serves BOTH the search leg (returns its configured URLs) and the
// extract leg (records every URL it is asked to extract, fails the configured
// failURL). Pointer receiver so calls are observable. Literal-IP URLs keep the
// tests net-free (the SSRF preflight resolves literal IPs without DNS).
type saeFake struct {
	fakeProvider
	urls    []string
	failURL string
	mu      sync.Mutex
	calls   []string
}

func (p *saeFake) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	results := make([]SearchResult, 0, len(p.urls))
	for i, u := range p.urls {
		results = append(results, SearchResult{Title: fmt.Sprintf("r%d", i), URL: u, Snippet: "s", Provider: p.name})
	}
	return SearchResponse{Query: req.Query, Task: req.Task, Provider: p.name, Results: results}, nil
}

func (p *saeFake) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	p.mu.Lock()
	p.calls = append(p.calls, req.URL)
	p.mu.Unlock()
	if req.URL == p.failURL {
		return ExtractResponse{}, fmt.Errorf("provider extract failed for %s", req.URL)
	}
	return ExtractResponse{URL: req.URL, Provider: p.name, Content: "extracted " + req.URL}, nil
}

func (p *saeFake) callLog() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

func newSAEService(p Provider) *Service {
	registry := NewRegistry()
	_ = registry.Register(p)
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: p.Name(), FreeRemaining: 100})
	return NewService(registry, ledger, RouteMatrix{TaskGeneral: {p.Name()}, TaskExtract: {p.Name()}})
}

const (
	goodIP1 = "http://93.184.216.34/"
	goodIP2 = "http://1.1.1.1/"
	goodIP3 = "http://8.8.8.8/"
	ssrfIP  = "http://169.254.169.254/" // link-local cloud-metadata → blocked by preflight
)

func TestSearchAndExtractHappyPath(t *testing.T) {
	p := &saeFake{fakeProvider: fakeProvider{name: "p"}, urls: []string{goodIP1, goodIP2}}
	svc := newSAEService(p)
	resp, err := svc.SearchAndExtract(context.Background(), SearchAndExtractRequest{Query: "jaguar", Task: TaskGeneral, ExtractTop: 1})
	if err != nil {
		t.Fatalf("search_and_extract: %v", err)
	}
	if len(resp.Search.Results) != 2 {
		t.Fatalf("expected 2 search results, got %d", len(resp.Search.Results))
	}
	if len(resp.Extracts) != 1 || resp.Extracts[0].URL != goodIP1 {
		t.Fatalf("expected the top URL extracted, got %#v", resp.Extracts)
	}
	if len(resp.ExtractErrors) != 0 {
		t.Fatalf("expected no extract errors, got %#v", resp.ExtractErrors)
	}
}

// A blocked (SSRF) top URL is non-fatal AND the preflight rejects it BEFORE the
// provider is ever called; with extract_top>=2 the next good URL is extracted.
func TestSearchAndExtractSSRFBlockedNonFatalAndPreflightFirst(t *testing.T) {
	p := &saeFake{fakeProvider: fakeProvider{name: "p"}, urls: []string{ssrfIP, goodIP1}}
	svc := newSAEService(p)
	resp, err := svc.SearchAndExtract(context.Background(), SearchAndExtractRequest{Query: "jaguar", Task: TaskGeneral, ExtractTop: 2})
	if err != nil {
		t.Fatalf("blocked URL must be non-fatal, got err %v", err)
	}
	if len(resp.ExtractErrors) != 1 || resp.ExtractErrors[0].URL != ssrfIP {
		t.Fatalf("expected the blocked URL recorded in ExtractErrors, got %#v", resp.ExtractErrors)
	}
	if len(resp.Extracts) != 1 || resp.Extracts[0].URL != goodIP1 {
		t.Fatalf("expected the next good URL extracted, got %#v", resp.Extracts)
	}
	for _, c := range p.callLog() {
		if c == ssrfIP {
			t.Fatalf("SSRF URL must be rejected by preflight BEFORE reaching the provider; provider saw %v", p.callLog())
		}
	}
}

func TestSearchAndExtractProviderErrorNonFatal(t *testing.T) {
	p := &saeFake{fakeProvider: fakeProvider{name: "p"}, urls: []string{goodIP1}, failURL: goodIP1}
	svc := newSAEService(p)
	resp, err := svc.SearchAndExtract(context.Background(), SearchAndExtractRequest{Query: "jaguar", Task: TaskGeneral, ExtractTop: 1})
	if err != nil {
		t.Fatalf("provider extract error must be non-fatal, got %v", err)
	}
	if len(resp.ExtractErrors) != 1 || resp.ExtractErrors[0].URL != goodIP1 {
		t.Fatalf("expected provider error recorded, got %#v", resp.ExtractErrors)
	}
	if len(resp.Extracts) != 0 {
		t.Fatalf("expected no successful extracts, got %#v", resp.Extracts)
	}
}

func TestSearchAndExtractClampsExtractTop(t *testing.T) {
	p := &saeFake{fakeProvider: fakeProvider{name: "p"}, urls: []string{goodIP1, goodIP2, goodIP3, "http://9.9.9.9/"}}
	svc := newSAEService(p)
	// extract_top=99 → clamp to maxExtractTop (3) attempts.
	resp, err := svc.SearchAndExtract(context.Background(), SearchAndExtractRequest{Query: "jaguar", Task: TaskGeneral, ExtractTop: 99})
	if err != nil {
		t.Fatalf("search_and_extract: %v", err)
	}
	if got := len(resp.Extracts) + len(resp.ExtractErrors); got != maxExtractTop {
		t.Fatalf("extract_top=99 should clamp to %d attempts, got %d", maxExtractTop, got)
	}
	// extract_top=0 → defaults to 1 attempt.
	p2 := &saeFake{fakeProvider: fakeProvider{name: "p"}, urls: []string{goodIP1, goodIP2}}
	svc2 := newSAEService(p2)
	resp2, _ := svc2.SearchAndExtract(context.Background(), SearchAndExtractRequest{Query: "jaguar", Task: TaskGeneral, ExtractTop: 0})
	if got := len(resp2.Extracts) + len(resp2.ExtractErrors); got != 1 {
		t.Fatalf("extract_top=0 should default to 1 attempt, got %d", got)
	}
}

func TestSearchAndExtractDeduplicatesURLs(t *testing.T) {
	p := &saeFake{fakeProvider: fakeProvider{name: "p"}, urls: []string{goodIP1, goodIP1, goodIP2}}
	svc := newSAEService(p)
	_, err := svc.SearchAndExtract(context.Background(), SearchAndExtractRequest{Query: "jaguar", Task: TaskGeneral, ExtractTop: 3})
	if err != nil {
		t.Fatalf("search_and_extract: %v", err)
	}
	// goodIP1 appears twice in results but must be extracted once (no double-debit).
	count := 0
	for _, c := range p.callLog() {
		if c == goodIP1 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate URL must be extracted once, got %d calls for %s (%v)", count, goodIP1, p.callLog())
	}
}

func TestSearchAndExtractSearchFailureIsFatal(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(failingProvider{fakeProvider{name: "f"}})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "f", FreeRemaining: 10})
	svc := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"f"}, TaskExtract: {"f"}})
	resp, err := svc.SearchAndExtract(context.Background(), SearchAndExtractRequest{Query: "jaguar", Task: TaskGeneral, ExtractTop: 1})
	if err == nil {
		t.Fatal("expected a fatal error when the search leg fails")
	}
	if len(resp.Extracts) != 0 {
		t.Fatalf("no extracts should run when search fails, got %#v", resp.Extracts)
	}
}

func TestSearchAndExtractErrorIsSanitized(t *testing.T) {
	// The blocked-URL ExtractError.Error must go through safeerr.Message — assert
	// it carries no raw credential markers.
	p := &saeFake{fakeProvider: fakeProvider{name: "p"}, urls: []string{ssrfIP}}
	svc := newSAEService(p)
	resp, _ := svc.SearchAndExtract(context.Background(), SearchAndExtractRequest{Query: "jaguar", Task: TaskGeneral, ExtractTop: 1})
	if len(resp.ExtractErrors) != 1 {
		t.Fatalf("expected one extract error, got %#v", resp.ExtractErrors)
	}
	msg := resp.ExtractErrors[0].Error
	for _, bad := range []string{"Bearer ", "api_key", "Authorization"} {
		if strings.Contains(msg, bad) {
			t.Fatalf("extract error leaked %q: %s", bad, msg)
		}
	}
}

package ddgs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

// A skipped ad row that carries its OWN result__snippet must not shift every
// subsequent organic snippet onto the wrong result. The previous parser zipped
// links and snippets by a counter that only advanced on kept links, so the ad
// row's snippet leaked onto the first organic result and pushed the rest down.
func TestDDGSAdSnippetAlignment(t *testing.T) {
	html := `<html><body>
<a class="result__a" href="https://duckduckgo.com/y.js?ad=1">Sponsored Title</a>
<a class="result__snippet" href="https://ad.example">AD_SNIPPET should not map to an organic result</a>
<a class="result__a" href="https://example.com/one">Org One</a>
<a class="result__snippet" href="https://example.com/one">ORG_ONE_SNIPPET first organic</a>
<a class="result__a" href="https://example.org/two">Org Two</a>
<a class="result__snippet" href="https://example.org/two">ORG_TWO_SNIPPET second organic</a>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	p := Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}}
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "nole", Limit: 5})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 organic results (ad skipped), got %d: %#v", len(resp.Results), resp.Results)
	}
	if resp.Results[0].URL != "https://example.com/one" {
		t.Fatalf("results[0].URL = %q, want example.com/one", resp.Results[0].URL)
	}
	if !strings.Contains(resp.Results[0].Snippet, "ORG_ONE_SNIPPET") {
		t.Fatalf("results[0].Snippet = %q, want the first organic snippet", resp.Results[0].Snippet)
	}
	if !strings.Contains(resp.Results[1].Snippet, "ORG_TWO_SNIPPET") {
		t.Fatalf("results[1].Snippet = %q, want the second organic snippet", resp.Results[1].Snippet)
	}
	for _, r := range resp.Results {
		if strings.Contains(r.Snippet, "AD_SNIPPET") {
			t.Fatalf("ad snippet leaked onto an organic result: %q", r.Snippet)
		}
	}
}

package cli

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestRESTSearchTraceOptIn(t *testing.T) {
	h := newTestHTTPHandler(t)

	rec := doREST(t, h, http.MethodPost, "/api/search", []byte(`{"query":"hello","task":"general"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/search = %d: %s", rec.Code, rec.Body.String())
	}
	var resp core.SearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.RouteTrace) != 0 {
		t.Fatalf("default REST search must omit route_trace, got %d", len(resp.RouteTrace))
	}
	if resp.RoutingInsight == "" {
		t.Fatalf("routing_insight must be present even when route_trace is omitted")
	}

	rec = doREST(t, h, http.MethodPost, "/api/search", []byte(`{"query":"hello","task":"general","include_trace":true}`))
	resp = core.SearchResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode (trace): %v", err)
	}
	if len(resp.RouteTrace) == 0 {
		t.Fatalf("include_trace:true must include route_trace")
	}
}

func TestRESTSearchAndExtract(t *testing.T) {
	h := newTestHTTPHandler(t)

	for _, m := range []string{http.MethodGet, http.MethodPut} {
		if rec := doREST(t, h, m, "/api/search_and_extract", nil); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /api/search_and_extract = %d, want 405", m, rec.Code)
		}
	}

	rec := doREST(t, h, http.MethodPost, "/api/search_and_extract", []byte(`{"query":"hello","task":"general","extract_top":1}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/search_and_extract = %d: %s", rec.Code, rec.Body.String())
	}
	var resp core.SearchAndExtractResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Search.Results) == 0 {
		t.Fatalf("expected search results")
	}
	if len(resp.Extracts)+len(resp.ExtractErrors) == 0 {
		t.Fatalf("expected at least one extract attempt")
	}
	if len(resp.Search.RouteTrace) != 0 {
		t.Fatalf("default must omit the search route_trace")
	}

	if rec := doREST(t, h, http.MethodPost, "/api/search_and_extract", []byte("{not json")); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed /api/search_and_extract = %d, want 400", rec.Code)
	}
}

func TestRESTResearchEvidenceNoSummary(t *testing.T) {
	h := newTestHTTPHandler(t)

	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		if rec := doREST(t, h, m, "/api/research", nil); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /api/research = %d, want 405", m, rec.Code)
		}
	}

	rec := doREST(t, h, http.MethodPost, "/api/research", []byte(`{"question":"what is model context protocol","max_steps":1}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/research = %d: %s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := m["summary"]; ok {
		t.Fatalf("research output must carry no summary key: %s", rec.Body.String())
	}
	for _, want := range []string{"sources", "extracts", "providers_used"} {
		if _, ok := m[want]; !ok {
			t.Fatalf("research output missing %q key: %s", want, rec.Body.String())
		}
	}

	if rec := doREST(t, h, http.MethodPost, "/api/research", []byte("{bad")); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed /api/research = %d, want 400", rec.Code)
	}
}

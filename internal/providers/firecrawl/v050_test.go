package firecrawl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestFirecrawlFreshnessForRecencyTasks(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(firecrawlSearchResponse{
			Success: boolPtr(true),
			Data:    firecrawlSearchData{Web: []firecrawlSearchWebResult{{Title: "t", URL: "https://u", Description: "d"}}},
		})
	}))
	defer srv.Close()
	p := New(WithAPIKey("k"), WithBaseURL(srv.URL))

	out, err := p.Search(context.Background(), core.SearchRequest{Query: "q", Task: core.TaskNews})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if body["tbs"] != "qdr:m" {
		t.Fatalf("news tbs = %v, want qdr:m", body["tbs"])
	}
	// Web-source results carry no score/date — honest empty, never fabricated.
	if out.Results[0].Score != nil || out.Results[0].PublishedAt != "" {
		t.Fatalf("firecrawl web result should have nil Score / empty PublishedAt, got %#v", out.Results[0])
	}

	_, _ = p.Search(context.Background(), core.SearchRequest{Query: "q", Task: core.TaskGeneral})
	if _, ok := body["tbs"]; ok {
		t.Fatalf("general must omit tbs, got %v", body["tbs"])
	}
}

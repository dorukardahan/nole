package tavily

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestSearchCurrentCountryAndLanguageContract(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		task                           core.TaskType
		country, language, wantCountry string
	}{
		{"general US", core.TaskGeneral, "us", "en", "united states"},
		{"Turkish docs", core.TaskDocs, "tr", "tr", "turkey"},
		{"British research", core.TaskResearch, "gb", "en-gb", "united kingdom"},
		{"news country unsupported", core.TaskNews, "us", "en", ""},
		{"factcheck country unsupported", core.TaskFactcheck, "tr", "tr", ""},
		{"unsupported country", core.TaskGeneral, "zz", "", ""},
		{"defaults unchanged", core.TaskGeneral, "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				country, _ := body["country"].(string)
				language, _ := body["language"].(string)
				if country != tc.wantCountry || language != tc.language {
					t.Errorf("country/language = %q/%q, want %q/%q", country, language, tc.wantCountry, tc.language)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if tc.wantCountry == "" {
					if _, exists := body["country"]; exists {
						t.Error("unsupported country must be omitted")
					}
				}
				if tc.language == "" {
					if _, exists := body["language"]; exists {
						t.Error("default language must be omitted")
					}
				}
				for _, key := range []string{"filter_by_language", "auto_parameters"} {
					if _, exists := body[key]; exists {
						t.Errorf("unexpected automatic setting %s", key)
					}
				}
				depth := "basic"
				if tc.task == core.TaskResearch {
					depth = "advanced"
				}
				if body["search_depth"] != depth {
					t.Errorf("depth changed: %v", body["search_depth"])
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{"title": "Documentation", "url": "https://example.com/docs", "content": "public documentation"}}})
			}))
			defer srv.Close()
			p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
			resp, err := p.Search(context.Background(), core.SearchRequest{Query: "public documentation", Task: tc.task, Limit: 1, Options: core.SearchOptions{Country: tc.country, SearchLang: tc.language}})
			if err != nil || len(resp.Results) != 1 || calls != 1 {
				t.Fatalf("search: results=%d calls=%d err=%v", len(resp.Results), calls, err)
			}
		})
	}
}

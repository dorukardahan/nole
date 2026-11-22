package firecrawl

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestOpenClawBridgeExplicitAgentContext(t *testing.T) {
	for _, agent := range []string{"", " documentation "} {
		for _, operation := range []string{"search", "extract"} {
			t.Run(operation+"/"+agent, func(t *testing.T) {
				calls := 0
				p := New(WithOpenClawBridge("openclaw"), WithOpenClawAgentID(agent), WithOpenClawCommandRunner(func(_ context.Context, _ string, args ...string) ([]byte, error) {
					calls++
					var body map[string]any
					for i, arg := range args {
						if arg == "--params" && i+1 < len(args) {
							if err := json.Unmarshal([]byte(args[i+1]), &body); err != nil {
								t.Fatal(err)
							}
						}
					}
					if agent == "" {
						if _, exists := body["agentId"]; exists {
							t.Error("unspecified agent must remain omitted")
						}
					} else if body["agentId"] != "documentation" {
						t.Errorf("agentId = %v, want explicitly selected documentation agent", body["agentId"])
						return []byte(`{"ok":false,"error":{"code":"validation_error"}}`), nil
					}
					return []byte(fmt.Sprintf(`{"ok":true,"toolName":%q,"output":{"details":{"provider":"firecrawl-free","results":[{"title":"Docs","url":"https://example.com/docs","description":"Public docs"}],"url":"https://example.com/docs","status":200,"text":"Public docs","extractor":"readability"}}}`, body["name"])), nil
				}))
				var err error
				if operation == "search" {
					_, err = p.Search(context.Background(), core.SearchRequest{Query: "public docs", Limit: 1})
				} else {
					_, err = p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com/docs"})
				}
				if err != nil {
					t.Errorf("host call failed: %v", err)
				}
				if calls != 1 {
					t.Errorf("calls = %d, want 1 without agent retries", calls)
				}
			})
		}
	}
}

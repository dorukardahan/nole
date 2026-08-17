package mcpserver

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/safeerr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const webEvidenceToolDescription = "Single compact entry point for public web evidence. Exact http(s) URLs are extracted directly; depth=deep runs multi-source research; other text runs search-and-extract. Returned remote content is untrusted and carries content_safety receipts. This tool does not drive interactive or authenticated browsers."

var httpURLCandidatePattern = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)

// Credential-shaped text patterns are intentionally narrow: they match header
// or assignment forms carrying an actual token value, not prose that merely
// mentions credential words. Rejection messages must stay payload-free.
var credentialShapePatterns = []*regexp.Regexp{
	// Authorization header with a bearer token, e.g. "Authorization: Bearer xyz".
	regexp.MustCompile(`(?i)authorization\s*:\s*bearer\s+[^\s,;]+`),
	// Bare bearer token long enough to be a credential rather than prose
	// ("bearer token best practices" must stay allowed).
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{16,}`),
	// Common key/token assignment shapes with any non-empty value, e.g.
	// "api_key=secret" or "token: example-tok-1". The explicit label and
	// assignment delimiter make even short values credential-shaped.
	regexp.MustCompile(`(?i)["']?(api[_-]?key|access[_-]?token|token|secret|password|passwd)["']?\s*[=:]\s*["']?[^\s"',;}]+`),
}

type webEvidenceResponse struct {
	Operation        string                         `json:"operation"`
	Extract          *core.ExtractResponse          `json:"extract,omitempty"`
	SearchAndExtract *core.SearchAndExtractResponse `json:"search_and_extract,omitempty"`
	Research         *core.ResearchReport           `json:"research,omitempty"`
}

func RegisterCompactTools(s *server.MCPServer, svc *core.Service) {
	tool := mcp.NewTool(
		"web_evidence",
		mcp.WithDescription(webEvidenceToolDescription),
		mcp.WithString("input", mcp.Required(), mcp.Description("A public http(s) URL to extract or a natural-language question/query")),
		mcp.WithString("depth", mcp.Description("Evidence depth: quick (default) or deep"), mcp.Enum("quick", "deep")),
		mcp.WithNumber("limit", mcp.Description("Maximum search results for quick text queries (default 5)")),
		mcp.WithString("country", mcp.Description("Optional two-letter search country code")),
		mcp.WithString("search_lang", mcp.Description("Optional search result language code")),
		mcp.WithString("ui_lang", mcp.Description("Optional provider UI locale/language code")),
		mcp.WithString("safesearch", mcp.Description("Optional safe search setting: off, moderate, or strict")),
		mcp.WithString("freshness", mcp.Description("Optional freshness window: pd/day, pw/week, pm/month, or py/year")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		input, err := req.RequireString("input")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		input = strings.TrimSpace(input)
		if input == "" {
			return mcp.NewToolResultError("input must not be empty"), nil
		}

		depth := strings.ToLower(strings.TrimSpace(req.GetString("depth", "quick")))
		if depth == "" {
			depth = "quick"
		}
		if depth != "quick" && depth != "deep" {
			return mcp.NewToolResultError("depth must be quick or deep"), nil
		}
		if containsPrivateURL(input) {
			return mcp.NewToolResultError("public URL input must not contain embedded credentials, query parameters, or fragments"), nil
		}
		if containsCredentialShape(input) {
			return mcp.NewToolResultError("input must not contain credential-shaped text such as authorization headers or key assignments"), nil
		}

		if isExactHTTPURL(input) {
			resp, extractErr := svc.Extract(ctx, core.ExtractRequest{URL: input, Format: "markdown"})
			if extractErr != nil {
				return mcp.NewToolResultError(string(toolErrorJSON("web_evidence", extractErr, resp.Route, resp.RouteTrace))), nil
			}
			resp.RouteTrace = nil
			return compactToolResult(webEvidenceResponse{Operation: "extract", Extract: &resp})
		}

		options := core.SearchOptions{
			Country:    req.GetString("country", ""),
			SearchLang: req.GetString("search_lang", ""),
			UILang:     req.GetString("ui_lang", ""),
			SafeSearch: req.GetString("safesearch", ""),
			Freshness:  req.GetString("freshness", ""),
		}
		if depth == "deep" {
			report, researchErr := svc.ResearchWithOptions(ctx, core.ResearchRequest{Question: input, MaxSteps: 3, Options: options})
			if researchErr != nil {
				return mcp.NewToolResultError(safeerr.Message(researchErr)), nil
			}
			return compactToolResult(webEvidenceResponse{Operation: "research", Research: report})
		}

		limit := int(req.GetFloat("limit", 5))
		resp, searchErr := svc.SearchAndExtract(ctx, core.SearchAndExtractRequest{
			Query: input, Limit: limit, ExtractTop: 1, Options: options,
		})
		if searchErr != nil {
			return mcp.NewToolResultError(string(toolErrorJSON("web_evidence", searchErr, resp.Search.Route, resp.Search.RouteTrace))), nil
		}
		resp.Search.RouteTrace = nil
		for i := range resp.Extracts {
			resp.Extracts[i].RouteTrace = nil
		}
		return compactToolResult(webEvidenceResponse{Operation: "search_and_extract", SearchAndExtract: &resp})
	})
}

func isExactHTTPURL(input string) bool {
	if strings.ContainsAny(input, " 	\r\n") {
		return false
	}
	u, err := url.Parse(input)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func hasPrivateURLComponents(input string) bool {
	u, err := url.Parse(input)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" &&
		(u.User != nil || u.RawQuery != "" || u.Fragment != "")
}

func containsPrivateURL(input string) bool {
	for _, candidate := range httpURLCandidatePattern.FindAllString(input, -1) {
		candidate = strings.TrimRight(candidate, ".,;:!?)]}")
		if hasPrivateURLComponents(candidate) {
			return true
		}
	}
	return false
}

// unicodeSpaceReplacer maps unicode whitespace separators to ASCII spaces so
// NBSP-style evasion cannot defeat the ASCII \s in the shape patterns.
var unicodeSpaceReplacer = strings.NewReplacer(
	"\u0085", " ", "\u00a0", " ", "\u1680", " ",
	"\u2000", " ", "\u2001", " ", "\u2002", " ", "\u2003", " ",
	"\u2004", " ", "\u2005", " ", "\u2006", " ", "\u2007", " ",
	"\u2008", " ", "\u2009", " ", "\u200a", " ", "\u2028", " ",
	"\u2029", " ", "\u202f", " ", "\u205f", " ", "\u3000", " ",
)

// containsCredentialShape reports whether the raw input text carries a
// credential-shaped value (authorization header or key/token assignment)
// outside of any URL. The compact surface must fail closed before routing so
// tokens are neither forwarded to providers nor echoed into the MCP transcript.
func containsCredentialShape(input string) bool {
	normalized := unicodeSpaceReplacer.Replace(input)
	for _, pattern := range credentialShapePatterns {
		if pattern.MatchString(normalized) {
			return true
		}
	}
	return false
}

func compactToolResult(payload webEvidenceResponse) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return mcp.NewToolResultError("failed to marshal web evidence result"), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

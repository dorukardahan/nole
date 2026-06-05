package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/safeerr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	searchToolDescription           = "Search the public web for internet research, current information, technical docs, news, fact-checking, code examples, pricing, people/company lookups, or deep-research source discovery using free-tier task-based provider routing."
	extractToolDescription          = "Extract clean readable content from a public web page URL for summarization, citation, documentation lookup, or research context using free-tier routing with local URL safety preflight."
	searchAndExtractToolDescription = "Search the public web and extract the clean readable content of the top result(s) in a single call — the combined search-then-read primitive for grounding an answer or following up on a hit."
	researchToolDescription         = "Run a multi-step research pass (search across task-fit routes, then extract top sources) and return the deduplicated sources and extracted content for the agent to synthesize. Returns evidence, not a composed answer."
	includeTraceDescription         = "Include the full per-attempt route_trace debug blob in the response (default false; the compact routing_insight is always present)."
)

// tipStateMaxEntries caps the number of client-supplied session IDs tracked in
// the tip-deduplication map. Once this limit is reached, new (unseen) session
// IDs are treated as fail-open: the tip is emitted but not recorded. Legitimate
// stable-session clients whose IDs are already in the map are unaffected.
//
// 1000 persistent sessions × (≈50 bytes/entry) ≈ 50 KB — negligible. The cap
// primarily guards against a rotating-header client that sends a different
// Mcp-Session-Id on every request, which would otherwise cause the map to grow
// linearly with traffic volume.
const tipStateMaxEntries = 1000

// hashSessionID reduces a client-supplied session ID to a fixed-length key so
// the dedup map cannot be driven to large memory by long IDs. SHA-256 → hex
// gives 64 chars per entry; with the entry cap (tipStateMaxEntries), total
// map size stays bounded regardless of input.
func hashSessionID(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:])
}

// HasExtractCapableConfigured reports whether a KEYED or local-Scrapling extract
// provider is configured in the environment — i.e. a JS-capable / higher-fidelity
// extract path. It is NOT the MCP tool-gate anymore: since the keyless httpfetch
// backstop is always registered, the extract tools are advertised whenever the
// service registry has an available extract provider (Service.HasExtractCapableProvider),
// which is always true in the default service. This predicate remains useful to
// distinguish "a JS-capable extract provider is configured" from "only the keyless
// best-effort backstop is available" (e.g. for doctor's keyed-path regression guard
// and setup hints). For remote BYOK providers it checks keys; for Scrapling it
// checks the configured local runtime.
func HasExtractCapableConfigured() bool {
	if strings.TrimSpace(os.Getenv("NOLE_SCRAPLING_PYTHON")) != "" {
		return true
	}
	for _, p := range core.BYOKProviders() {
		if !p.SupportsExtract {
			continue
		}
		for _, ev := range p.EnvVars {
			if os.Getenv(ev) != "" {
				return true
			}
		}
	}
	return false
}

func RegisterTools(s *server.MCPServer, svc *core.Service) {
	taskDesc := buildTaskDescription()
	searchTool := mcp.NewTool(
		"search",
		mcp.WithDescription(searchToolDescription),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural-language web search or internet research query")),
		mcp.WithString("task", mcp.Description(taskDesc), mcp.Enum(buildTaskEnumValues()...)),
		mcp.WithNumber("limit", mcp.Description("Maximum number of search results to return")),
		mcp.WithString("country", mcp.Description("Optional two-letter search country code (for supported providers, e.g. us, tr)")),
		mcp.WithString("search_lang", mcp.Description("Optional search result language code for supported providers (e.g. en)")),
		mcp.WithString("ui_lang", mcp.Description("Optional provider UI locale/language code (e.g. en-us)")),
		mcp.WithString("safesearch", mcp.Description("Optional safe search setting for supported providers: off, moderate, or strict")),
		mcp.WithString("freshness", mcp.Description("Optional freshness window: pd/day, pw/week, pm/month, or py/year")),
		mcp.WithBoolean("include_trace", mcp.Description(includeTraceDescription)),
	)
	// tipState tracks which client-supplied session IDs have already received
	// the setup_tip. Only persistent sessions (client-supplied Mcp-Session-Id
	// or stdio) use this map. Ephemeral HTTP requests (client omitted the
	// header) are identified by the EphemeralCtxKey context value set in
	// internal/cli/http.go and always receive the tip without touching the map.
	//
	// Transport breakdown:
	//
	//   - stdio (nole mcp): one process = one client. No session is injected
	//     into ctx, so we fall back to the fixed key "stdio-default". Tip is
	//     emitted once per process lifetime.
	//
	//   - HTTP (nole serve --mcp), client sends Mcp-Session-Id: persistent
	//     session. ID stored in map, tip emitted once per session.
	//
	//   - HTTP, no Mcp-Session-Id header: ephemeral request. EphemeralCtxKey
	//     is set to true in the context by internal/cli/http.go. The tip is
	//     always emitted; nothing is written to the map. Memory is bounded
	//     regardless of traffic volume, and a client that later decides to pin
	//     a session cannot accidentally inherit ephemeral semantics from a
	//     server-generated ID (the previous "http-ephemeral-" prefix approach).
	//
	// Map is capped at tipStateMaxEntries (see constant above). New IDs seen
	// after the cap is reached are emitted but not recorded (fail-open).
	var tipState struct {
		sync.Mutex
		emitted map[string]bool // session ID → already emitted (persistent sessions only)
	}
	tipState.emitted = make(map[string]bool)
	s.AddTool(searchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		// NormalizeTaskParam maps aliases (community→social) and returns "" for
		// blank/unknown/extract so the service classifies the query instead of
		// misrouting. The enum above is advisory only (mcp-go does not enforce
		// it), so leniency must live here, not in an error.
		task := core.NormalizeTaskParam(req.GetString("task", ""))
		limit := int(req.GetFloat("limit", 5))
		options := searchOptionsFromMCP(req)
		resp, err := svc.Search(ctx, core.SearchRequest{Query: query, Task: task, Limit: limit, Options: options})
		if err != nil {
			return mcp.NewToolResultError(string(toolErrorJSON("search", err, resp.Route, resp.RouteTrace))), nil
		}
		// route_trace is opt-in on the agent surface: omit the debug blob by
		// default (the compact routing_insight stays). Success path only — the
		// error envelope above keeps its trace.
		if !req.GetBool("include_trace", false) {
			resp.RouteTrace = nil
		}

		// Check whether this is an ephemeral HTTP request (no Mcp-Session-Id
		// header). The flag is set at dispatch time in internal/cli/http.go
		// so ephemerality is determined by how the request arrived, not by any
		// string pattern in the session ID.
		ephemeral, _ := ctx.Value(EphemeralCtxKey{}).(bool)

		var shouldEmit bool
		if ephemeral {
			// Ephemeral requests always get the tip; nothing is written to the
			// map, so memory stays bounded regardless of traffic volume.
			shouldEmit = true
		} else {
			// Persistent sessions (stdio or client-pinned HTTP): emit once per
			// session. Determine the session key: if the mcp-go library injected
			// a ClientSession use its ID; otherwise fall back to "stdio-default"
			// so stdio preserves its once-per-process behaviour.
			sessionID := "stdio-default"
			if sess := server.ClientSessionFromContext(ctx); sess != nil {
				sessionID = sess.SessionID()
			}

			// Hash the client-supplied session ID before using it as a map key.
			// A hostile client could send an arbitrarily large Mcp-Session-Id
			// header (e.g. 10 MB). Storing raw IDs would let
			// tipStateMaxEntries × maxHeaderSize bytes accumulate in memory.
			// Hashing to SHA-256 hex caps every key at 64 bytes so the total
			// map size is bounded at roughly tipStateMaxEntries × 100 bytes
			// (~100 KB) regardless of input length.
			// sessionID is still used unmodified for logging and other purposes
			// outside this block; only the MAP KEY is hashed.
			hashedID := hashSessionID(sessionID)

			// Bounded-map logic (fixes P2 Issue 2).
			// - Already seen: respect the recorded value (emit once, suppress forever).
			// - Not seen, room in map: record and emit.
			// - Not seen, map at cap: emit but do not record (fail-open). A
			//   rotating-header client gets potentially-repeated tips but the
			//   server's memory is capped. Legitimate stable-session clients
			//   already recorded are unaffected.
			tipState.Lock()
			if seen, ok := tipState.emitted[hashedID]; ok {
				shouldEmit = !seen
				if shouldEmit {
					tipState.emitted[hashedID] = true
				}
			} else if len(tipState.emitted) < tipStateMaxEntries {
				tipState.emitted[hashedID] = true
				shouldEmit = true
			} else {
				// Cap reached: emit but don't record.
				shouldEmit = true
			}
			tipState.Unlock()
		}

		if shouldEmit {
			statusResp := svc.ProviderStatus(ctx)
			if tip := core.BuildSetupTip(statusResp.SetupSuggestions); tip != nil {
				resp.SetupTip = tip
			}
		}
		b, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	})

	// Advertise extract / search_and_extract whenever the registry has an
	// extract-capable provider (capability check only — no Status probe, so MCP
	// startup never launches Scrapling's Python and the tool set never flaps with
	// transient provider health). With the keyless httpfetch backstop always
	// registered by the default service, this is always true — extract works out of
	// the box with zero keys and zero setup. (A service built with no extract
	// provider at all — e.g. a custom embedding — still correctly hides them.)
	if svc.HasExtractCapableProvider() {
		extractTool := mcp.NewTool(
			"extract",
			mcp.WithDescription(extractToolDescription),
			mcp.WithString("url", mcp.Required(), mcp.Description("Public http(s) web page URL to read and extract")),
			mcp.WithString("format", mcp.Description("Output format, default markdown")),
			mcp.WithBoolean("include_trace", mcp.Description(includeTraceDescription)),
		)
		s.AddTool(extractTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			url, err := req.RequireString("url")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			format := req.GetString("format", "markdown")
			resp, err := svc.Extract(ctx, core.ExtractRequest{URL: url, Format: format})
			if err != nil {
				return mcp.NewToolResultError(string(toolErrorJSON("extract", err, resp.Route, resp.RouteTrace))), nil
			}
			if !req.GetBool("include_trace", false) {
				resp.RouteTrace = nil
			}
			b, err := json.MarshalIndent(resp, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
		})

		// search_and_extract is gated alongside extract (same registry check): its
		// extract leg needs an available extract provider, which the keyless
		// httpfetch backstop guarantees in the default service.
		saeTool := mcp.NewTool(
			"search_and_extract",
			mcp.WithDescription(searchAndExtractToolDescription),
			mcp.WithString("query", mcp.Required(), mcp.Description("Natural-language web search or internet research query")),
			mcp.WithString("task", mcp.Description(taskDesc), mcp.Enum(buildTaskEnumValues()...)),
			mcp.WithNumber("limit", mcp.Description("Maximum number of search results to return")),
			mcp.WithNumber("extract_top", mcp.Description("How many of the top results to also extract (default 1, max 3)")),
			mcp.WithString("country", mcp.Description("Optional two-letter search country code (for supported providers, e.g. us, tr)")),
			mcp.WithString("search_lang", mcp.Description("Optional search result language code for supported providers (e.g. en)")),
			mcp.WithString("ui_lang", mcp.Description("Optional provider UI locale/language code (e.g. en-us)")),
			mcp.WithString("safesearch", mcp.Description("Optional safe search setting for supported providers: off, moderate, or strict")),
			mcp.WithString("freshness", mcp.Description("Optional freshness window: pd/day, pw/week, pm/month, or py/year")),
			mcp.WithBoolean("include_trace", mcp.Description(includeTraceDescription)),
		)
		s.AddTool(saeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, err := req.RequireString("query")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			task := core.NormalizeTaskParam(req.GetString("task", ""))
			limit := int(req.GetFloat("limit", 5))
			extractTop := int(req.GetFloat("extract_top", 1))
			options := searchOptionsFromMCP(req)
			resp, err := svc.SearchAndExtract(ctx, core.SearchAndExtractRequest{Query: query, Task: task, Limit: limit, ExtractTop: extractTop, Options: options})
			if err != nil {
				return mcp.NewToolResultError(string(toolErrorJSON("search_and_extract", err, resp.Search.Route, resp.Search.RouteTrace))), nil
			}
			if !req.GetBool("include_trace", false) {
				resp.Search.RouteTrace = nil
				for i := range resp.Extracts {
					resp.Extracts[i].RouteTrace = nil
				}
			}
			b, err := json.MarshalIndent(resp, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
		})
	}

	statusTool := mcp.NewTool("provider_status", mcp.WithDescription("Show configured provider health plus sanitized cost policy/class status"))
	s.AddTool(statusTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		b, err := json.MarshalIndent(svc.ProviderStatus(ctx), "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	})

	budgetTool := mcp.NewTool("budget_status", mcp.WithDescription("Show local cost policy, cap, spend, and free-tier budget/quota status"))
	s.AddTool(budgetTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		b, err := json.MarshalIndent(svc.BudgetStatus(), "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	})

	// research is registered unconditionally: even with no extract provider it
	// still returns deduplicated multi-source SOURCES (honest evidence), so
	// gating it would hide a genuinely useful capability.
	researchTool := mcp.NewTool(
		"research",
		mcp.WithDescription(researchToolDescription),
		mcp.WithString("question", mcp.Required(), mcp.Description("The research question to investigate across multiple sources")),
		mcp.WithNumber("max_steps", mcp.Description("Maximum search passes; also caps how many sources are extracted (default 3)")),
	)
	s.AddTool(researchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		question, err := req.RequireString("question")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		maxSteps := int(req.GetFloat("max_steps", 3))
		report, err := svc.Research(ctx, question, maxSteps)
		if err != nil {
			// ResearchReport carries no route/trace, so the toolErrorJSON envelope
			// (which requires them) does not apply; return a sanitized message.
			return mcp.NewToolResultError(safeerr.Message(err)), nil
		}
		b, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	})
}

func searchOptionsFromMCP(req mcp.CallToolRequest) core.SearchOptions {
	return core.SearchOptions{
		Country:    req.GetString("country", ""),
		SearchLang: req.GetString("search_lang", ""),
		UILang:     req.GetString("ui_lang", ""),
		SafeSearch: req.GetString("safesearch", ""),
		Freshness:  req.GetString("freshness", ""),
	}
}

// buildTaskDescription generates the task parameter description from the canonical task list.
func buildTaskDescription() string {
	parts := make([]string, 0, len(core.TaskTypes()))
	for _, t := range core.TaskTypes() {
		if t == core.TaskExtract {
			continue // extract is not a search task
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", string(t), core.TaskDescription(t)))
	}
	return "Optional. If omitted, Nólë auto-detects the task from the query. Task type: " + strings.Join(parts, ", ")
}

// buildTaskEnumValues returns the canonical search-task values advertised on the
// MCP `task` enum (every TaskType except the extract routing key). The enum is
// advisory — leniency and aliasing are enforced server-side via
// NormalizeTaskParam — but it typo-proofs the common case for the agent.
func buildTaskEnumValues() []string {
	values := make([]string, 0, len(core.TaskTypes()))
	for _, t := range core.TaskTypes() {
		if t == core.TaskExtract {
			continue
		}
		values = append(values, string(t))
	}
	return values
}

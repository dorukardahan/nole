package firecrawl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

const (
	openClawBridgeTimeout = 30 * time.Second
	openClawStderrLimit   = 4096
)

var errOpenClawOutputTooLarge = errors.New("openclaw output exceeded limit")

// OpenClawCommandRunner executes the authenticated OpenClaw CLI. Production
// uses exec.CommandContext; tests inject a deterministic runner. The bridge is
// explicit opt-in through WithOpenClawBridge and is never selected by the
// provider merely because an OpenClaw binary happens to be on PATH.
type OpenClawCommandRunner func(ctx context.Context, command string, args ...string) ([]byte, error)

// OpenClawBridgeMode controls which host capabilities the dedicated wrapper
// advertises. Setup writes the mode explicitly after inspecting OpenClaw's
// installed Firecrawl plugin.
type OpenClawBridgeMode string

const (
	OpenClawBridgeFull      OpenClawBridgeMode = "full"
	OpenClawBridgeFetchOnly OpenClawBridgeMode = "fetch-only"
)

func WithOpenClawBridge(cliPath string) Option {
	return WithOpenClawBridgeMode(cliPath, OpenClawBridgeFull)
}

func WithOpenClawBridgeMode(cliPath string, mode OpenClawBridgeMode) Option {
	return func(p *Provider) {
		p.openClawCLI = strings.TrimSpace(cliPath)
		p.openClawMode = mode
	}
}

func WithOpenClawCommandRunner(runner OpenClawCommandRunner) Option {
	return func(p *Provider) {
		if runner != nil {
			p.openClawRunner = runner
		}
	}
}

func runOpenClawCommand(ctx context.Context, command string, args ...string) ([]byte, error) {
	return runOpenClawCommandWithLimit(ctx, providerhttp.MaxExtractResponseBytes, command, args...)
}

func runOpenClawCommandWithLimit(ctx context.Context, outputLimit int64, command string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = sanitizedOpenClawCommandEnv(os.Environ())
	stdout := &boundedCommandBuffer{limit: outputLimit}
	stderr := &boundedCommandBuffer{limit: openClawStderrLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if stdout.Overflowed() {
		return nil, errOpenClawOutputTooLarge
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}

func sanitizedOpenClawCommandEnv(environ []string) []string {
	stripped := map[string]struct{}{
		"FIRECRAWL_API_KEY":    {},
		"BRAVE_API_KEY":        {},
		"BRAVE_SEARCH_API_KEY": {},
		"TAVILY_API_KEY":       {},
	}
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, found := strings.Cut(entry, "=")
		if found {
			remove := false
			for secretName := range stripped {
				if strings.EqualFold(name, secretName) {
					remove = true
					break
				}
			}
			if remove {
				continue
			}
		}
		result = append(result, entry)
	}
	return result
}

type boundedCommandBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	overflow bool
}

func (b *boundedCommandBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - int64(b.buffer.Len())
	if remaining > 0 {
		if int64(len(p)) > remaining {
			b.overflow = true
			p = p[:remaining]
		}
		_, _ = b.buffer.Write(p)
	} else if original > 0 {
		b.overflow = true
	}
	// Report the original length so a child process cannot fail only because
	// diagnostic output exceeded our local retention cap.
	return original, nil
}

func (b *boundedCommandBuffer) Bytes() []byte    { return b.buffer.Bytes() }
func (b *boundedCommandBuffer) Overflowed() bool { return b.overflow }

type openClawToolsInvokeParams struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type openClawToolsInvokeResult struct {
	OK       bool                    `json:"ok"`
	ToolName string                  `json:"toolName"`
	Output   openClawAgentToolResult `json:"output"`
	Error    *struct {
		Code string `json:"code"`
	} `json:"error,omitempty"`
}

type openClawAgentToolResult struct {
	Details json.RawMessage `json:"details"`
}

type openClawSearchDetails struct {
	Provider string `json:"provider"`
	Results  []struct {
		Title       string   `json:"title"`
		URL         string   `json:"url"`
		Description string   `json:"description"`
		Content     string   `json:"content"`
		Published   string   `json:"published"`
		PublishedAt string   `json:"publishedAt"`
		Score       *float64 `json:"score"`
	} `json:"results"`
}

type openClawFetchDetails struct {
	URL       string `json:"url"`
	FinalURL  string `json:"finalUrl"`
	Status    int    `json:"status"`
	Title     string `json:"title"`
	Extractor string `json:"extractor"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	FetchedAt string `json:"fetchedAt"`
	External  struct {
		Provider string `json:"provider"`
		Source   string `json:"source"`
	} `json:"externalContent"`
}

func (p Provider) openClawBridgeEnabled() bool {
	return strings.TrimSpace(p.openClawCLI) != ""
}

func (p Provider) openClawSearchBridgeEnabled() bool {
	return p.openClawBridgeEnabled() && p.openClawMode == OpenClawBridgeFull
}

func (p Provider) invokeOpenClawTool(ctx context.Context, toolName string, args map[string]any) (output openClawAgentToolResult, err error) {
	allowed, generation := p.breaker.Allow()
	if !allowed {
		return openClawAgentToolResult{}, providerhttp.ErrCircuitOpen
	}
	defer func() {
		if err == nil {
			p.breaker.RecordSuccess(generation)
			return
		}
		if ctx.Err() != nil {
			p.breaker.RecordCancellation(generation)
			return
		}
		p.breaker.RecordFailure(generation)
	}()

	paramsJSON, err := json.Marshal(openClawToolsInvokeParams{Name: toolName, Args: args})
	if err != nil {
		return openClawAgentToolResult{}, fmt.Errorf("firecrawl: OpenClaw bridge request encoding failed")
	}
	bridgeCtx, cancel := context.WithTimeout(ctx, openClawBridgeTimeout)
	defer cancel()
	runner := p.openClawRunner
	if runner == nil {
		runner = runOpenClawCommand
	}
	raw, err := runner(
		bridgeCtx,
		p.openClawCLI,
		"gateway", "call", "tools.invoke",
		"--params", string(paramsJSON),
		"--json",
	)
	if err != nil {
		if ctx.Err() != nil {
			return openClawAgentToolResult{}, fmt.Errorf("firecrawl: OpenClaw bridge invocation cancelled")
		}
		if bridgeCtx.Err() != nil {
			return openClawAgentToolResult{}, fmt.Errorf("firecrawl: OpenClaw bridge timed out")
		}
		if errors.Is(err, errOpenClawOutputTooLarge) {
			return openClawAgentToolResult{}, fmt.Errorf("firecrawl: OpenClaw bridge response too large")
		}
		return openClawAgentToolResult{}, fmt.Errorf("firecrawl: OpenClaw bridge invocation failed")
	}
	if len(raw) == 0 || int64(len(raw)) > providerhttp.MaxExtractResponseBytes {
		return openClawAgentToolResult{}, fmt.Errorf("firecrawl: OpenClaw bridge returned an invalid response size")
	}
	var result openClawToolsInvokeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return openClawAgentToolResult{}, fmt.Errorf("firecrawl: OpenClaw bridge returned invalid JSON")
	}
	if !result.OK || result.ToolName != toolName || len(result.Output.Details) == 0 || string(result.Output.Details) == "null" {
		return openClawAgentToolResult{}, fmt.Errorf("firecrawl: OpenClaw bridge tool call failed")
	}
	return result.Output, nil
}

func isOpenClawFirecrawlProvider(provider string) bool {
	return provider == "firecrawl-free"
}

func (p Provider) searchViaOpenClaw(ctx context.Context, req core.SearchRequest, limit int) (core.SearchResponse, error) {
	hostLimit := limit
	if hostLimit > 10 {
		hostLimit = 10
	}
	args := map[string]any{
		"query": req.Query,
		"count": hostLimit,
	}
	output, err := p.invokeOpenClawTool(ctx, "web_search", args)
	if err != nil {
		return core.SearchResponse{}, err
	}
	var details openClawSearchDetails
	if err := json.Unmarshal(output.Details, &details); err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: OpenClaw web_search details were invalid")
	}
	if !isOpenClawFirecrawlProvider(details.Provider) {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: unexpected OpenClaw web_search provider")
	}
	results := make([]core.SearchResult, 0, len(details.Results))
	for _, item := range details.Results {
		snippet := item.Description
		if snippet == "" {
			snippet = item.Content
		}
		publishedAt := item.PublishedAt
		if publishedAt == "" {
			publishedAt = item.Published
		}
		results = append(results, core.SearchResult{
			Title:       item.Title,
			URL:         item.URL,
			Snippet:     core.TruncateRunes(snippet, 300),
			Provider:    "firecrawl",
			Score:       item.Score,
			PublishedAt: publishedAt,
		})
	}
	return core.SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: "firecrawl",
		Results:  results,
	}, nil
}

func (p Provider) extractViaOpenClaw(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	output, err := p.invokeOpenClawTool(ctx, "web_fetch", map[string]any{"url": req.URL})
	if err != nil {
		return core.ExtractResponse{}, err
	}
	var details openClawFetchDetails
	if err := json.Unmarshal(output.Details, &details); err != nil {
		return core.ExtractResponse{}, fmt.Errorf("firecrawl: OpenClaw web_fetch details were invalid")
	}
	if strings.TrimSpace(details.Text) == "" || details.Status >= 400 {
		return core.ExtractResponse{}, fmt.Errorf("firecrawl: OpenClaw web_fetch returned unusable content")
	}
	metadata := map[string]string{
		"extractor": details.Extractor,
		"host_tool": "openclaw_web_fetch",
		"status":    strconv.Itoa(details.Status),
	}
	if details.External.Provider != "" {
		metadata["host_provider"] = details.External.Provider
	} else if details.External.Source != "" {
		metadata["host_provider"] = details.External.Source
	}
	if details.FinalURL != "" {
		metadata["final_url"] = details.FinalURL
	}
	if details.Title != "" {
		metadata["title"] = details.Title
	}
	if details.FetchedAt != "" {
		metadata["fetched_at"] = details.FetchedAt
	}
	if details.Truncated {
		metadata["truncated"] = "true"
	}
	return core.ExtractResponse{
		URL:      req.URL,
		Provider: "firecrawl",
		Content:  details.Text,
		Metadata: metadata,
	}, nil
}

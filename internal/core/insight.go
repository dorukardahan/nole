package core

import (
	"fmt"
	"strings"
)

type InsightMode string

const (
	InsightCompact InsightMode = "compact"
	InsightOff     InsightMode = "off"
	InsightVerbose InsightMode = "verbose"
)

func ParseInsightMode(raw string) (InsightMode, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(InsightCompact):
		return InsightCompact, true
	case string(InsightOff):
		return InsightOff, true
	case string(InsightVerbose):
		return InsightVerbose, true
	default:
		return "", false
	}
}

func BuildSearchRoutingInsight(resp SearchResponse) string {
	return buildRuntimeRoutingInsight("search", resp.Task, resp.Provider, resp.Route, resp.RouteTrace, len(resp.Results))
}

func BuildExtractRoutingInsight(resp ExtractResponse) string {
	resultCount := 0
	if strings.TrimSpace(resp.Content) != "" {
		resultCount = 1
	}
	return buildRuntimeRoutingInsight("extract", TaskExtract, resp.Provider, resp.Route, resp.RouteTrace, resultCount)
}

func BuildErrorRoutingInsight(operation string, route []string, trace []RouteAttempt) string {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "request"
	}
	attempts := len(trace)
	total := routeSlotCount(route, trace)
	if attempts == 0 && total == 0 {
		return fmt.Sprintf("Nólë: %s failed before routing", operation)
	}
	return fmt.Sprintf("Nólë: %s failed (%s)", operation, attemptSummary(attempts, total))
}

func BuildClassificationRoutingInsight(classification QueryClassification) string {
	if len(classification.Intents) == 0 {
		return "Nólë: classified as general (no intent signals)"
	}
	parts := make([]string, 0, minInt(len(classification.Intents), 3))
	for _, intent := range classification.Intents[:minInt(len(classification.Intents), 3)] {
		parts = append(parts, intent.Label)
	}
	if len(classification.Intents) > 3 {
		parts = append(parts, fmt.Sprintf("+%d more", len(classification.Intents)-3))
	}
	return fmt.Sprintf("Nólë: classified %s (%s)", classification.PrimaryTask, strings.Join(parts, ", "))
}

func BuildRoutePlanRoutingInsight(plan RoutePlan) string {
	if len(plan.Routes) == 0 {
		return "Nólë: route-plan planned no routes"
	}
	parts := make([]string, 0, minInt(len(plan.Routes), 3))
	for _, route := range plan.Routes[:minInt(len(plan.Routes), 3)] {
		provider := "none"
		if len(route.Route) > 0 {
			provider = route.Route[0]
		}
		parts = append(parts, fmt.Sprintf("%s via %s", route.Label, provider))
	}
	if len(plan.Routes) > 3 {
		parts = append(parts, fmt.Sprintf("+%d more", len(plan.Routes)-3))
	}
	return fmt.Sprintf("Nólë: route-plan planned %s (%s, %d provider slots)", strings.Join(parts, ", "), plural(len(plan.Routes), "intent", "intents"), len(plan.RouteTrace))
}

func FormatRouteTraceLines(trace []RouteAttempt) []string {
	lines := make([]string, 0, len(trace))
	for _, attempt := range trace {
		status := attempt.Status
		if status == "" {
			status = "unknown"
		}
		parts := []string{fmt.Sprintf("%s: %s", attempt.Provider, status)}
		if attempt.Reason != "" {
			parts = append(parts, "reason="+attempt.Reason)
		}
		if attempt.CostPolicy != "" {
			parts = append(parts, "cost_policy="+string(attempt.CostPolicy))
		}
		if attempt.CostClass != "" {
			parts = append(parts, "cost_class="+string(attempt.CostClass))
		}
		if attempt.ResultCount > 0 || attempt.Status == "success" {
			parts = append(parts, fmt.Sprintf("results=%d", attempt.ResultCount))
		}
		if attempt.LatencyMS > 0 {
			parts = append(parts, fmt.Sprintf("latency_ms=%d", attempt.LatencyMS))
		}
		lines = append(lines, strings.Join(parts, " "))
	}
	return lines
}

func buildRuntimeRoutingInsight(operation string, task TaskType, provider string, route []string, trace []RouteAttempt, resultCount int) string {
	attempts := len(trace)
	total := routeSlotCount(route, trace)
	policy := tracePolicy(trace)
	policyPart := ""
	if policy != "" {
		policyPart = string(policy) + ", "
	}
	if provider == "" {
		provider = successProvider(trace)
	}
	if operation == "extract" {
		if provider == "" {
			return fmt.Sprintf("Nólë: extract failed (%s)", attemptSummary(attempts, total))
		}
		return fmt.Sprintf("Nólë: extract page via %s (%s%s, content extracted)", provider, policyPart, attemptSummary(attempts, total))
	}
	if task == "" {
		task = TaskGeneral
	}
	if provider == "" {
		return fmt.Sprintf("Nólë: search %s failed (%s)", task, attemptSummary(attempts, total))
	}
	return fmt.Sprintf("Nólë: search %s via %s (%s%s, %s)", task, provider, policyPart, attemptSummary(attempts, total), plural(resultCount, "result", "results"))
}

func tracePolicy(trace []RouteAttempt) CostPolicy {
	for i := len(trace) - 1; i >= 0; i-- {
		if trace[i].Status == "success" && trace[i].CostPolicy != "" {
			return trace[i].CostPolicy
		}
	}
	for _, attempt := range trace {
		if attempt.CostPolicy != "" {
			return attempt.CostPolicy
		}
	}
	return ""
}

func successProvider(trace []RouteAttempt) string {
	for i := len(trace) - 1; i >= 0; i-- {
		if trace[i].Status == "success" {
			return trace[i].Provider
		}
	}
	return ""
}

func routeSlotCount(route []string, trace []RouteAttempt) int {
	if len(route) > 0 {
		return len(route)
	}
	return len(trace)
}

func attemptSummary(attempts, total int) string {
	if total <= 0 {
		total = attempts
	}
	return fmt.Sprintf("%d/%d attempts", attempts, total)
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

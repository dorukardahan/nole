package core

import (
	"sort"
	"strings"
	"unicode"
)

const PlannerRuleVersion = "2026-05-18.rule.v1"

type PlanOptions struct {
	TaskOverride     TaskType
	ProviderOverride []string
	SingleIntent     bool
}

type PlanOverrideReport struct {
	Task         TaskType `json:"task,omitempty"`
	Providers    []string `json:"providers,omitempty"`
	SingleIntent bool     `json:"single_intent,omitempty"`
}

type PlannedIntent struct {
	Task    TaskType `json:"task"`
	Label   string   `json:"label"`
	Score   int      `json:"score"`
	Signals []string `json:"signals,omitempty"`
	Reason  string   `json:"reason"`
}

type QueryClassification struct {
	Query          string             `json:"query"`
	PrimaryTask    TaskType           `json:"primary_task"`
	Ambiguous      bool               `json:"ambiguous"`
	RuleVersion    string             `json:"rule_version"`
	Intents        []PlannedIntent    `json:"intents"`
	RoutingInsight string             `json:"routing_insight,omitempty"`
	Overrides      PlanOverrideReport `json:"overrides,omitempty"`
}

type PlannedRoute struct {
	Task   TaskType `json:"task"`
	Label  string   `json:"label"`
	Route  []string `json:"route"`
	Reason string   `json:"reason"`
}

type RoutePlan struct {
	Query          string             `json:"query"`
	PrimaryTask    TaskType           `json:"primary_task"`
	Ambiguous      bool               `json:"ambiguous"`
	RuleVersion    string             `json:"rule_version"`
	Intents        []PlannedIntent    `json:"intents"`
	Routes         []PlannedRoute     `json:"routes"`
	RoutingInsight string             `json:"routing_insight,omitempty"`
	RouteTrace     []RouteAttempt     `json:"route_trace"`
	Overrides      PlanOverrideReport `json:"overrides,omitempty"`
}

type intentRule struct {
	task    TaskType
	label   string
	phrases []weightedPhrase
}

type weightedPhrase struct {
	phrase string
	weight int
}

var plannerRules = []intentRule{
	{task: TaskDocs, label: "docs", phrases: []weightedPhrase{
		{"documentation", 4}, {"docs", 4}, {"api reference", 4}, {"reference", 2}, {"manual", 2}, {"sdk", 2}, {"quickstart", 2}, {"configuration", 2}, {"configure", 2}, {"tutorial", 1}, {"guide", 1},
	}},
	{task: TaskPricing, label: "pricing", phrases: []weightedPhrase{
		{"pricing", 5}, {"price", 4}, {"prices", 4}, {"cost", 4}, {"costs", 4}, {"billing", 4}, {"plans", 3}, {"plan", 2}, {"quota", 2}, {"rate limit", 2}, {"limits", 1}, {"overage", 3},
	}},
	{task: TaskAcademic, label: "academic", phrases: []weightedPhrase{
		{"arxiv", 5}, {"paper", 4}, {"papers", 4}, {"scholar", 4}, {"doi", 4}, {"survey", 3}, {"citation", 3}, {"journal", 3}, {"academic", 3}, {"research paper", 4}, {"literature", 2},
	}},
	{task: TaskNews, label: "news", phrases: []weightedPhrase{
		{"latest", 4}, {"news", 5}, {"today", 3}, {"recent", 3}, {"current", 2}, {"announced", 3}, {"announcement", 3}, {"changelog", 2}, {"release notes", 2}, {"breaking", 4}, {"this week", 3}, {"tonight", 3}, {"upcoming", 3}, {"concert", 3}, {"concerts", 3},
	}},
	{task: TaskCode, label: "code", phrases: []weightedPhrase{
		{"github", 4}, {"code", 4}, {"implementation", 4}, {"implement", 3}, {"example", 2}, {"examples", 2}, {"snippet", 3}, {"function", 2}, {"library", 2}, {"package", 1}, {"bug", 3}, {"error", 3}, {"stack overflow", 3}, {"stackoverflow", 3},
	}},
	{task: TaskSocial, label: "community", phrases: []weightedPhrase{
		{"reddit", 5}, {"forum", 4}, {"forums", 4}, {"community", 4}, {"discord", 4}, {"hacker news", 4}, {"hn", 2}, {"discussion", 3}, {"discussions", 3}, {"reviews", 2}, {"people say", 3}, {"experience", 2},
	}},
	{task: TaskFactcheck, label: "factcheck", phrases: []weightedPhrase{
		{"fact check", 5}, {"fact-check", 5}, {"is it true", 4}, {"debunk", 4}, {"hoax", 4}, {"verify", 2},
	}},
	{task: TaskPeople, label: "people", phrases: []weightedPhrase{
		{"who is", 4}, {"profile", 3}, {"biography", 3}, {"bio", 2}, {"founder", 2},
	}},
	{task: TaskResearch, label: "research", phrases: []weightedPhrase{
		{"compare", 2}, {"comparison", 2}, {"landscape", 3}, {"overview", 2}, {"alternatives", 2}, {"deep dive", 3},
	}},
	{task: TaskSemantic, label: "semantic", phrases: []weightedPhrase{
		{"semantic search", 4}, {"related concepts", 3}, {"find similar", 3}, {"conceptually", 3},
	}},
}

// ClassifyQuery performs deterministic, LLM-free query intent classification.
func ClassifyQuery(query string, opts PlanOptions) QueryClassification {
	overrides := buildOverrideReport(opts)
	if opts.TaskOverride != "" {
		intent := PlannedIntent{Task: opts.TaskOverride, Label: taskLabel(opts.TaskOverride), Score: 100, Reason: "task override"}
		classification := QueryClassification{
			Query:       query,
			PrimaryTask: opts.TaskOverride,
			Ambiguous:   false,
			RuleVersion: PlannerRuleVersion,
			Intents:     []PlannedIntent{intent},
			Overrides:   overrides,
		}
		classification.RoutingInsight = BuildClassificationRoutingInsight(classification)
		return classification
	}

	norm := normalizeQuery(query)
	intents := scoreIntents(norm)
	ambiguous := false
	if len(intents) == 0 {
		ambiguous = true
		intents = []PlannedIntent{{Task: TaskGeneral, Label: taskLabel(TaskGeneral), Score: 0, Reason: "no strong task signals; use broad web route"}}
	} else {
		ambiguous = hasTopScoreTie(intents)
		if opts.SingleIntent && len(intents) > 1 {
			intents = intents[:1]
		}
	}

	classification := QueryClassification{
		Query:       query,
		PrimaryTask: intents[0].Task,
		Ambiguous:   ambiguous,
		RuleVersion: PlannerRuleVersion,
		Intents:     intents,
		Overrides:   overrides,
	}
	classification.RoutingInsight = BuildClassificationRoutingInsight(classification)
	return classification
}

// BuildRoutePlan classifies a query and returns provider routes without making provider calls.
func BuildRoutePlan(query string, matrix RouteMatrix, opts PlanOptions) RoutePlan {
	classification := ClassifyQuery(query, opts)
	routes := make([]PlannedRoute, 0, len(classification.Intents))
	trace := []RouteAttempt{}
	for _, intent := range classification.Intents {
		route := routeForPlan(matrix, intent.Task, opts.ProviderOverride)
		reason := "ranked_for_" + string(intent.Task)
		if len(opts.ProviderOverride) > 0 {
			reason = "provider_override"
		}
		routes = append(routes, PlannedRoute{Task: intent.Task, Label: intent.Label, Route: route, Reason: reason})
		for _, provider := range route {
			trace = append(trace, RouteAttempt{Provider: provider, Status: "planned", Reason: reason})
		}
	}
	plan := RoutePlan{
		Query:       query,
		PrimaryTask: classification.PrimaryTask,
		Ambiguous:   classification.Ambiguous,
		RuleVersion: PlannerRuleVersion,
		Intents:     classification.Intents,
		Routes:      routes,
		RouteTrace:  trace,
		Overrides:   classification.Overrides,
	}
	plan.RoutingInsight = BuildRoutePlanRoutingInsight(plan)
	return plan
}

func scoreIntents(norm string) []PlannedIntent {
	intents := []PlannedIntent{}
	for _, rule := range plannerRules {
		score := 0
		signals := []string{}
		for _, phrase := range rule.phrases {
			if containsPhrase(norm, phrase.phrase) {
				score += phrase.weight
				signals = append(signals, phrase.phrase)
			}
		}
		if score == 0 {
			continue
		}
		intents = append(intents, PlannedIntent{
			Task:    rule.task,
			Label:   rule.label,
			Score:   score,
			Signals: signals,
			Reason:  "matched rule signals: " + strings.Join(signals, ", "),
		})
	}
	sort.SliceStable(intents, func(i, j int) bool {
		if intents[i].Score != intents[j].Score {
			return intents[i].Score > intents[j].Score
		}
		return taskPriority(intents[i].Task) < taskPriority(intents[j].Task)
	})
	return intents
}

func normalizeQuery(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 2)
	b.WriteByte(' ')
	for _, r := range strings.ToLower(query) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte(' ')
	}
	b.WriteByte(' ')
	return strings.Join(strings.Fields(b.String()), " ")
}

func containsPhrase(norm, phrase string) bool {
	needle := normalizeQuery(phrase)
	if needle == "" {
		return false
	}
	return strings.Contains(" "+norm+" ", " "+needle+" ")
}

func hasTopScoreTie(intents []PlannedIntent) bool {
	if len(intents) < 2 {
		return false
	}
	return intents[0].Score == intents[1].Score
}

func routeForPlan(matrix RouteMatrix, task TaskType, providerOverride []string) []string {
	if len(providerOverride) > 0 {
		return append([]string(nil), providerOverride...)
	}
	route := matrix[task]
	if len(route) == 0 {
		route = matrix[TaskGeneral]
	}
	return append([]string(nil), route...)
}

func buildOverrideReport(opts PlanOptions) PlanOverrideReport {
	return PlanOverrideReport{Task: opts.TaskOverride, Providers: append([]string(nil), opts.ProviderOverride...), SingleIntent: opts.SingleIntent}
}

func taskLabel(task TaskType) string {
	for _, rule := range plannerRules {
		if rule.task == task {
			return rule.label
		}
	}
	if task == TaskGeneral {
		return "general"
	}
	return string(task)
}

func taskPriority(task TaskType) int {
	for i, candidate := range []TaskType{TaskDocs, TaskPricing, TaskAcademic, TaskNews, TaskCode, TaskSocial, TaskFactcheck, TaskPeople, TaskResearch, TaskSemantic, TaskGeneral} {
		if task == candidate {
			return i
		}
	}
	return 100
}

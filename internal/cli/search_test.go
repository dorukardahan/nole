package cli

import (
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestSearchCommandTaskHelpIncludesAllSearchTasks(t *testing.T) {
	help := taskHelpText()
	for _, task := range core.TaskTypes() {
		if task == core.TaskExtract {
			continue
		}
		if !strings.Contains(help, string(task)) {
			t.Errorf("task help missing task %q: %s", task, help)
		}
	}
}

func TestSearchCommandTaskHelpExcludesExtract(t *testing.T) {
	help := taskHelpText()
	if strings.Contains(help, "extract") {
		t.Errorf("task help should not include extract: %s", help)
	}
}

func TestParseTaskAcceptsAllValidTasks(t *testing.T) {
	validTasks := []string{
		"general", "news", "docs", "academic", "factcheck",
		"semantic", "code", "social", "people", "pricing",
		"research", "extract",
	}
	for _, raw := range validTasks {
		result := parseTask(raw)
		if string(result) != raw {
			t.Errorf("parseTask(%q) = %q, want %q", raw, result, raw)
		}
	}
}

func TestParseTaskFallbackToGeneral(t *testing.T) {
	result := parseTask("unknown")
	if result != core.TaskGeneral {
		t.Errorf("parseTask(unknown) = %q, want general", result)
	}
}

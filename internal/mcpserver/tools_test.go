package mcpserver

import (
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestBuildTaskDescriptionIncludesAllSearchTasks(t *testing.T) {
	desc := buildTaskDescription()
	for _, task := range core.TaskTypes() {
		if task == core.TaskExtract {
			continue // extract is not a search task
		}
		if !strings.Contains(desc, string(task)) {
			t.Errorf("task description missing task %q: %s", task, desc)
		}
	}
}

func TestBuildTaskDescriptionExcludesExtract(t *testing.T) {
	desc := buildTaskDescription()
	if strings.Contains(desc, "extract") {
		t.Errorf("task description should not include extract: %s", desc)
	}
}

func TestBuildTaskDescriptionHasDescriptions(t *testing.T) {
	desc := buildTaskDescription()
	if !strings.Contains(desc, "news") || !strings.Contains(desc, "academic") {
		t.Errorf("task description missing expected tasks: %s", desc)
	}
	if !strings.Contains(desc, "pricing") || !strings.Contains(desc, "semantic") {
		t.Errorf("task description missing later tasks: %s", desc)
	}
}

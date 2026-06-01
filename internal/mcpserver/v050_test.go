package mcpserver

import (
	"strings"
	"testing"
)

func TestBuildTaskEnumValues(t *testing.T) {
	values := buildTaskEnumValues()
	set := map[string]bool{}
	for _, v := range values {
		set[v] = true
	}
	for _, want := range []string{"general", "news", "docs", "social", "semantic", "research"} {
		if !set[want] {
			t.Errorf("task enum missing canonical value %q: %v", want, values)
		}
	}
	if set["extract"] {
		t.Errorf("task enum must exclude extract: %v", values)
	}
	if set["community"] {
		t.Errorf("task enum should advertise canonical 'social', not alias 'community': %v", values)
	}
}

func TestBuildTaskDescriptionHasAutoDetectNote(t *testing.T) {
	desc := buildTaskDescription()
	if !strings.Contains(desc, "auto-detect") && !strings.Contains(desc, "If omitted") {
		t.Errorf("task description should note auto-detection when omitted: %s", desc)
	}
}

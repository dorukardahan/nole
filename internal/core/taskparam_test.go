package core

import "testing"

func TestNormalizeTaskParam(t *testing.T) {
	cases := []struct {
		in   string
		want TaskType
	}{
		{"news", TaskNews},
		{"NEWS", TaskNews},
		{" docs ", TaskDocs},
		{"technical-docs", TaskDocs},
		{"community", TaskSocial}, // alias → canonical
		{"forum", TaskSocial},
		{"social", TaskSocial},
		{"semantic", TaskSemantic},
		{"research", TaskResearch},
		{"deep-research", TaskResearch},
		{"general", TaskGeneral},
		{"", ""},        // blank → service classifies
		{"bogus", ""},   // unknown → service classifies
		{"extract", ""}, // extract is not a search task → classify
	}
	for _, tc := range cases {
		if got := NormalizeTaskParam(tc.in); got != tc.want {
			t.Errorf("NormalizeTaskParam(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsKnownSearchTask(t *testing.T) {
	for _, task := range TaskTypes() {
		want := task != TaskExtract
		if got := IsKnownSearchTask(task); got != want {
			t.Errorf("IsKnownSearchTask(%q) = %v, want %v", task, got, want)
		}
	}
	if IsKnownSearchTask(TaskType("bogus")) {
		t.Error("IsKnownSearchTask(bogus) should be false")
	}
	if IsKnownSearchTask("") {
		t.Error(`IsKnownSearchTask("") should be false`)
	}
}

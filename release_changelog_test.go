package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const releaseChangelogFixture = `# Changelog

## [Unreleased]

### Added

- Something not released yet.

## [1.4.0] - 2026-05-02

### Fixed

- A released fix.

` + "```text" + `
## [9.9.9] - 2026-01-01
` + "```" + `

## [1.3.0] - 2026-04-01

- Older release.

[Unreleased]: https://github.com/dorukardahan/nole/compare/v1.4.0...HEAD
[1.4.0]: https://github.com/dorukardahan/nole/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/dorukardahan/nole/compare/v1.2.0...v1.3.0
`

func writeReleaseChangelogFixture(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func runReleaseChangelogCheck(t *testing.T, tag, root string) (string, error) {
	t.Helper()
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts", "check-release-changelog.sh"), tag, root)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestReleaseChangelogCheckAcceptsDocumentedTag(t *testing.T) {
	root := writeReleaseChangelogFixture(t, releaseChangelogFixture)
	out, err := runReleaseChangelogCheck(t, "v1.4.0", root)
	if err != nil {
		t.Fatalf("check rejected a documented tag: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK: CHANGELOG.md documents v1.4.0") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestReleaseChangelogCheckSkipsPrereleaseTags(t *testing.T) {
	root := writeReleaseChangelogFixture(t, releaseChangelogFixture)
	out, err := runReleaseChangelogCheck(t, "v1.5.0-rc.1", root)
	if err != nil {
		t.Fatalf("check rejected a prerelease tag: %v\n%s", err, out)
	}
	if !strings.Contains(out, "SKIP:") {
		t.Fatalf("prerelease tag was not reported as skipped:\n%s", out)
	}
}

func TestReleaseChangelogCheckRejectsInconsistentTags(t *testing.T) {
	cases := []struct {
		name      string
		tag       string
		changelog string
		want      string
	}{
		{
			name:      "still unreleased",
			tag:       "v1.5.0",
			changelog: releaseChangelogFixture,
			want:      "no '## [1.5.0] - YYYY-MM-DD' section",
		},
		{
			name:      "version only inside a code fence",
			tag:       "v9.9.9",
			changelog: releaseChangelogFixture,
			want:      "no '## [9.9.9] - YYYY-MM-DD' section",
		},
		{
			name:      "typo prefix does not match a longer version",
			tag:       "v1.4.0",
			changelog: strings.ReplaceAll(releaseChangelogFixture, "## [1.4.0] - 2026-05-02", "## [1.4.00] - 2026-05-02"),
			want:      "no '## [1.4.0] - YYYY-MM-DD' section",
		},
		{
			name:      "malformed tag",
			tag:       "1.4.0",
			changelog: releaseChangelogFixture,
			want:      "release tag must look like vMAJOR.MINOR.PATCH",
		},
		{
			name:      "undated section",
			tag:       "v1.4.0",
			changelog: strings.ReplaceAll(releaseChangelogFixture, "## [1.4.0] - 2026-05-02", "## [1.4.0]"),
			want:      "must be dated",
		},
		{
			name:      "empty section",
			tag:       "v1.4.0",
			changelog: strings.ReplaceAll(releaseChangelogFixture, "### Fixed\n\n- A released fix.\n", ""),
			want:      "section for 1.4.0 is empty",
		},
		{
			name:      "only empty subsection headings",
			tag:       "v1.4.0",
			changelog: strings.ReplaceAll(releaseChangelogFixture, "### Fixed\n\n- A released fix.\n", "### Added\n\n### Fixed\n\n"),
			want:      "section for 1.4.0 is empty",
		},
		{
			name:      "duplicate section",
			tag:       "v1.4.0",
			changelog: strings.ReplaceAll(releaseChangelogFixture, "## [1.3.0] - 2026-04-01", "## [1.4.0] - 2026-04-01"),
			want:      "has 2 sections for 1.4.0",
		},
		{
			name:      "missing compare link",
			tag:       "v1.4.0",
			changelog: strings.ReplaceAll(releaseChangelogFixture, "[1.4.0]: https://github.com/dorukardahan/nole/compare/v1.3.0...v1.4.0\n", ""),
			want:      "missing the '[1.4.0]: .../compare/...v1.4.0' link reference",
		},
		{
			name:      "compare link points at another tag",
			tag:       "v1.4.0",
			changelog: strings.ReplaceAll(releaseChangelogFixture, "compare/v1.3.0...v1.4.0", "compare/v1.3.0...v1.4.1"),
			want:      "link reference",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeReleaseChangelogFixture(t, tc.changelog)
			out, err := runReleaseChangelogCheck(t, tc.tag, root)
			if err == nil {
				t.Fatalf("check accepted %s:\n%s", tc.tag, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("check failed for the wrong reason, want %q:\n%s", tc.want, out)
			}
		})
	}
}

// The newest versioned section of the real changelog must always pass, so the
// check cannot drift away from the release-prep format it guards.
func TestReleaseChangelogCheckAcceptsLatestRepositoryRelease(t *testing.T) {
	data, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^## \[([0-9]+\.[0-9]+\.[0-9]+)\] - `).FindSubmatch(data)
	if match == nil {
		t.Fatal("CHANGELOG.md has no versioned release section")
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tag := "v" + string(match[1])
	if out, err := runReleaseChangelogCheck(t, tag, repoRoot); err != nil {
		t.Fatalf("latest changelog release %s does not pass the release check: %v\n%s", tag, err, out)
	}
}

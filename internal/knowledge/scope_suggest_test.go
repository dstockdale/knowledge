package knowledge_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"knowledge/internal/knowledge"
)

func TestSuggestScopesReportsCoverageAndInvalidPaths(t *testing.T) {
	root := t.TempDir()
	codeRoot := t.TempDir()
	mkdirAll(t, codeRoot, "lib/boopbup/analytics")
	mkdirAll(t, codeRoot, "assets/js")
	writeMarkdown(t, root, "architecture/frontend-constitution.md", `# Frontend Constitution

React, CSS, Tailwind, and layout rules.
`)
	writeMarkdown(t, root, "plans/analytics-foundation.md", `---
type: plan
area: analytics
status: active
scope:
  paths:
    - lib/boopbup/analytics/**
    - missing/path/**
---

# Analytics Foundation
`)

	report, err := knowledge.SuggestScopes(root, knowledge.ScopeSuggestionOptions{CodeRoot: codeRoot, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if report.Documents != 2 || report.Scoped != 1 || report.Unscoped != 1 {
		t.Fatalf("coverage = %#v", report)
	}
	if len(report.InvalidScopedPaths) != 1 || report.InvalidScopedPaths[0].Scope != "missing/path/**" {
		t.Fatalf("invalid scoped paths = %#v", report.InvalidScopedPaths)
	}
	if len(report.Suggestions) != 1 {
		t.Fatalf("suggestions = %#v", report.Suggestions)
	}
	suggestion := report.Suggestions[0]
	if suggestion.ID != "architecture.frontend-constitution" {
		t.Fatalf("suggestion ID = %q", suggestion.ID)
	}
	for _, want := range []string{"assets/js/**", "assets/css/**", "lib/boopbup_web/**", "priv/static/**"} {
		if !slices.Contains(suggestion.SuggestedPaths, want) {
			t.Fatalf("missing frontend suggestion %s: %#v", want, suggestion.SuggestedPaths)
		}
	}
}

func TestSuggestScopesSuggestsAnalyticsPaths(t *testing.T) {
	root := t.TempDir()
	writeMarkdown(t, root, "plans/analytics-foundation.md", `---
type: plan
area: analytics
status: active
---

# Analytics Foundation
`)
	report, err := knowledge.SuggestScopes(root, knowledge.ScopeSuggestionOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Suggestions) != 1 {
		t.Fatalf("suggestions = %#v", report.Suggestions)
	}
	suggestion := report.Suggestions[0]
	for _, want := range []string{"lib/boopbup/analytics/**", "priv/repo/migrations/**", "test/boopbup/analytics/**"} {
		if !slices.Contains(suggestion.SuggestedPaths, want) {
			t.Fatalf("missing analytics suggestion %s: %#v", want, suggestion.SuggestedPaths)
		}
	}
}

func mkdirAll(t *testing.T, root, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(path)), 0o755); err != nil {
		t.Fatal(err)
	}
}

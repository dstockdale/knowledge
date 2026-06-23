package knowledge_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"knowledge/internal/knowledge"
)

func TestSearchUsesFTSAndLifecycleFiltering(t *testing.T) {
	ctx := context.Background()
	root := corpusRoot(t)
	dbPath := filepath.Join(t.TempDir(), "index.sqlite")
	results, err := knowledge.Search(ctx, root, dbPath, knowledge.SearchOptions{Query: "registration", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	ids := searchIDs(results)
	if !slices.Contains(ids, "boop.spec.registration") {
		t.Fatalf("registration spec missing from results: %#v", ids)
	}
	if slices.Contains(ids, "boop.idea.social-login-every-provider") {
		t.Fatalf("rejected idea should be excluded by default: %#v", ids)
	}
}

func TestContextForTaskIncludesExpectedDocsAndHistoricalRationale(t *testing.T) {
	ctx := context.Background()
	root := corpusRoot(t)
	dbPath := filepath.Join(t.TempDir(), "index.sqlite")
	manifest, err := knowledge.ContextForTask(ctx, root, dbPath, knowledge.ContextRequest{
		Task:        "Replace password login with passkeys",
		Paths:       []string{"lib/boop/accounts"},
		TokenBudget: 2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := contextIDs(manifest.Documents)
	for _, id := range []string{"boop.adr.authentication-identity", "boop.spec.registration", "boop.plan.passkeys"} {
		if !slices.Contains(ids, id) {
			t.Fatalf("%s missing from context documents: %#v", id, ids)
		}
	}
	if slices.Contains(ids, "boop.idea.social-login-every-provider") {
		t.Fatalf("rejected idea should be excluded by default: %#v", ids)
	}
	historicalIDs := contextIDs(manifest.HistoricalDocuments)
	if !slices.Contains(historicalIDs, "boop.adr.password-only-auth") {
		t.Fatalf("superseded ADR should be listed as historical rationale: %#v", historicalIDs)
	}
	if slices.Contains(historicalIDs, "boop.idea.social-login-every-provider") {
		t.Fatalf("rejected idea should not be suggested as historical rationale: %#v", historicalIDs)
	}
}

func TestAffectedDocumentsMatchesScopedPaths(t *testing.T) {
	results, err := knowledge.AffectedDocuments(corpusRoot(t), []string{"lib/boop/accounts/authentication.ex"})
	if err != nil {
		t.Fatal(err)
	}
	ids := affectedIDs(results)
	for _, id := range []string{"boop.adr.authentication-identity", "boop.spec.registration", "boop.plan.passkeys"} {
		if !slices.Contains(ids, id) {
			t.Fatalf("%s missing from affected docs: %#v", id, ids)
		}
	}
}

func TestAffectedDocumentsNoMatchReturnsEmptySlice(t *testing.T) {
	results, err := knowledge.AffectedDocuments(corpusRoot(t), []string{"assets/js/no-match.js"})
	if err != nil {
		t.Fatal(err)
	}
	if results == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(results) != 0 {
		t.Fatalf("expected no affected documents, got %#v", results)
	}
}

func TestSearchExactSpikeTitleBeatsGenericClientMatches(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(repoRoot(t), "testdata", "obsidian")
	dbPath := filepath.Join(t.TempDir(), "index.sqlite")
	response, err := knowledge.SearchWithValidation(ctx, root, dbPath, knowledge.SearchOptions{Query: "ClickHouse Client Integration Spike", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) == 0 {
		t.Fatal("expected search results")
	}
	if response.Results[0].ID != "ideas.analytics.clickhouse-client-integration-spike" {
		t.Fatalf("top result = %#v", response.Results[0])
	}
	if response.Validation.WarningCount == 0 {
		t.Fatal("expected validation warnings from permissive Obsidian corpus")
	}
}

func TestContextFrontendTaskIncludesConstitution(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(repoRoot(t), "testdata", "obsidian")
	dbPath := filepath.Join(t.TempDir(), "index.sqlite")
	manifest, err := knowledge.ContextForTask(ctx, root, dbPath, knowledge.ContextRequest{
		Task:        "frontend layout work",
		Paths:       []string{"assets/js"},
		TokenBudget: 2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := contextIDs(manifest.Documents)
	if !slices.Contains(ids, "architecture.frontend-constitution") {
		t.Fatalf("frontend constitution missing from context: %#v", ids)
	}
}

func TestContextIncludesGoverningFrontendDocUnderAdminBudgetPressure(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeMarkdown(t, root, "architecture/frontend-constitution.md", `# Frontend Constitution

These rules apply to React, CSS, Tailwind, component, and layout work across admin surfaces.

## Rules

Prefer CSS grid for page layout and preserve existing component rhythm.
`)
	writeMarkdown(t, root, "plans/admin-layout-workspace.md", `---
id: plans.admin-layout-workspace
kind: plan
status: active
title: Admin Layout Workspace
---

# Admin Layout Workspace

## Work Log

`+strings.Repeat("Admin layout workspace details for listing media upload client implementation.\n", 160))

	manifest, err := knowledge.ContextForTask(ctx, root, filepath.Join(t.TempDir(), "index.sqlite"), knowledge.ContextRequest{
		Task:        "build React admin layout for listing media upload",
		Paths:       []string{"assets/js"},
		TokenBudget: 2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := contextIDs(manifest.Documents)
	if !slices.Contains(ids, "architecture.frontend-constitution") {
		t.Fatalf("frontend constitution missing from context: %#v", ids)
	}
	var foundReason bool
	for _, doc := range manifest.Documents {
		if doc.ID == "architecture.frontend-constitution" && slices.Contains(doc.Reasons, "governing frontend match") {
			foundReason = true
		}
	}
	if !foundReason {
		t.Fatalf("frontend constitution missing governing reason: %#v", manifest.Documents)
	}
}

func TestIncludeHistoricalKeepsExactDoneMatchFindable(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(repoRoot(t), "testdata", "obsidian")
	dbPath := filepath.Join(t.TempDir(), "index.sqlite")
	response, err := knowledge.SearchWithValidation(ctx, root, dbPath, knowledge.SearchOptions{
		Query:             "Retired ClickHouse Import Plan",
		IncludeHistorical: true,
		Limit:             5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) == 0 {
		t.Fatal("expected historical exact match")
	}
	if response.Results[0].ID != "plans.completed.retired-clickhouse-import-plan" {
		t.Fatalf("top result = %#v", response.Results[0])
	}
}

func TestSearchContinuesWithInvalidDocuments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeDoc(t, root, "a.md", "duplicate.id", "Duplicate A")
	writeDoc(t, root, "b.md", "duplicate.id", "Duplicate B")
	writeDoc(t, root, "valid.md", "valid.doc", "Valid Search Target")
	dbPath := filepath.Join(t.TempDir(), "index.sqlite")
	response, err := knowledge.SearchWithValidation(ctx, root, dbPath, knowledge.SearchOptions{Query: "Valid Search Target", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	ids := searchIDs(response.Results)
	if !slices.Contains(ids, "valid.doc") {
		t.Fatalf("valid doc missing despite duplicate-id issue: %#v", ids)
	}
	if response.Validation.IssueCount == 0 {
		t.Fatal("expected duplicate-id validation issue")
	}
}

func TestContextEvalFixture(t *testing.T) {
	var evals []struct {
		Task           string   `yaml:"task"`
		Paths          []string `yaml:"paths"`
		MustInclude    []string `yaml:"must_include"`
		MustNotInclude []string `yaml:"must_not_include"`
	}
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "evals", "context.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &evals); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, eval := range evals {
		manifest, err := knowledge.ContextForTask(ctx, corpusRoot(t), filepath.Join(t.TempDir(), "index.sqlite"), knowledge.ContextRequest{
			Task:        eval.Task,
			Paths:       eval.Paths,
			TokenBudget: 3000,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids := contextIDs(manifest.Documents)
		for _, id := range eval.MustInclude {
			if !slices.Contains(ids, id) {
				t.Fatalf("must_include %s missing: %#v", id, ids)
			}
		}
		for _, id := range eval.MustNotInclude {
			if slices.Contains(ids, id) {
				t.Fatalf("must_not_include %s present: %#v", id, ids)
			}
		}
	}
}

func searchIDs(results []knowledge.SearchResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ID)
	}
	return ids
}

func contextIDs(results []knowledge.ContextDocument) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ID)
	}
	return ids
}

func affectedIDs(results []knowledge.AffectedDocument) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ID)
	}
	return ids
}

func writeMarkdown(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

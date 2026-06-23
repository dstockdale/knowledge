package knowledge_test

import (
	"os"
	"path/filepath"
	"testing"

	"knowledge/internal/knowledge"
)

func TestParseFileExtractsFrontmatterAndSections(t *testing.T) {
	root := corpusRoot(t)
	doc, err := knowledge.ParseFile(root, filepath.Join(root, "architecture/auth/authentication-identity.adr.md"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != "boop.adr.authentication-identity" {
		t.Fatalf("ID = %q", doc.ID)
	}
	if doc.Kind != "adr" || doc.Status != "accepted" {
		t.Fatalf("kind/status = %s/%s", doc.Kind, doc.Status)
	}
	if len(doc.Sections) != 2 {
		t.Fatalf("sections = %d", len(doc.Sections))
	}
	if doc.Sections[0].Heading != "Decision" || doc.Sections[0].Anchor != "decision" {
		t.Fatalf("first section = %#v", doc.Sections[0])
	}
}

func TestValidateFixtureCorpus(t *testing.T) {
	docs, err := knowledge.Load(corpusRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if issues := knowledge.Validate(docs, ""); len(issues) != 0 {
		t.Fatalf("unexpected validation issues: %#v", issues)
	}
}

func TestParseObsidianStyleFrontmatter(t *testing.T) {
	root := filepath.Join(repoRoot(t), "testdata", "obsidian")
	doc, err := knowledge.ParseFile(root, filepath.Join(root, "adrs/2026-06-10-user-auth-identity-graph.md"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != "adrs.2026-06-10-user-auth-identity-graph" {
		t.Fatalf("derived ID = %q", doc.ID)
	}
	if doc.Kind != "adr" {
		t.Fatalf("kind = %q", doc.Kind)
	}
	if doc.Title != "ADR: User Auth And Identity Graph Foundation" {
		t.Fatalf("title = %q", doc.Title)
	}
	if len(doc.Relations["source"]) != 1 || doc.Relations["source"][0] != "ideas.identity.users-people-and-entities" {
		t.Fatalf("source relation = %#v", doc.Relations["source"])
	}
}

func TestParseObsidianSpikeType(t *testing.T) {
	root := filepath.Join(repoRoot(t), "testdata", "obsidian")
	doc, err := knowledge.ParseFile(root, filepath.Join(root, "ideas/analytics/clickhouse-client-integration-spike.md"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != "spike" {
		t.Fatalf("kind = %q", doc.Kind)
	}
	if doc.ID != "ideas.analytics.clickhouse-client-integration-spike" {
		t.Fatalf("derived ID = %q", doc.ID)
	}
	for _, warning := range doc.Warnings {
		if warning.Code == "unknown_source_type" {
			t.Fatalf("type: spike should be known, got warning: %#v", warning)
		}
	}
}

func TestUnknownSourceTypeFallsBackToResearchUnlessStrict(t *testing.T) {
	root := filepath.Join(repoRoot(t), "testdata", "obsidian")
	doc, err := knowledge.ParseFile(root, filepath.Join(root, "ideas/unknown-source-shape.md"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != "research" {
		t.Fatalf("kind = %q", doc.Kind)
	}
	permissive := knowledge.ValidateWithOptions([]knowledge.Document{doc}, knowledge.ValidationOptions{})
	if issues := knowledge.FilterIssues(permissive, "error"); len(issues) != 0 {
		t.Fatalf("unexpected permissive errors: %#v", issues)
	}
	warnings := knowledge.FilterIssues(permissive, "warning")
	if !hasIssueCode(warnings, "unknown_source_type") {
		t.Fatalf("expected unknown_source_type warning, got %#v", warnings)
	}
	strict := knowledge.ValidateWithOptions([]knowledge.Document{doc}, knowledge.ValidationOptions{Strict: true})
	issues := knowledge.FilterIssues(strict, "error")
	if !hasIssueCode(issues, "unknown_source_type") {
		t.Fatalf("expected strict unknown_source_type error, got %#v", issues)
	}
}

func TestParseMarkdownWithoutFrontmatter(t *testing.T) {
	root := filepath.Join(repoRoot(t), "testdata", "obsidian")
	doc, err := knowledge.ParseFile(root, filepath.Join(root, "architecture/api-boundary.md"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != "architecture.api-boundary" {
		t.Fatalf("derived ID = %q", doc.ID)
	}
	if doc.Kind != "spec" || doc.Status != "current" || doc.Title != "API Boundary" {
		t.Fatalf("derived metadata = %s/%s/%q", doc.Kind, doc.Status, doc.Title)
	}
	if len(doc.Warnings) == 0 {
		t.Fatal("expected warnings for derived metadata")
	}
}

func TestValidatePermissiveAndStrictModes(t *testing.T) {
	docs, err := knowledge.Load(filepath.Join(repoRoot(t), "testdata", "obsidian"))
	if err != nil {
		t.Fatal(err)
	}
	permissive := knowledge.ValidateWithOptions(docs, knowledge.ValidationOptions{})
	if issues := knowledge.FilterIssues(permissive, "error"); len(issues) != 0 {
		t.Fatalf("unexpected permissive errors: %#v", issues)
	}
	if warnings := knowledge.FilterIssues(permissive, "warning"); len(warnings) == 0 {
		t.Fatal("expected permissive warnings")
	}
	strict := knowledge.ValidateWithOptions(docs, knowledge.ValidationOptions{Strict: true})
	if issues := knowledge.FilterIssues(strict, "error"); len(issues) == 0 {
		t.Fatal("expected strict errors for derived metadata")
	}
}

func TestValidateDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "a.md", "same.id", "First")
	writeDoc(t, root, "b.md", "same.id", "Second")
	docs, err := knowledge.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	issues := knowledge.Validate(docs, "")
	if len(issues) == 0 {
		t.Fatal("expected duplicate id issue")
	}
	found := false
	for _, issue := range issues {
		if issue.Code == "duplicate_id" {
			found = true
		}
	}
	if !found {
		t.Fatalf("duplicate_id issue missing: %#v", issues)
	}
}

func TestValidationSummaryLimits(t *testing.T) {
	issues := []knowledge.ValidationIssue{
		{Severity: "error", Code: "a"},
		{Severity: "error", Code: "b"},
		{Severity: "warning", Code: "c"},
		{Severity: "warning", Code: "d"},
	}
	summary := knowledge.SummarizeValidation(4, issues, 1, 1)
	if summary.IssueCount != 2 || summary.WarningCount != 2 {
		t.Fatalf("counts = %d/%d", summary.IssueCount, summary.WarningCount)
	}
	if len(summary.Issues) != 1 || len(summary.Warnings) != 1 {
		t.Fatalf("limited issues/warnings = %#v/%#v", summary.Issues, summary.Warnings)
	}
	if !summary.IssuesTruncated || !summary.WarningsTruncated {
		t.Fatalf("expected truncation flags: %#v", summary)
	}
}

func TestHistoricalStatusClassification(t *testing.T) {
	if !knowledge.IsHistorical("superseded") {
		t.Fatal("superseded should be historical")
	}
	if knowledge.IsHistorical("accepted") {
		t.Fatal("accepted should not be historical")
	}
}

func writeDoc(t *testing.T, root, name, id, title string) {
	t.Helper()
	content := `---
id: ` + id + `
kind: adr
status: accepted
title: ` + title + `
---

# Decision

Test content.
`
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func corpusRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "corpus")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func hasIssueCode(issues []knowledge.ValidationIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

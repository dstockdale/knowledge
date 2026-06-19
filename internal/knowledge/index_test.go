package knowledge_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
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

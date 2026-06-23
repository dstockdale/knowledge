package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"knowledge/internal/knowledge"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	ctx := context.Background()
	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "check":
		return runCheck(args[1:])
	case "index":
		return runIndex(ctx, args[1:])
	case "status":
		return runStatus(ctx, args[1:])
	case "scope-suggest":
		return runScopeSuggest(args[1:])
	case "search":
		return runSearch(ctx, args[1:])
	case "read":
		return runRead(args[1:])
	case "context":
		return runContext(ctx, args[1:])
	case "graph":
		return runGraph(args[1:])
	case "affected":
		return runAffected(args[1:])
	case "mcp":
		return runMCP(args[1:])
	default:
		return usage()
	}
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(absRoot, ".knowledge"), 0o755); err != nil {
		return err
	}
	return printJSON(map[string]any{"root": absRoot, "index_dir": filepath.Join(absRoot, ".knowledge")})
}

func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	codeRoot := fs.String("code-root", "", "optional code root for scoped path validation")
	strict := fs.Bool("strict", false, "require explicit knowledge frontmatter contract")
	issueLimit := fs.Int("issue-limit", -1, "maximum issues to print; defaults to all")
	warningLimit := fs.Int("warning-limit", -1, "maximum warnings to print; defaults to all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	docs, loadIssues, err := knowledge.LoadBestEffort(*root)
	if err != nil {
		return err
	}
	allIssues := append(loadIssues, knowledge.ValidateWithOptions(docs, knowledge.ValidationOptions{CodeRoot: *codeRoot, Strict: *strict})...)
	summary := knowledge.SummarizeValidation(len(docs), allIssues, *issueLimit, *warningLimit)
	if err := printJSON(summary); err != nil {
		return err
	}
	if summary.IssueCount > 0 {
		return fmt.Errorf("validation failed with %d issue(s)", summary.IssueCount)
	}
	return nil
}

func runIndex(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	dbPath := fs.String("db", "", "index database path")
	strict := fs.Bool("strict", false, "require explicit knowledge frontmatter contract before indexing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	db := knowledge.ResolveDBPath(*root, *dbPath)
	if *strict {
		docs, err := knowledge.Load(*root)
		if err != nil {
			return err
		}
		issues := knowledge.FilterIssues(knowledge.ValidateWithOptions(docs, knowledge.ValidationOptions{Strict: true}), "error")
		if len(issues) > 0 {
			return fmt.Errorf("strict validation failed with %d issue(s): %s", len(issues), issues[0].Message)
		}
	}
	docs, err := knowledge.RebuildIndex(ctx, *root, db)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"db_path": db, "documents": len(docs)})
}

func runSearch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	dbPath := fs.String("db", "", "index database path")
	includeHistorical := fs.Bool("include-historical", false, "include historical lifecycle statuses")
	limit := fs.Int("limit", 20, "maximum results")
	var kinds, statuses, scopes, paths multiFlag
	fs.Var(&kinds, "kind", "kind filter, repeatable")
	fs.Var(&statuses, "status", "status filter, repeatable")
	fs.Var(&scopes, "scope", "scope filter, repeatable")
	fs.Var(&paths, "path", "path filter, repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := fs.Arg(0)
	if query == "" {
		return fmt.Errorf("search requires a query")
	}
	response, err := knowledge.SearchWithValidation(ctx, *root, knowledge.ResolveDBPath(*root, *dbPath), knowledge.SearchOptions{
		Query:             query,
		Kinds:             kinds,
		Statuses:          statuses,
		Scopes:            scopes,
		Paths:             paths,
		IncludeHistorical: *includeHistorical,
		Limit:             *limit,
	})
	if err != nil {
		return err
	}
	return printJSON(response)
}

func runStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	dbPath := fs.String("db", "", "index database path")
	issueLimit := fs.Int("issue-limit", 20, "maximum issues to print")
	warningLimit := fs.Int("warning-limit", 20, "maximum warnings to print")
	if err := fs.Parse(args); err != nil {
		return err
	}
	result, err := knowledge.Status(ctx, *root, knowledge.ResolveDBPath(*root, *dbPath), *issueLimit, *warningLimit)
	if err != nil {
		return err
	}
	return printJSON(result)
}

func runScopeSuggest(args []string) error {
	fs := flag.NewFlagSet("scope-suggest", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	codeRoot := fs.String("code-root", "", "optional code root for scoped path validation")
	limit := fs.Int("limit", 20, "maximum suggestions to print")
	if err := fs.Parse(args); err != nil {
		return err
	}
	result, err := knowledge.SuggestScopes(*root, knowledge.ScopeSuggestionOptions{CodeRoot: *codeRoot, Limit: *limit})
	if err != nil {
		return err
	}
	return printJSON(result)
}

func runRead(args []string) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	heading := fs.String("section", "", "section heading or anchor")
	maxTokens := fs.Int("max-tokens", 0, "maximum estimated tokens")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return fmt.Errorf("read requires a document id")
	}
	result, err := knowledge.ReadDocument(*root, id, *heading, *maxTokens)
	if err != nil {
		return err
	}
	return printJSON(result)
}

func runContext(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("context", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	dbPath := fs.String("db", "", "index database path")
	task := fs.String("task", "", "task description")
	tokenBudget := fs.Int("token-budget", 6000, "estimated token budget")
	includeHistorical := fs.Bool("include-historical", false, "include historical lifecycle statuses")
	var paths, symbols multiFlag
	fs.Var(&paths, "path", "affected path, repeatable")
	fs.Var(&symbols, "symbol", "affected symbol, repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *task == "" {
		return fmt.Errorf("context requires --task")
	}
	manifest, err := knowledge.ContextForTask(ctx, *root, knowledge.ResolveDBPath(*root, *dbPath), knowledge.ContextRequest{
		Task:              *task,
		Paths:             paths,
		Symbols:           symbols,
		TokenBudget:       *tokenBudget,
		IncludeHistorical: *includeHistorical,
	})
	if err != nil {
		return err
	}
	return printJSON(manifest)
}

func runGraph(args []string) error {
	fs := flag.NewFlagSet("graph", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	depth := fs.Int("depth", 1, "graph traversal depth")
	var relations multiFlag
	fs.Var(&relations, "relation", "relation filter, repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return fmt.Errorf("graph requires a document id")
	}
	result, err := knowledge.Neighbors(*root, id, relations, *depth)
	if err != nil {
		return err
	}
	return printJSON(result)
}

func runAffected(args []string) error {
	fs := flag.NewFlagSet("affected", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	var paths multiFlag
	fs.Var(&paths, "path", "changed path, repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("affected requires at least one --path")
	}
	results, err := knowledge.AffectedDocuments(*root, paths)
	if err != nil {
		return err
	}
	return printJSON(results)
}

func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	root := fs.String("root", ".", "knowledge root")
	dbPath := fs.String("db", "", "index database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return knowledge.RunMCP(context.Background(), *root, knowledge.ResolveDBPath(*root, *dbPath))
}

func usage() error {
	return fmt.Errorf("usage: knowledge <init|check|index|status|scope-suggest|search|read|context|graph|affected|mcp> [options]")
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

type multiFlag []string

func (m *multiFlag) String() string {
	return fmt.Sprint([]string(*m))
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

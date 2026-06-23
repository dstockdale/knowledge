package knowledge

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type contextForTaskInput struct {
	Task              string   `json:"task" jsonschema:"task description to retrieve context for"`
	Paths             []string `json:"paths,omitempty" jsonschema:"affected repository paths"`
	Symbols           []string `json:"symbols,omitempty" jsonschema:"affected code symbols"`
	TokenBudget       int      `json:"token_budget,omitempty" jsonschema:"estimated token budget"`
	IncludeHistorical bool     `json:"include_historical,omitempty" jsonschema:"include historical lifecycle statuses"`
}

type searchInput struct {
	Query             string   `json:"query" jsonschema:"search query"`
	Kinds             []string `json:"kinds,omitempty" jsonschema:"document kinds to include"`
	Statuses          []string `json:"statuses,omitempty" jsonschema:"document statuses to include"`
	Scopes            []string `json:"scopes,omitempty" jsonschema:"scope domains or paths to include"`
	Paths             []string `json:"paths,omitempty" jsonschema:"affected paths to match"`
	IncludeHistorical bool     `json:"include_historical,omitempty" jsonschema:"include historical lifecycle statuses"`
	Limit             int      `json:"limit,omitempty" jsonschema:"maximum result count"`
}

type searchOutput struct {
	Results    []SearchResult    `json:"results"`
	Validation ValidationSummary `json:"validation"`
}

type readInput struct {
	ID        string `json:"id" jsonschema:"stable document id"`
	Heading   string `json:"heading,omitempty" jsonschema:"optional section heading or anchor"`
	MaxTokens int    `json:"max_tokens,omitempty" jsonschema:"maximum estimated tokens"`
}

type neighborsInput struct {
	ID        string   `json:"id" jsonschema:"stable document id"`
	Relations []string `json:"relations,omitempty" jsonschema:"relation names to traverse"`
	Depth     int      `json:"depth,omitempty" jsonschema:"traversal depth, capped at 3"`
}

type validateInput struct {
	CodeRoot     string `json:"code_root,omitempty" jsonschema:"optional code root for scoped path validation"`
	Strict       bool   `json:"strict,omitempty" jsonschema:"require explicit knowledge frontmatter contract"`
	IssueLimit   int    `json:"issue_limit,omitempty" jsonschema:"maximum issue examples to return, default 20"`
	WarningLimit int    `json:"warning_limit,omitempty" jsonschema:"maximum warning examples to return, default 20"`
}

type validateOutput struct {
	Documents         int               `json:"documents"`
	IssueCount        int               `json:"issue_count"`
	WarningCount      int               `json:"warning_count"`
	Issues            []ValidationIssue `json:"issues"`
	Warnings          []ValidationIssue `json:"warnings"`
	IssuesTruncated   bool              `json:"issues_truncated"`
	WarningsTruncated bool              `json:"warnings_truncated"`
}

type affectedDocumentsInput struct {
	Paths []string `json:"paths" jsonschema:"changed repository paths"`
}

type affectedDocumentsOutput struct {
	Documents []AffectedDocument `json:"documents"`
}

type statusInput struct {
	IssueLimit   int `json:"issue_limit,omitempty" jsonschema:"maximum issue examples to return, default 20"`
	WarningLimit int `json:"warning_limit,omitempty" jsonschema:"maximum warning examples to return, default 20"`
}

type scopeSuggestionsInput struct {
	CodeRoot string `json:"code_root,omitempty" jsonschema:"optional code root for scoped path validation"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum suggestions to return, default 20"`
}

func RunMCP(ctx context.Context, root, dbPath string) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "knowledge", Version: "v0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "context_for_task",
		Description: "Return a token-budgeted context manifest for a coding task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input contextForTaskInput) (*mcp.CallToolResult, ContextManifest, error) {
		output, err := ContextForTask(ctx, root, dbPath, ContextRequest{
			Task:              input.Task,
			Paths:             input.Paths,
			Symbols:           input.Symbols,
			TokenBudget:       input.TokenBudget,
			IncludeHistorical: input.IncludeHistorical,
		})
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Search indexed knowledge documents with lifecycle-aware filters.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input searchInput) (*mcp.CallToolResult, searchOutput, error) {
		response, err := SearchWithValidation(ctx, root, dbPath, SearchOptions{
			Query:             input.Query,
			Kinds:             input.Kinds,
			Statuses:          input.Statuses,
			Scopes:            input.Scopes,
			Paths:             input.Paths,
			IncludeHistorical: input.IncludeHistorical,
			Limit:             input.Limit,
		})
		return nil, searchOutput{Results: response.Results, Validation: response.Validation}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read",
		Description: "Read a document or one specific section by stable id.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input readInput) (*mcp.CallToolResult, ReadResult, error) {
		output, err := ReadDocument(root, input.ID, input.Heading, input.MaxTokens)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "neighbors",
		Description: "Traverse explicit document relationships.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input neighborsInput) (*mcp.CallToolResult, GraphResult, error) {
		output, err := Neighbors(root, input.ID, input.Relations, input.Depth)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "validate",
		Description: "Validate frontmatter, stable ids, relations, headings, and optional scoped paths.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input validateInput) (*mcp.CallToolResult, validateOutput, error) {
		docs, loadIssues, err := LoadBestEffort(root)
		if err != nil {
			return nil, validateOutput{}, err
		}
		allIssues := append(loadIssues, ValidateWithOptions(docs, ValidationOptions{CodeRoot: input.CodeRoot, Strict: input.Strict})...)
		summary := SummarizeValidation(len(docs), allIssues, defaultValidationLimit(input.IssueLimit), defaultValidationLimit(input.WarningLimit))
		return nil, validateOutput{
			Documents:         summary.Documents,
			IssueCount:        summary.IssueCount,
			WarningCount:      summary.WarningCount,
			Issues:            summary.Issues,
			Warnings:          summary.Warnings,
			IssuesTruncated:   summary.IssuesTruncated,
			WarningsTruncated: summary.WarningsTruncated,
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "status",
		Description: "Return knowledge root, index freshness, usability, and compact validation status.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input statusInput) (*mcp.CallToolResult, StatusResult, error) {
		output, err := Status(ctx, root, dbPath, defaultValidationLimit(input.IssueLimit), defaultValidationLimit(input.WarningLimit))
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "scope_suggestions",
		Description: "Suggest repo-relative scope.paths for unscoped knowledge documents.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input scopeSuggestionsInput) (*mcp.CallToolResult, ScopeSuggestionReport, error) {
		output, err := SuggestScopes(root, ScopeSuggestionOptions{CodeRoot: input.CodeRoot, Limit: defaultSuggestionLimit(input.Limit)})
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "affected_documents",
		Description: "Find knowledge documents scoped to changed repository paths.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input affectedDocumentsInput) (*mcp.CallToolResult, affectedDocumentsOutput, error) {
		docs, err := AffectedDocuments(root, input.Paths)
		return nil, affectedDocumentsOutput{Documents: docs}, err
	})

	return server.Run(ctx, &mcp.StdioTransport{})
}

func defaultValidationLimit(value int) int {
	if value <= 0 {
		return 20
	}
	return value
}

func defaultSuggestionLimit(value int) int {
	if value <= 0 {
		return 20
	}
	return value
}

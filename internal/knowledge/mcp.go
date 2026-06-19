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
	Results []SearchResult `json:"results"`
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
	CodeRoot string `json:"code_root,omitempty" jsonschema:"optional code root for scoped path validation"`
	Strict   bool   `json:"strict,omitempty" jsonschema:"require explicit knowledge frontmatter contract"`
}

type validateOutput struct {
	Documents int               `json:"documents"`
	Issues    []ValidationIssue `json:"issues"`
	Warnings  []ValidationIssue `json:"warnings"`
}

type affectedDocumentsInput struct {
	Paths []string `json:"paths" jsonschema:"changed repository paths"`
}

type affectedDocumentsOutput struct {
	Documents []AffectedDocument `json:"documents"`
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
		results, err := Search(ctx, root, dbPath, SearchOptions{
			Query:             input.Query,
			Kinds:             input.Kinds,
			Statuses:          input.Statuses,
			Scopes:            input.Scopes,
			Paths:             input.Paths,
			IncludeHistorical: input.IncludeHistorical,
			Limit:             input.Limit,
		})
		return nil, searchOutput{Results: results}, err
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
		docs, err := Load(root)
		if err != nil {
			return nil, validateOutput{}, err
		}
		allIssues := ValidateWithOptions(docs, ValidationOptions{CodeRoot: input.CodeRoot, Strict: input.Strict})
		return nil, validateOutput{
			Documents: len(docs),
			Issues:    FilterIssues(allIssues, "error"),
			Warnings:  FilterIssues(allIssues, "warning"),
		}, nil
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

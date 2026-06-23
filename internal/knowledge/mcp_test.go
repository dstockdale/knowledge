package knowledge_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/knowledge", "mcp", "--root", corpusRoot(t), "--db", filepath.Join(t.TempDir(), "index.sqlite"))
	cmd.Dir = repoRoot(t)

	client := mcp.NewClient(&mcp.Implementation{Name: "knowledge-test", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{name: "status", args: map[string]any{}},
		{name: "validate", args: map[string]any{}},
		{name: "scope_suggestions", args: map[string]any{"limit": 5}},
		{name: "search", args: map[string]any{"query": "registration"}},
		{name: "read", args: map[string]any{"id": "boop.adr.authentication-identity", "heading": "Decision"}},
		{name: "neighbors", args: map[string]any{"id": "boop.plan.passkeys", "depth": 1}},
		{name: "context_for_task", args: map[string]any{"task": "add passkeys", "paths": []string{"lib/boop/accounts"}, "token_budget": 2000}},
		{name: "affected_documents", args: map[string]any{"paths": []string{"lib/boop/accounts/authentication.ex"}}},
	} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if err != nil {
			t.Fatalf("%s call failed: %v", call.name, err)
		}
		if res.IsError {
			t.Fatalf("%s returned tool error: %#v", call.name, res.Content)
		}
	}
}

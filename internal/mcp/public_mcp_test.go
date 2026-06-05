package mcp

import (
	"os"
	"strings"
	"testing"

	"github.com/kiritosuki/gocoder/internal/types"
)

// TestPublicMemoryMCP is an opt-in smoke test for a real public npm MCP server.
// Run with:
//
//	GOCODER_PUBLIC_MCP_TEST=1 go test -run TestPublicMemoryMCP -v ./internal/mcp
func TestPublicMemoryMCP(t *testing.T) {
	if os.Getenv("GOCODER_PUBLIC_MCP_TEST") != "1" {
		t.Skip("set GOCODER_PUBLIC_MCP_TEST=1 to run public MCP integration test")
	}

	mgr := NewManager()
	err := mgr.Connect("memory", types.MCPConfig{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-memory"},
	})
	if err != nil && strings.Contains(err.Error(), "could not determine executable") {
		err = mgr.Connect("memory", types.MCPConfig{
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-memory@latest"},
		})
	}
	if err != nil {
		t.Fatalf("connect public memory MCP: %v", err)
	}
	defer mgr.CloseAll()

	if tools := mgr.GetAllTools(); len(tools) == 0 {
		t.Fatal("public memory MCP returned no tools")
	}
}

func TestPublicFilesystemMCP(t *testing.T) {
	if os.Getenv("GOCODER_PUBLIC_MCP_TEST") != "1" {
		t.Skip("set GOCODER_PUBLIC_MCP_TEST=1 to run public MCP integration test")
	}

	mgr := NewManager()
	if err := mgr.Connect("filesystem", types.MCPConfig{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", os.TempDir()},
	}); err != nil {
		t.Fatalf("connect public filesystem MCP: %v", err)
	}
	defer mgr.CloseAll()

	tools := mgr.GetAllTools()
	if len(tools) == 0 {
		t.Fatal("public filesystem MCP returned no tools")
	}

	foundRead := false
	for _, tool := range tools {
		if strings.Contains(tool.Name, "read") {
			foundRead = true
			break
		}
	}
	if !foundRead {
		t.Fatalf("filesystem MCP tools did not include a read-like tool: %+v", tools)
	}
}

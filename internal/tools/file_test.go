package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kiritosuki/gocoder/internal/types"
)

func TestFileToolsRejectWorkspaceEscape(t *testing.T) {
	workDir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	RegisterBuiltins(reg, BuiltinConfig{WorkDir: workDir, ToolResultDir: workDir})

	_, err := reg.Execute("read_file", types.ToolContext{}, map[string]any{
		"path": filepath.Join(outside, "secret.txt"),
	})
	if err == nil || !strings.Contains(err.Error(), "路径越界") {
		t.Fatalf("expected workspace escape error, got %v", err)
	}

	_, err = reg.Execute("write_file", types.ToolContext{}, map[string]any{
		"path":    "../escape.txt",
		"content": "nope",
	})
	if err == nil || !strings.Contains(err.Error(), "路径越界") {
		t.Fatalf("expected write workspace escape error, got %v", err)
	}
}

func TestEditFileDiffAndUniqueness(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	RegisterBuiltins(reg, BuiltinConfig{WorkDir: workDir, ToolResultDir: workDir})

	out, err := reg.Execute("edit_file", types.ToolContext{}, map[string]any{
		"path":       "main.go",
		"old_string": "func main() {}",
		"new_string": "func main() { println(\"ok\") }",
	})
	if err != nil {
		t.Fatalf("edit_file failed: %v", err)
	}
	if !strings.Contains(out, "--- a/main.go") || !strings.Contains(out, "文件 main.go 已修改") {
		t.Fatalf("expected diff and success message, got %s", out)
	}
}

func TestRunCommandRejectsWorkingDirEscape(t *testing.T) {
	workDir := t.TempDir()
	outside := t.TempDir()

	reg := NewRegistry()
	RegisterBuiltins(reg, BuiltinConfig{WorkDir: workDir, ToolResultDir: workDir})

	_, err := reg.Execute("run_command", types.ToolContext{}, map[string]any{
		"command":     "pwd",
		"working_dir": outside,
	})
	if err == nil || !strings.Contains(err.Error(), "路径越界") {
		t.Fatalf("expected shell working_dir escape error, got %v", err)
	}
}

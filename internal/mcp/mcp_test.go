package mcp

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kiritosuki/gocoder/internal/types"
)

// 用 test_mcp_server.py 做端到端测试
func TestStdioConnection(t *testing.T) {
	// 找到项目根目录下的 test_mcp_server.py
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "test_mcp_server.py")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("未找到 test_mcp_server.py，跳过集成测试")
		}
		dir = parent
	}

	cfg := types.MCPConfig{
		Command: "python3",
		Args:    []string{filepath.Join(dir, "test_mcp_server.py")},
	}

	client, err := NewStdioClient("test", cfg)
	if err != nil {
		t.Fatalf("创建 stdio client 失败: %v", err)
	}
	defer client.Close()

	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	tools, err := client.DiscoverTools()
	if err != nil {
		t.Fatalf("DiscoverTools 失败: %v", err)
	}

	if len(tools) < 2 {
		t.Fatalf("期望至少 2 个工具，实际 %d", len(tools))
	}

	names := map[string]bool{}
	for _, t := range tools {
		names[t.Name] = true
	}
	for _, want := range []string{"echo", "get_time"} {
		if !names[want] {
			t.Errorf("缺少工具: %s", want)
		}
	}

	// 测试工具调用
	output, err := client.CallTool("echo", map[string]any{"message": "hello_test"})
	if err != nil {
		t.Fatalf("CallTool echo 失败: %v", err)
	}
	if !strings.Contains(output, "ECHO: hello_test") {
		t.Errorf("echo 返回异常: %s", output)
	}

	t.Logf("MCP 集成测试通过: %d 个工具, echo 正常", len(tools))
}

func TestManagerConnect(t *testing.T) {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "test_mcp_server.py")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("未找到 test_mcp_server.py")
		}
		dir = parent
	}

	mgr := NewManager()

	cfg := types.MCPConfig{
		Command: "python3",
		Args:    []string{filepath.Join(dir, "test_mcp_server.py")},
	}

	if err := mgr.Connect("test-srv", cfg); err != nil {
		t.Fatalf("Manager Connect 失败: %v", err)
	}
	defer mgr.CloseAll()

	servers := mgr.ConnectedServers()
	if len(servers) != 1 || servers[0] != "test-srv" {
		t.Fatalf("ConnectedServers 异常: %v", servers)
	}

	// GetToolServer
	server := mgr.GetToolServer("echo")
	if server != "test-srv" {
		t.Errorf("GetToolServer(echo) = %q, want test-srv", server)
	}

	// CallTool
	output, err := mgr.CallTool("test-srv", "echo", map[string]any{"message": "mgr_test"})
	if err != nil {
		t.Fatalf("Manager CallTool 失败: %v", err)
	}
	if !strings.Contains(output, "ECHO: mgr_test") {
		t.Errorf("CallTool 返回异常: %s", output)
	}

	t.Log("Manager 集成测试通过")
}

func TestManagerToolNameCollision(t *testing.T) {
	dir := findProjectRoot(t)
	cfg := types.MCPConfig{
		Command: "python3",
		Args:    []string{filepath.Join(dir, "test_mcp_server.py")},
	}

	mgr := NewManager()
	if err := mgr.Connect("alpha", cfg); err != nil {
		t.Fatalf("connect alpha: %v", err)
	}
	if err := mgr.Connect("beta", cfg); err != nil {
		t.Fatalf("connect beta: %v", err)
	}
	defer mgr.CloseAll()

	names := map[string]bool{}
	for _, tool := range mgr.GetAllTools() {
		names[tool.Name] = true
	}
	if !names["echo"] {
		t.Fatalf("missing first echo binding: %v", names)
	}
	if !names["beta__echo"] {
		t.Fatalf("missing collision-safe echo binding: %v", names)
	}

	out, err := mgr.CallRegisteredTool("beta__echo", map[string]any{"message": "collision"})
	if err != nil {
		t.Fatalf("call registered collision tool: %v", err)
	}
	if !strings.Contains(out, "ECHO: collision") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestHTTPConnectionWithRootFallback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("本环境无法监听本地端口，跳过 HTTP MCP 测试: %v", err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		var req types.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		result := map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "http-test", "version": "1.0.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "http_echo",
				"description": "HTTP echo",
				"inputSchema": map[string]any{"type": "object"},
			}}}
		case "tools/call":
			var params map[string]any
			_ = json.Unmarshal(req.Params, &params)
			args, _ := params["arguments"].(map[string]any)
			result = map[string]any{"content": []map[string]any{{
				"type": "text",
				"text": "HTTP: " + stringify(args["message"]),
			}}}
		}
		_ = json.NewEncoder(w).Encode(types.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshal(result),
		})
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	client, err := NewHTTPClient("http", types.MCPConfig{URL: server.URL})
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	if err := client.Initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	tools, err := client.DiscoverTools()
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "http_echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	out, err := client.CallTool("http_echo", map[string]any{"message": "ok"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out != "HTTP: ok" {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestStdioSkipsNotificationBeforeResponse(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "notify_server.py")
	if err := os.WriteFile(script, []byte(`#!/usr/bin/env python3
import json, sys
for line in sys.stdin:
    req=json.loads(line)
    method=req.get("method")
    rid=req.get("id")
    if method == "initialize":
        print(json.dumps({"jsonrpc":"2.0","method":"notifications/progress","params":{"message":"boot"}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","id":rid,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"notify","version":"1"}}}), flush=True)
    elif method == "notifications/initialized":
        pass
    elif method == "tools/list":
        if req.get("params") is None:
            continue
        print(json.dumps({"jsonrpc":"2.0","method":"notifications/progress","params":{"message":"listing"}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","id":rid,"result":{"tools":[{"name":"notify_tool","description":"test","inputSchema":{"type":"object"}}]}}), flush=True)
`), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	client, err := NewStdioClient("notify", types.MCPConfig{Command: "python3", Args: []string{script}})
	if err != nil {
		t.Fatalf("new stdio client: %v", err)
	}
	defer client.Close()
	if err := client.Initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	tools, err := client.DiscoverTools()
	if err != nil {
		t.Fatalf("discover tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "notify_tool" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "test_mcp_server.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("未找到 test_mcp_server.py")
		}
		dir = parent
	}
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

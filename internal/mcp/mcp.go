// Package mcp implements a small MCP (Model Context Protocol) client.
// It supports stdio servers and JSON-RPC-over-HTTP servers, then exposes
// discovered remote tools through a manager-level name index.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kiritosuki/gocoder/internal/types"
)

const (
	protocolVersion = "2024-11-05"
	defaultTimeout  = 30 * time.Second
)

var invalidToolNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// Client manages one MCP server connection.
type Client struct {
	config types.MCPConfig
	name   string
	mode   string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	httpClient *http.Client
	httpURLs   []string

	requestID atomic.Int64
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int]chan *types.JSONRPCResponse
	readErr   chan error
	done      chan struct{}

	tools      []types.MCPToolDefinition
	serverInfo map[string]any
}

// NewStdioClient starts a stdio MCP server process.
func NewStdioClient(name string, cfg types.MCPConfig) (*Client, error) {
	if cfg.Command == "" {
		return nil, errors.New("stdio MCP server 缺少 command")
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = cmd.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdin 管道失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stderr 管道失败: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 MCP server 失败: %w", err)
	}

	c := &Client{
		config:  cfg,
		name:    name,
		mode:    "stdio",
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		pending: make(map[int]chan *types.JSONRPCResponse),
		readErr: make(chan error, 1),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	go drain(stderr)
	return c, nil
}

// NewHTTPClient creates an HTTP MCP client. If cfg.URL does not already end
// with /mcp, both /mcp and the raw URL are tried so locally hosted servers with
// either routing style work.
func NewHTTPClient(name string, cfg types.MCPConfig) (*Client, error) {
	if cfg.URL == "" {
		return nil, errors.New("HTTP MCP server 缺少 url")
	}
	base := strings.TrimRight(cfg.URL, "/")
	urls := []string{base}
	if !strings.HasSuffix(base, "/mcp") {
		urls = []string{base + "/mcp", base}
	}
	return &Client{
		config:     cfg,
		name:       name,
		mode:       "http",
		httpClient: &http.Client{Timeout: defaultTimeout},
		httpURLs:   urls,
	}, nil
}

// Initialize performs MCP initialize and sends the initialized notification.
func (c *Client) Initialize() error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "GoCoder",
			"version": "1.0.0",
		},
	}

	resp, err := c.sendRequest("initialize", params)
	if err != nil {
		return fmt.Errorf("MCP initialize 失败: %w", err)
	}

	var result struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ServerInfo      map[string]any `json:"serverInfo"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("解析 initialize 响应失败: %w", err)
	}
	c.serverInfo = result.ServerInfo

	if err := c.sendNotification("notifications/initialized", nil); err != nil {
		return fmt.Errorf("发送 initialized 通知失败: %w", err)
	}
	return nil
}

// DiscoverTools fetches the server's tool list.
func (c *Client) DiscoverTools() ([]types.MCPToolDefinition, error) {
	resp, err := c.sendRequest("tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("获取工具列表失败: %w", err)
	}

	var result struct {
		Tools []types.MCPToolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("解析工具列表失败: %w", err)
	}
	c.tools = normalizeTools(result.Tools)
	return c.tools, nil
}

// CallTool calls a remote MCP tool and returns text content suitable for an LLM
// tool result. Non-text content is summarized instead of silently discarded.
func (c *Client) CallTool(name string, arguments map[string]any) (string, error) {
	resp, err := c.sendRequest("tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	})
	if err != nil {
		return "", fmt.Errorf("调用 MCP 工具 %s 失败: %w", name, err)
	}

	var result struct {
		Content []map[string]any `json:"content"`
		IsError bool             `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		var simple struct {
			Content any `json:"content"`
		}
		if err2 := json.Unmarshal(resp.Result, &simple); err2 != nil {
			return "", fmt.Errorf("解析工具结果失败: %w", err)
		}
		return stringify(simple.Content), nil
	}

	parts := make([]string, 0, len(result.Content))
	for _, item := range result.Content {
		typ, _ := item["type"].(string)
		switch typ {
		case "text":
			if text, ok := item["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		case "image":
			parts = append(parts, "[MCP image content omitted]")
		case "resource":
			parts = append(parts, stringify(item))
		default:
			if len(item) > 0 {
				parts = append(parts, stringify(item))
			}
		}
	}
	out := strings.Join(parts, "\n")
	if result.IsError {
		return out, fmt.Errorf("MCP 工具 %s 返回错误: %s", name, out)
	}
	return out, nil
}

func (c *Client) Tools() []types.MCPToolDefinition {
	tools := make([]types.MCPToolDefinition, len(c.tools))
	copy(tools, c.tools)
	return tools
}

func (c *Client) Name() string {
	return c.name
}

func (c *Client) Close() error {
	if c.mode != "stdio" {
		return nil
	}
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	return nil
}

func (c *Client) sendRequest(method string, params any) (*types.JSONRPCResponse, error) {
	id := int(c.requestID.Add(1))
	paramsJSON, err := encodeParams(params)
	if err != nil {
		return nil, err
	}
	req := types.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}
	if c.mode == "http" {
		return c.httpCall(req)
	}
	return c.stdioCall(req)
}

func (c *Client) stdioCall(req types.JSONRPCRequest) (*types.JSONRPCResponse, error) {
	ch := make(chan *types.JSONRPCResponse, 1)
	c.pendingMu.Lock()
	c.pending[req.ID] = ch
	c.pendingMu.Unlock()
	defer c.removePending(req.ID)

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	c.writeMu.Lock()
	_, err = c.stdin.Write(data)
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("写入请求失败: %w", err)
	}

	timer := time.NewTimer(defaultTimeout)
	defer timer.Stop()

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP 错误 [%d]: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case err := <-c.readErr:
		return nil, err
	case <-timer.C:
		return nil, fmt.Errorf("请求 %s 超时", req.Method)
	case <-c.done:
		return nil, errors.New("MCP 连接已关闭")
	}
}

func (c *Client) readLoop() {
	reader := bufio.NewReader(c.stdout)
	for {
		select {
		case <-c.done:
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			select {
			case c.readErr <- fmt.Errorf("读取响应失败: %w", err):
			default:
			}
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var resp types.JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.ID == 0 {
			continue
		}

		c.pendingMu.Lock()
		ch := c.pending[resp.ID]
		c.pendingMu.Unlock()
		if ch != nil {
			ch <- &resp
		}
	}
}

func (c *Client) removePending(id int) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *Client) httpCall(req types.JSONRPCRequest) (*types.JSONRPCResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, url := range c.httpURLs {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			cancel()
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}

		var jsonResp types.JSONRPCResponse
		if err := json.Unmarshal(body, &jsonResp); err != nil {
			return nil, fmt.Errorf("解析 HTTP 响应失败: %w", err)
		}
		if jsonResp.Error != nil {
			return nil, fmt.Errorf("MCP 错误 [%d]: %s", jsonResp.Error.Code, jsonResp.Error.Message)
		}
		return &jsonResp, nil
	}
	return nil, lastErr
}

func (c *Client) sendNotification(method string, params any) error {
	if c.mode == "http" {
		return nil
	}
	paramsJSON, err := encodeParams(params)
	if err != nil {
		return err
	}
	data, err := json.Marshal(types.JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
	})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(data)
	return err
}

func drain(r io.Reader) {
	_, _ = io.Copy(io.Discard, r)
}

func encodeParams(params any) (json.RawMessage, error) {
	if params == nil {
		return json.RawMessage(`{}`), nil
	}
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ToolBinding records how a registered function name maps back to a remote MCP
// server and original remote tool name.
type ToolBinding struct {
	ServerName     string
	OriginalName   string
	RegisteredName string
}

// Manager manages multiple MCP clients and stable tool-name routing.
type Manager struct {
	clients  map[string]*Client
	bindings map[string]ToolBinding
	mu       sync.RWMutex
}

func NewManager() *Manager {
	return &Manager{
		clients:  make(map[string]*Client),
		bindings: make(map[string]ToolBinding),
	}
}

func (m *Manager) Connect(name string, cfg types.MCPConfig) error {
	var client *Client
	var err error
	if cfg.URL != "" {
		client, err = NewHTTPClient(name, cfg)
	} else {
		client, err = NewStdioClient(name, cfg)
	}
	if err != nil {
		return fmt.Errorf("连接 MCP server %s 失败: %w", name, err)
	}
	if err := client.Initialize(); err != nil {
		_ = client.Close()
		return fmt.Errorf("初始化 MCP server %s 失败: %w", name, err)
	}
	if _, err := client.DiscoverTools(); err != nil {
		_ = client.Close()
		return fmt.Errorf("发现 MCP 工具失败 %s: %w", name, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if old := m.clients[name]; old != nil {
		_ = old.Close()
	}
	for registered, binding := range m.bindings {
		if binding.ServerName == name {
			delete(m.bindings, registered)
		}
	}
	m.clients[name] = client
	for _, t := range client.Tools() {
		registered := m.uniqueToolNameLocked(name, t.Name)
		m.bindings[registered] = ToolBinding{
			ServerName:     name,
			OriginalName:   t.Name,
			RegisteredName: registered,
		}
	}
	return nil
}

func (m *Manager) GetAllTools() []types.MCPToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.bindings))
	for name := range m.bindings {
		names = append(names, name)
	}
	sort.Strings(names)

	all := make([]types.MCPToolDefinition, 0, len(names))
	for _, registered := range names {
		b := m.bindings[registered]
		client := m.clients[b.ServerName]
		if client == nil {
			continue
		}
		for _, t := range client.Tools() {
			if t.Name == b.OriginalName {
				t.Name = registered
				all = append(all, t)
				break
			}
		}
	}
	return all
}

func (m *Manager) CallTool(serverName, toolName string, args map[string]any) (string, error) {
	m.mu.RLock()
	if binding, ok := m.bindings[toolName]; ok {
		serverName = binding.ServerName
		toolName = binding.OriginalName
	}
	client := m.clients[serverName]
	m.mu.RUnlock()

	if client == nil {
		return "", fmt.Errorf("MCP server %s 未连接", serverName)
	}
	return client.CallTool(toolName, args)
}

func (m *Manager) CallRegisteredTool(toolName string, args map[string]any) (string, error) {
	return m.CallTool("", toolName, args)
}

func (m *Manager) GetToolServer(toolName string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if binding, ok := m.bindings[toolName]; ok {
		return binding.ServerName
	}
	for name, c := range m.clients {
		for _, t := range c.Tools() {
			if t.Name == toolName {
				return name
			}
		}
	}
	return ""
}

func (m *Manager) Binding(toolName string) (ToolBinding, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.bindings[toolName]
	return b, ok
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.clients {
		_ = c.Close()
	}
	m.clients = make(map[string]*Client)
	m.bindings = make(map[string]ToolBinding)
}

func (m *Manager) ConnectedServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (m *Manager) uniqueToolNameLocked(serverName, toolName string) string {
	base := sanitizeToolName(toolName)
	if _, exists := m.bindings[base]; !exists {
		return base
	}
	prefixed := sanitizeToolName(serverName + "__" + toolName)
	if _, exists := m.bindings[prefixed]; !exists {
		return prefixed
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", prefixed, i)
		if _, exists := m.bindings[candidate]; !exists {
			return candidate
		}
	}
}

func normalizeTools(tools []types.MCPToolDefinition) []types.MCPToolDefinition {
	out := make([]types.MCPToolDefinition, 0, len(tools))
	for _, t := range tools {
		t.Name = sanitizeToolName(t.Name)
		if t.Name == "" {
			continue
		}
		if t.Description == "" {
			t.Description = "MCP tool " + t.Name
		}
		if t.InputSchema == nil {
			t.InputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, t)
	}
	return out
}

func sanitizeToolName(name string) string {
	name = strings.TrimSpace(name)
	name = invalidToolNameChars.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		data, err := json.MarshalIndent(x, "", "  ")
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(data)
	}
}

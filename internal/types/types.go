// Package types 定义 GoCoder 全部共享类型
package types

import (
	"encoding/json"
	"time"
)

// ──── OpenAI-compatible API 类型 ────

// Message 对话消息，兼容 OpenAI Chat Completion 格式
type Message struct {
	Role       string     `json:"role"`                   // system/user/assistant/tool
	Content    string     `json:"content,omitempty"`      // 文本内容（可为空，当 tool_calls 存在时）
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant 的工具调用
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool 角色时关联的 call id
	Name       string     `json:"name,omitempty"`         // tool 名称（tool 角色时可选）
}

// ToolCall 表示 LLM 请求调用一个工具
type ToolCall struct {
	Index    int          `json:"index,omitempty"` // 流式响应中的工具调用序号
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall 工具调用的函数名和参数
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 编码的参数
}

// ChatRequest OpenAI-compatible 请求体
type ChatRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	Stream      bool             `json:"stream"`
}

// ChatResponse 非流式响应
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Model   string   `json:"model"`
	Usage   Usage    `json:"usage,omitempty"`
	Choices []Choice `json:"choices"`
}

// ChatStreamChunk SSE 流式响应的单个 chunk
type ChatStreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"` // 某些 provider 在最后一个 chunk 返回 usage
}

// StreamChoice 流式 choice
type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        Delta       `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

// Delta 流式增量内容
type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Choice 非流式 choice
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage token 用量信息
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ──── 工具系统类型 ────

// ToolDefinition 工具定义，序列化为 OpenAI function schema
type ToolDefinition struct {
	Type     string         `json:"type"` // "function"
	Function FunctionSchema `json:"function"`
}

// FunctionSchema JSON Schema for a function tool
type FunctionSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolExecutor 工具执行函数签名
// ctx 提供工具执行的上下文信息（工作目录、session 等）
// input 是模型传入的 JSON 参数，已解码为 map
// 返回工具执行结果字符串和可能的错误
type ToolExecutor func(ctx ToolContext, input map[string]any) (string, error)

// ToolContext 工具执行的上下文
type ToolContext struct {
	WorkDir   string // 当前工作目录
	SessionID string // 当前会话 ID
}

// RegisteredTool 内部注册的工具（包含执行器和元数据）
type RegisteredTool struct {
	Definition       ToolDefinition
	Executor         ToolExecutor
	RequiresApproval bool   // 是否需要用户审批
	RiskLevel        string // "readonly" / "write" / "shell"
}

// ToolOutput 工具执行的输出结果
type ToolOutput struct {
	ToolCallID string `json:"tool_call_id"` // 关联的 tool_call id
	Name       string `json:"name"`         // 工具名称
	Output     string `json:"output"`       // 输出内容
	Truncated  bool   `json:"truncated"`    // 是否被截断
	DiskPath   string `json:"disk_path,omitempty"` // 完整输出落盘路径
}

// ──── Session 事件类型 ────

// EventType 会话事件类型枚举
type EventType string

const (
	EventMessage      EventType = "message"
	EventToolCall     EventType = "tool_call"
	EventToolResult   EventType = "tool_result"
	EventPermission   EventType = "permission"
	EventCompact      EventType = "compact"
	EventError        EventType = "error"
	EventSessionStart EventType = "session_start"
)

// SessionEvent JSONL 会话中的单个事件
type SessionEvent struct {
	Type      EventType  `json:"type"`
	Timestamp int64      `json:"ts"`
	Data      any        `json:"data"` // 具体事件载荷
}

// MessageEvent 消息事件载荷
type MessageEvent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToolCallEvent 工具调用事件载荷
type ToolCallEvent struct {
	CallID    string         `json:"call_id"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolResultEvent 工具结果事件载荷
type ToolResultEvent struct {
	CallID    string `json:"call_id"`
	ToolName  string `json:"tool_name"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
	DiskPath  string `json:"disk_path,omitempty"`
	Duration  int64  `json:"duration_ms"`
}

// PermissionEvent 权限审批事件载荷
type PermissionEvent struct {
	CallID   string `json:"call_id"`
	ToolName string `json:"tool_name"`
	Decision string `json:"decision"` // allow / deny
}

// CompactEvent 压缩事件载荷
type CompactEvent struct {
	Method        string `json:"method"` // snip / model_compact
	CollapsedSpan []int  `json:"collapsed_span"` // [start_idx, end_idx]
	Summary       string `json:"summary,omitempty"`
}

// ──── Agent 类型 ────

// AgentState agent 当前状态
type AgentState int

const (
	StateIdle            AgentState = iota // 等待用户输入
	StateStreaming                         // LLM 正在流式返回
	StateExecuting                         // 正在执行工具
	StateAwaitingPermission                // 等待用户审批
	StateError                             // 错误状态
)

func (s AgentState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateStreaming:
		return "streaming"
	case StateExecuting:
		return "executing"
	case StateAwaitingPermission:
		return "awaiting_permission"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// AgentConfig agent 运行时配置
type AgentConfig struct {
	MaxToolRounds      int     // 最大工具调用轮数，默认 15
	CompactThreshold   float64 // token 用量触发 compact 的阈值，默认 0.7
	ForceSnipThreshold float64 // token 用量强制 snip 的阈值，默认 0.9
	MaxOutputPreview   int     // 大工具输出在上下文中保留的预览长度，默认 2000
	MaxOutputDisk      int     // 触发落盘的输出长度阈值，默认 5000
	WorkDir            string  // 工作目录
	ModelMaxTokens     int     // 模型上下文窗口大小
}

// ──── 权限类型 ────

// PermissionDecision 权限决策
type PermissionDecision string

const (
	PermAllow     PermissionDecision = "allow"
	PermDeny      PermissionDecision = "deny"
	PermAllowAll  PermissionDecision = "allow_all"  // 本次会话始终允许该工具
	PermDenyAll   PermissionDecision = "deny_all"   // 本次会话始终拒绝该工具
)

// PermissionRequest 权限审批请求
type PermissionRequest struct {
	CallID      string         `json:"call_id"`
	ToolName    string         `json:"tool_name"`
	Description string         `json:"description"`  // 人类可读的操作描述
	Diff        string         `json:"diff,omitempty"` // 文件修改时附带 diff
	Command     string         `json:"command,omitempty"` // shell 命令时附带完整命令
	RiskLevel   string         `json:"risk_level"`
}

// ──── MCP 类型 ────

// MCPConfig MCP server 配置
type MCPConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"` // HTTP 模式时的 URL
}

// JSONRPCRequest JSON-RPC 2.0 请求
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse JSON-RPC 2.0 响应
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError JSON-RPC 错误
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPToolDefinition MCP 服务端返回的工具定义
type MCPToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ──── 配置类型 ────

// AppConfig 完整应用配置
type AppConfig struct {
	Model       ModelConfig          `yaml:"model"`
	Preferences Preferences          `yaml:"preferences"`
	MCPServers  map[string]MCPConfig `yaml:"mcp_servers"`
	Version     string               `yaml:"config_format_version"`
}

// ModelConfig 单个模型配置
type ModelConfig struct {
	Name       string `yaml:"name"`
	Endpoint   string `yaml:"endpoint"`
	AuthEnvVar string `yaml:"auth_env_var"`
	MaxTokens  int    `yaml:"max_tokens"`
}

// Preferences 用户偏好设置
type Preferences struct {
	MaxToolRounds    int     `yaml:"max_tool_rounds"`
	CompactThreshold float64 `yaml:"compact_threshold"`
	ProjectDir       string  `yaml:"project_dir"`
}

// ──── 流式回调 ────

// StreamCallback 流式输出回调
// content 是累积的全部文本，done 表示是否结束，err 是可能的错误
type StreamCallback func(content string, done bool, err error)

// ToolStatusCallback 工具执行状态回调
type ToolStatusCallback func(status string, toolName string, detail string)

// ──── 辅助函数 ────

// NewAgentConfig 返回带默认值的 AgentConfig
func NewAgentConfig() AgentConfig {
	return AgentConfig{
		MaxToolRounds:      15,
		CompactThreshold:   0.7,
		ForceSnipThreshold: 0.9,
		MaxOutputPreview:   2000,
		MaxOutputDisk:      5000,
		WorkDir:            ".",
		ModelMaxTokens:     128000,
	}
}

// Now 返回当前 Unix 时间戳
func Now() int64 {
	return time.Now().Unix()
}

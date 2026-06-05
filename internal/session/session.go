package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kiritosuki/gocoder/config"
	"github.com/kiritosuki/gocoder/internal/types"
)

// Session 管理 JSONL 追加式会话文件
type Session struct {
	path    string
	file    *os.File
	writer  *bufio.Writer
	id      string
	started int64
	events  int
}

// New 创建新的会话文件
func New() (*Session, error) {
	dir, err := config.SessionsDir()
	if err != nil {
		return nil, fmt.Errorf("获取会话目录失败: %w", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建会话目录失败: %w", err)
	}

	id := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("session_%s.jsonl", id)
	fullPath := filepath.Join(dir, filename)

	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("创建会话文件失败: %w", err)
	}

	s := &Session{
		path:    fullPath,
		file:    f,
		writer:  bufio.NewWriter(f),
		id:      id,
		started: types.Now(),
	}

	// 写入 session_start 事件
	s.Append(types.SessionEvent{
		Type:      types.EventSessionStart,
		Timestamp: s.started,
		Data: map[string]string{
			"session_id": id,
			"version":    "1.0",
		},
	})

	return s, nil
}

// Open 打开已有会话文件
func Open(path string) (*Session, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("打开会话文件失败: %w", err)
	}

	id := filepath.Base(path)
	id = id[8 : len(id)-6] // strip prefix and suffix

	return &Session{
		path:   path,
		file:   f,
		writer: bufio.NewWriter(f),
		id:     id,
	}, nil
}

// Append 追加一个事件到会话文件
func (s *Session) Append(event types.SessionEvent) error {
	if event.Timestamp == 0 {
		event.Timestamp = types.Now()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化事件失败: %w", err)
	}
	data = append(data, '\n')
	if _, err := s.writer.Write(data); err != nil {
		return fmt.Errorf("写入事件失败: %w", err)
	}
	s.events++
	return s.writer.Flush()
}

// AppendMessage 便捷方法：追加消息事件
func (s *Session) AppendMessage(role, content string) error {
	return s.Append(types.SessionEvent{
		Type: types.EventMessage,
		Data: types.MessageEvent{Role: role, Content: content},
	})
}

// AppendAgentMessage preserves OpenAI-style tool call/result structure in the
// JSONL trace instead of flattening every message into plain text.
func (s *Session) AppendAgentMessage(msg types.Message) error {
	if msg.Role == "system" {
		return nil
	}
	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		if msg.Content != "" {
			if err := s.AppendMessage(msg.Role, msg.Content); err != nil {
				return err
			}
		}
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{"_raw": tc.Function.Arguments}
			}
			if err := s.AppendToolCall(tc.ID, tc.Function.Name, args); err != nil {
				return err
			}
		}
		return nil
	}
	if msg.Role == "tool" {
		return s.AppendToolResult(msg.ToolCallID, msg.Name, msg.Content, false, "", 0)
	}
	return s.AppendMessage(msg.Role, msg.Content)
}

// AppendToolCall 便捷方法：追加工具调用事件
func (s *Session) AppendToolCall(callID, toolName string, args map[string]any) error {
	return s.Append(types.SessionEvent{
		Type: types.EventToolCall,
		Data: types.ToolCallEvent{CallID: callID, ToolName: toolName, Arguments: args},
	})
}

// AppendToolResult 便捷方法：追加工具结果事件
func (s *Session) AppendToolResult(callID, toolName, output string, truncated bool, diskPath string, durationMs int64) error {
	return s.Append(types.SessionEvent{
		Type: types.EventToolResult,
		Data: types.ToolResultEvent{
			CallID: callID, ToolName: toolName, Output: output,
			Truncated: truncated, DiskPath: diskPath, Duration: durationMs,
		},
	})
}

// AppendPermission 便捷方法：追加权限事件
func (s *Session) AppendPermission(callID, toolName, decision string) error {
	return s.Append(types.SessionEvent{
		Type: types.EventPermission,
		Data: types.PermissionEvent{CallID: callID, ToolName: toolName, Decision: decision},
	})
}

// AppendCompact 便捷方法：追加压缩事件
func (s *Session) AppendCompact(method string, span []int, summary string) error {
	return s.Append(types.SessionEvent{
		Type: types.EventCompact,
		Data: types.CompactEvent{Method: method, CollapsedSpan: span, Summary: summary},
	})
}

// ID 返回会话 ID
func (s *Session) ID() string {
	return s.id
}

// Path 返回会话文件路径
func (s *Session) Path() string {
	return s.path
}

// Close 关闭会话文件
func (s *Session) Close() error {
	if err := s.writer.Flush(); err != nil {
		return err
	}
	return s.file.Close()
}

// ──── 会话加载和恢复 ────

// LoadEvents 从 JSONL 文件加载所有事件
func LoadEvents(path string) ([]types.SessionEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开会话文件失败: %w", err)
	}
	defer f.Close()

	var events []types.SessionEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var event types.SessionEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // 跳过损坏的行
		}
		events = append(events, event)
	}

	return events, scanner.Err()
}

// EventsToMessages 将事件列表恢复为 messages 数组
func EventsToMessages(events []types.SessionEvent) []types.Message {
	var msgs []types.Message
	toolResults := map[string]string{} // call_id → output

	for _, ev := range events {
		switch ev.Type {
		case types.EventMessage:
			data := ev.Data.(map[string]any)
			msgs = append(msgs, types.Message{
				Role:    strVal(data, "role"),
				Content: strVal(data, "content"),
			})
		case types.EventToolCall:
			data := ev.Data.(map[string]any)
			callID := strVal(data, "call_id")
			toolName := strVal(data, "tool_name")
			args, _ := json.Marshal(data["arguments"])

			msgs = append(msgs, types.Message{
				Role: "assistant",
				ToolCalls: []types.ToolCall{{
					ID:   callID,
					Type: "function",
					Function: types.FunctionCall{
						Name:      toolName,
						Arguments: string(args),
					},
				}},
			})

		case types.EventToolResult:
			data := ev.Data.(map[string]any)
			callID := strVal(data, "call_id")
			output := strVal(data, "output")
			// 检查是否有落盘文件，如果有就加载
			if diskPath := strVal(data, "disk_path"); diskPath != "" {
				if diskData, err := os.ReadFile(diskPath); err == nil {
					output = string(diskData)
				}
			}
			toolResults[callID] = output
			msgs = append(msgs, types.Message{
				Role:       "tool",
				ToolCallID: callID,
				Name:       strVal(data, "tool_name"),
				Content:    output,
			})

		case types.EventCompact:
			// compact 事件不计入 messages，但影响 context collapse
			// 实际恢复时在 session 层面处理
		}
	}

	return msgs
}

// ListSessions 列出所有会话文件
func ListSessions() ([]string, error) {
	dir, err := config.SessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sessions []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			sessions = append(sessions, filepath.Join(dir, e.Name()))
		}
	}
	return sessions, nil
}

func strVal(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

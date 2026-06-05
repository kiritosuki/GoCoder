// Package llm 提供 OpenAI-compatible HTTP 客户端，支持 SSE 流式传输、工具调用、自动重试
package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kiritosuki/gocoder/internal/types"
)

const (
	defaultTimeout   = 180 * time.Second
	maxRetries       = 3
	retryBaseDelay   = 1 * time.Second
)

// Client OpenAI-compatible API 客户端
type Client struct {
	config     types.ModelConfig
	httpClient *http.Client

	// 流式回调
	streamCallback types.StreamCallback

	// 历史消息（由外部管理，这里只负责传输）
	messages []types.Message

	// 最后一次请求的 usage（用于 token 计账）
	lastUsage *types.Usage
}

// NewClient 创建 LLM 客户端
func NewClient(cfg types.ModelConfig) *Client {
	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// SetStreamCallback 设置流式输出回调
func (c *Client) SetStreamCallback(cb types.StreamCallback) {
	c.streamCallback = cb
}

// LastUsage 返回最后一次请求的 token usage
func (c *Client) LastUsage() *types.Usage {
	return c.lastUsage
}

// Chat 发送对话请求（支持工具调用），返回完整响应
// 内部自动处理流式和非流式
func (c *Client) Chat(messages []types.Message, tools []types.ToolDefinition) (*ChatResult, error) {
	c.messages = messages

	req := types.ChatRequest{
		Model:       c.config.Name,
		Messages:    messages,
		Tools:       tools,
		MaxTokens:   4096,
		Temperature: 0,
		Stream:      true,
	}

	result, err := c.chatWithRetry(req)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ChatCompact 发送 compact 请求（不带工具，生成摘要）
func (c *Client) ChatCompact(messages []types.Message, prompt string) (string, error) {
	compactMessages := make([]types.Message, len(messages))
	copy(compactMessages, messages)
	compactMessages = append(compactMessages, types.Message{
		Role:    "user",
		Content: prompt,
	})

	req := types.ChatRequest{
		Model:       c.config.Name,
		Messages:    compactMessages,
		MaxTokens:   1024,
		Temperature: 0,
		Stream:      false,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var chatResp types.ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("解析 compact 响应失败: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("compact 返回空响应")
	}
	return chatResp.Choices[0].Message.Content, nil
}

// ──── 内部实现 ────

// ChatResult 一次完整的 LLM 响应结果
type ChatResult struct {
	Content   string            // 文本内容（可能为空，当有 tool_calls 时）
	ToolCalls []types.ToolCall  // 工具调用列表
	Usage     *types.Usage      // token 用量
}

func (c *Client) chatWithRetry(req types.ChatRequest) (*ChatResult, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay * time.Duration(1<<(attempt-1))
			time.Sleep(delay)
			if c.streamCallback != nil {
				c.streamCallback("", false, fmt.Errorf("[重试 %d/%d，等待 %v...]", attempt, maxRetries, delay))
			}
		}

		result, err := c.streamChat(req)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// 空响应错误 — 值得重试
		if strings.Contains(err.Error(), "空响应") {
			continue
		}
		// 服务端错误 — 值得重试
		if strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "502") ||
			strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "429") {
			continue
		}
		// 客户端错误不重试
		if strings.Contains(err.Error(), "400") || strings.Contains(err.Error(), "401") ||
			strings.Contains(err.Error(), "403") {
			return nil, err
		}
	}
	return nil, fmt.Errorf("请求失败（已重试%d次）: %w", maxRetries, lastErr)
}

func (c *Client) streamChat(req types.ChatRequest) (*ChatResult, error) {
	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API 错误 %d: %s", resp.StatusCode, string(body))
	}

	return c.processStream(resp.Body)
}

func (c *Client) sendRequest(req types.ChatRequest) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	// Auth header — main.go 已在启动前注入实际 key 到 AuthEnvVar
	authValue := c.config.AuthEnvVar
	if strings.Contains(c.config.Endpoint, "openai.azure.com") {
		httpReq.Header.Set("Api-Key", authValue)
	} else {
		httpReq.Header.Set("Authorization", "Bearer "+authValue)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	return c.httpClient.Do(httpReq)
}

func (c *Client) processStream(body io.Reader) (*ChatResult, error) {
	result := &ChatResult{
		ToolCalls: []types.ToolCall{},
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max per line

	toolCallAccum := map[int]*types.ToolCall{} // 按 index 累积 tool_call 片段

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			continue
		}
		if line == "data: [DONE]" {
			break
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		// 某些实现不带空格
		payload = strings.TrimPrefix(payload, "data:")

		var chunk types.ChatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 跳过解析异常的行
		}

		// 收集 usage（某些 provider 在最后 chunk 返回）
		if chunk.Usage != nil {
			result.Usage = chunk.Usage
			c.lastUsage = chunk.Usage
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		// 累积文本内容
		if choice.Delta.Content != "" {
			result.Content += choice.Delta.Content
			if c.streamCallback != nil {
				c.streamCallback(result.Content, false, nil)
			}
		}

		// 累积 tool_calls
		for _, tc := range choice.Delta.ToolCalls {
			idx := tc.Index
			if _, ok := toolCallAccum[idx]; !ok {
				toolCallAccum[idx] = &types.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: types.FunctionCall{
						Name: tc.Function.Name,
					},
				}
			}
			if tc.ID != "" {
				toolCallAccum[idx].ID = tc.ID
			}
			if tc.Function.Name != "" {
				toolCallAccum[idx].Function.Name = tc.Function.Name
			}
			toolCallAccum[idx].Function.Arguments += tc.Function.Arguments
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取流失败: %w", err)
	}

	// 空响应检测
	if result.Content == "" && len(toolCallAccum) == 0 {
		return nil, fmt.Errorf("空响应: 模型未返回任何内容")
	}

	// 收集 tool_calls
	for i := 0; i < len(toolCallAccum); i++ {
		if tc, ok := toolCallAccum[i]; ok {
			result.ToolCalls = append(result.ToolCalls, *tc)
		}
	}

	// 流式结束通知
	if c.streamCallback != nil {
		c.streamCallback(result.Content, true, nil)
	}

	return result, nil
}

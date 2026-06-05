// Package compact 提供上下文压缩和 token 估算功能
package compact

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/kiritosuki/gocoder/internal/types"
)

// ──── Token 估算 ────

// EstimateTokens 基于文本内容估算 token 数
// 规则: 英文 ~chars/4, 中文 ~chars*0.6, 混合场景取平均值
func EstimateTokens(text string) int {
	chars := utf8.RuneCountInString(text)
	cjkCount := 0
	for _, r := range text {
		if (r >= 0x4E00 && r <= 0x9FFF) ||
			(r >= 0x3040 && r <= 0x30FF) ||
			(r >= 0xAC00 && r <= 0xD7AF) {
			cjkCount++
		}
	}
	nonCJK := chars - cjkCount
	// CJK: ~0.6 token/char, non-CJK: ~0.25 token/char
	return int(float64(cjkCount)*0.6 + float64(nonCJK)*0.25)
}

// EstimateTokensFromMessages 估算消息列表的总 token 数
func EstimateTokensFromMessages(msgs []types.Message) int {
	total := 0
	for _, m := range msgs {
		// 每条消息有 ~4 token 的格式开销
		total += 4
		total += EstimateTokens(m.Role)
		total += EstimateTokens(m.Content)
		for _, tc := range m.ToolCalls {
			total += EstimateTokens(tc.Function.Name)
			total += EstimateTokens(tc.Function.Arguments)
			total += EstimateTokens(tc.ID)
		}
	}
	return total
}

// ──── Snip Compact（确定性裁剪） ────

// MessageGroup 表示不可分割的消息组
type MessageGroup struct {
	StartIdx int              // 在消息数组中的起始索引
	EndIdx   int              // 结束索引（不包含）
	Type     string           // "simple" / "tool_round"
	Messages []types.Message
}

// BuildMessageGroups 将消息列表分成不可切割的语义组
// 规则: assistant(tool_calls) + tool(result)* 必须保留或一起移除
func BuildMessageGroups(msgs []types.Message) []MessageGroup {
	var groups []MessageGroup
	i := 0
	for i < len(msgs) {
		msg := msgs[i]

		// assistant 带 tool_calls → 包含后续所有 tool 结果
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			end := i + 1
			// 收集所有关联的 tool 结果
			callIDs := map[string]bool{}
			for _, tc := range msg.ToolCalls {
				callIDs[tc.ID] = true
			}
			for end < len(msgs) && msgs[end].Role == "tool" && callIDs[msgs[end].ToolCallID] {
				end++
			}
			groups = append(groups, MessageGroup{
				StartIdx: i,
				EndIdx:   end,
				Type:     "tool_round",
				Messages: msgs[i:end],
			})
			i = end
		} else {
			groups = append(groups, MessageGroup{
				StartIdx: i,
				EndIdx:   i + 1,
				Type:     "simple",
				Messages: msgs[i : i+1],
			})
			i++
		}
	}
	return groups
}

// FindSafeSplitPoint 找到最早的安全分割点
// 保护: 最近3轮对话、最近用户消息、文件编辑上下文、最近的错误
func FindSafeSplitPoint(msgs []types.Message) int {
	if len(msgs) <= 10 {
		return 0 // 太短不分割
	}

	groups := BuildMessageGroups(msgs)
	if len(groups) <= 4 {
		return 0
	}

	// 保留最近 3 轮 tool_round
	protectedFromEnd := 0
	toolRoundCount := 0
	for i := len(groups) - 1; i >= 0; i-- {
		protectedFromEnd++
		if groups[i].Type == "tool_round" {
			toolRoundCount++
			if toolRoundCount >= 3 {
				break
			}
		}
	}

	if protectedFromEnd >= len(groups) {
		return 0
	}

	// splitIdx 是最早可以安全移除的位置
	cutGroupIdx := len(groups) - protectedFromEnd
	if cutGroupIdx <= 0 {
		return 0
	}

	return groups[cutGroupIdx].StartIdx
}

// Snip 执行确定性裁剪，移除早期消息组
// 返回裁剪后的消息列表和移除的数量
func Snip(msgs []types.Message, tokenLimit int) ([]types.Message, int) {
	if len(msgs) <= 6 {
		return msgs, 0
	}

	splitIdx := FindSafeSplitPoint(msgs)
	if splitIdx <= 2 {
		return msgs, 0
	}

	removed := msgs[:splitIdx]
	kept := msgs[splitIdx:]

	// 构造 snipped 上下文前缀
	boundary := BuildSnipBoundary(len(removed), removed)

	// 插入边界消息作为 system 消息
	result := make([]types.Message, 0, len(kept)+1)
	result = append(result, types.Message{
		Role:    "system",
		Content: boundary,
	})
	result = append(result, kept...)

	return result, len(removed)
}

// BuildSnipBoundary 构造 snip 边界提示文本
func BuildSnipBoundary(removedCount int, removed []types.Message) string {
	// 提取关键信息
	var keyInfo []string
	for _, m := range removed {
		switch m.Role {
		case "user":
			content := m.Content
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			keyInfo = append(keyInfo, fmt.Sprintf("用户问: %s", content))
		case "assistant":
			if len(m.ToolCalls) > 0 {
				tools := []string{}
				for _, tc := range m.ToolCalls {
					tools = append(tools, tc.Function.Name)
				}
				keyInfo = append(keyInfo, fmt.Sprintf("调用工具: %v", tools))
			}
		}
	}

	template := `[Snipped earlier conversation segment]
%d earlier messages were removed to save context space. This usually happens after a longer conversation.

Key moments from the removed segment:
%s

The conversation continues below. If you need information from the earlier part, use your tools to re-read files or ask the user.`

	keyText := ""
	for _, info := range keyInfo {
		keyText += fmt.Sprintf("- %s\n", info)
	}
	if keyText == "" {
		keyText = "(no significant tool calls or user requests in removed segment)\n"
	}

	return fmt.Sprintf(template, removedCount, keyText)
}

// ──── Model Compact（模型摘要压缩） ────

// CompactClient 是 compact 功能所需的 LLM 接口
type CompactClient interface {
	ChatCompact(messages []types.Message, prompt string) (string, error)
}

// ModelCompact 调用模型生成结构化摘要
func ModelCompact(client CompactClient, oldMessages []types.Message) (string, error) {
	prompt := `Please create a structured summary of the conversation segment above. Output in this JSON format:
{
  "task_goal": "what the user wants to accomplish",
  "decisions_made": ["decision 1", "decision 2"],
  "files_modified": {"path": "what changed"},
  "key_findings": ["finding 1", "finding 2"],
  "next_steps": ["step 1"]
}
Keep it concise. Only output the JSON, no other text.`

	summary, err := client.ChatCompact(oldMessages, prompt)
	if err != nil {
		return "", fmt.Errorf("模型摘要失败: %w", err)
	}
	return summary, nil
}

// ──── 工具结果落盘管理 ────

// SaveToolResult 将大工具输出保存到磁盘
func SaveToolResult(toolName, content string) string {
	dir := filepath.Join(os.Getenv("HOME"), ".gocoder", "tool_results")
	os.MkdirAll(dir, 0755)

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s.txt", timestamp, sanitizeName(toolName))
	fullPath := filepath.Join(dir, filename)

	os.WriteFile(fullPath, []byte(content), 0644)
	return fullPath
}

// LoadToolResult 从磁盘加载工具输出
func LoadToolResult(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取工具输出文件失败: %w", err)
	}
	return string(data), nil
}

func sanitizeName(name string) string {
	result := ""
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			result += string(r)
		}
	}
	if result == "" {
		result = "output"
	}
	return result
}

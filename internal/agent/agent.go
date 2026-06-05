// Package agent 是 GoCoder 的核心——Agent 循环引擎。
// 负责编排 LLM ↔ 工具调用 的多轮交互，管理消息上下文，处理 token 感知和紧凑。
package agent

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/kiritosuki/gocoder/internal/compact"
	"github.com/kiritosuki/gocoder/internal/llm"
	"github.com/kiritosuki/gocoder/internal/permission"
	"github.com/kiritosuki/gocoder/internal/tools"
	"github.com/kiritosuki/gocoder/internal/types"
)

// Agent 是 coding agent 的核心控制器
type Agent struct {
	client   *llm.Client
	registry *tools.Registry
	perm     *permission.Runner

	cfg types.AgentConfig

	// 消息历史（热上下文）
	messages []types.Message

	// System prompt
	systemPrompt string

	// 工具输出落盘管理
	diskOutputs map[string]string // tool_call_id → disk_path

	// 回调
	streamCB types.StreamCallback
	toolCB   types.ToolStatusCallback
	permCB   func(req types.PermissionRequest) types.PermissionDecision

	// token 追踪
	tokenTotal int // 当前总 token 估计
	tokenLimit int // 模型窗口上限
}

// New 创建一个 Agent
func New(client *llm.Client, registry *tools.Registry, cfg types.AgentConfig) *Agent {
	if cfg.MaxToolRounds == 0 {
		cfg.MaxToolRounds = 15
	}
	if cfg.CompactThreshold == 0 {
		cfg.CompactThreshold = 0.7
	}
	if cfg.ForceSnipThreshold == 0 {
		cfg.ForceSnipThreshold = 0.9
	}
	if cfg.MaxOutputPreview == 0 {
		cfg.MaxOutputPreview = 2000
	}
	if cfg.MaxOutputDisk == 0 {
		cfg.MaxOutputDisk = 5000
	}
	if cfg.ModelMaxTokens == 0 {
		cfg.ModelMaxTokens = 128000
	}

	return &Agent{
		client:      client,
		registry:    registry,
		perm:        permission.NewRunner(),
		cfg:         cfg,
		messages:    []types.Message{},
		diskOutputs: map[string]string{},
		tokenLimit:  cfg.ModelMaxTokens,
	}
}

// SetCallbacks 设置回调
func (a *Agent) SetCallbacks(
	streamCB types.StreamCallback,
	toolCB types.ToolStatusCallback,
	permCB func(req types.PermissionRequest) types.PermissionDecision,
) {
	a.streamCB = streamCB
	a.toolCB = toolCB
	a.permCB = permCB
	a.client.SetStreamCallback(streamCB)
}

// SetSystemPrompt 设置系统提示词
func (a *Agent) SetSystemPrompt(prompt string) {
	if prompt != "" {
		a.messages = append([]types.Message{{Role: "system", Content: prompt}}, a.messages...)
	}
	a.systemPrompt = prompt
}

// Messages 返回当前消息列表（只读）
func (a *Agent) Messages() []types.Message {
	return a.messages
}

// TokenUsage 返回当前 token 使用情况
func (a *Agent) TokenUsage() (used, limit int) {
	return a.tokenTotal, a.tokenLimit
}

// Run 执行一次 Agent 交互轮次
// userInput: 用户输入文本
// 返回: assistant 的文本响应, 可能的错误
func (a *Agent) Run(userInput string) (string, error) {
	// 1. 添加用户消息
	a.messages = append(a.messages, types.Message{
		Role:    "user",
		Content: userInput,
	})

	// 2. 进入 agent loop
	return a.agentLoop()
}

// Resume 从已有的消息历史恢复执行
func (a *Agent) Resume(messages []types.Message) (string, error) {
	a.messages = messages
	return a.agentLoop()
}

// ──── Agent Loop 核心 ────

func (a *Agent) agentLoop() (string, error) {
	for round := 0; round < a.cfg.MaxToolRounds; round++ {
		// Token 检查
		a.updateTokenEstimate()

		// 超 90% → 强制 snip
		if a.tokenUsageRatio() >= a.cfg.ForceSnipThreshold {
			log.Printf("[agent] token 用量 %.0f%%, 触发强制 snip", a.tokenUsageRatio()*100)
			a.forceSnip()
		} else if a.tokenUsageRatio() >= a.cfg.CompactThreshold {
			// 超 70% → 尝试 model compact
			log.Printf("[agent] token 用量 %.0f%%, 触发 model compact", a.tokenUsageRatio()*100)
			a.modelCompact()
		}

		// 调用 LLM
		toolDefs := a.registry.List()
		log.Printf("[agent] 第 %d 轮 LLM 调用，消息数: %d，工具数: %d", round+1, len(a.messages), len(toolDefs))

		result, err := a.client.Chat(a.messages, toolDefs)
		if err != nil {
			return "", fmt.Errorf("LLM 调用失败: %w", err)
		}

		// 更新 token 用量
		if result.Usage != nil {
			a.tokenTotal = result.Usage.PromptTokens + result.Usage.CompletionTokens
		}

		// 情况1: 模型返回文本，无工具调用 → 完成
		if len(result.ToolCalls) == 0 {
			a.messages = append(a.messages, types.Message{
				Role:    "assistant",
				Content: result.Content,
			})
			return result.Content, nil
		}

		// 情况2: 模型请求工具调用
		// 构建 assistant 消息（包含 tool_calls）
		assistantMsg := types.Message{
			Role:      "assistant",
			Content:   result.Content, // 某些模型在 tool_calls 前也有文本
			ToolCalls: result.ToolCalls,
		}
		a.messages = append(a.messages, assistantMsg)

		// 执行每个工具调用
		for _, tc := range result.ToolCalls {
			funcName := tc.Function.Name
			log.Printf("[agent] 执行工具: %s (id=%s)", funcName, tc.ID)

			if a.toolCB != nil {
				a.toolCB("executing", funcName, tc.ID)
			}

			// 权限审批
			if a.registry.RequiresApproval(funcName) {
				approved := a.requestApproval(tc)
				if !approved {
					// 被拒绝 → 返回拒绝消息作为 tool_result
					denyMsg := fmt.Sprintf("工具 %s 被用户拒绝执行。请尝试其他方案。", funcName)
					a.messages = append(a.messages, types.Message{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       funcName,
						Content:    denyMsg,
					})
					if a.toolCB != nil {
						a.toolCB("denied", funcName, tc.ID)
					}
					continue
				}
			}

			// 解析参数
			var params map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
				params = map[string]any{} // 解析失败用空参数
				log.Printf("[agent] 解析工具参数失败: %v", err)
			}

			// 执行工具
			ctx := types.ToolContext{
				WorkDir:   a.cfg.WorkDir,
				SessionID: "",
			}
			output, err := a.registry.Execute(funcName, ctx, params)
			if err != nil {
				output = fmt.Sprintf("工具执行错误: %v", err)
			}

			// 大输出处理
			diskPath := ""
			truncated := false
			if len(output) > a.cfg.MaxOutputDisk {
				diskPath = a.saveToDisk(funcName, output)
				output = a.truncatePreview(output)
				truncated = true
				a.diskOutputs[tc.ID] = diskPath
			}

			// 添加 tool_result 消息
			toolResult := output
			if truncated {
				toolResult = fmt.Sprintf("%s\n\n[完整输出已保存至: %s，可用 read_tool_result 工具读取]", output, diskPath)
			}

			a.messages = append(a.messages, types.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       funcName,
				Content:    toolResult,
			})

			if a.toolCB != nil {
				a.toolCB("completed", funcName, tc.ID)
			}
		}

		// 继续下一轮循环，LLM 会看到 tool_result 并可能继续调用工具或返回文本
	}

	return "", fmt.Errorf("达到最大工具调用轮数 (%d)，Agent 循环终止", a.cfg.MaxToolRounds)
}

// ──── 权限审批 ────

func (a *Agent) requestApproval(tc types.ToolCall) bool {
	funcName := tc.Function.Name

	// 检查 already allow/deny 列表
	decision := a.perm.Check(funcName)
	if decision == types.PermDeny {
		return false
	}
	if decision == types.PermAllow {
		return true
	}

	// 需要交互审批
	if a.permCB != nil {
		var params map[string]any
		json.Unmarshal([]byte(tc.Function.Arguments), &params)

		desc := buildPermissionDescription(tc, params)
		req := types.PermissionRequest{
			CallID:      tc.ID,
			ToolName:    funcName,
			Description: desc,
			RiskLevel:   a.registry.RiskLevel(funcName),
		}
		req.Diff = a.permissionDiff(funcName, params)

		// 对于 shell 命令，附上完整命令
		if funcName == "run_command" {
			if cmd, ok := params["command"].(string); ok {
				req.Command = cmd
				if risks := permission.AnalyzeShellCommand(cmd); len(risks) > 0 {
					req.Description = fmt.Sprintf("%s\n风险: %v", req.Description, risks)
				}
			}
		}

		decision := a.permCB(req)
		a.perm.RecordDecision(tc.ID, decision)
		switch decision {
		case types.PermAllowAll:
			a.perm.AllowAlways(funcName)
			return true
		case types.PermDenyAll:
			a.perm.DenyAlways(funcName)
			return false
		case types.PermAllow:
			return true
		default:
			return false
		}
	}

	// 没有审批回调 → 默认允许只读，拒绝写入
	return a.registry.RiskLevel(funcName) == "readonly"
}

func (a *Agent) permissionDiff(funcName string, params map[string]any) string {
	switch funcName {
	case "write_file":
		diff, err := tools.PreviewWriteDiff(a.cfg.WorkDir, getStr(params, "path"), getStr(params, "content"))
		if err != nil {
			return fmt.Sprintf("diff preview failed: %v", err)
		}
		return diff
	case "edit_file":
		diff, err := tools.PreviewEditDiff(a.cfg.WorkDir, getStr(params, "path"), getStr(params, "old_string"), getStr(params, "new_string"))
		if err != nil {
			return fmt.Sprintf("diff preview failed: %v", err)
		}
		return diff
	default:
		return ""
	}
}

func buildPermissionDescription(tc types.ToolCall, params map[string]any) string {
	funcName := tc.Function.Name
	switch funcName {
	case "write_file":
		return fmt.Sprintf("写入文件: %s", getStr(params, "path"))
	case "edit_file":
		return fmt.Sprintf("编辑文件: %s", getStr(params, "path"))
	case "run_command":
		return fmt.Sprintf("执行命令: %s", getStr(params, "command"))
	default:
		return fmt.Sprintf("执行 %s", funcName)
	}
}

// ──── Token 管理 ────

func (a *Agent) updateTokenEstimate() {
	// 优先用 provider usage，否则本地估算
	if usage := a.client.LastUsage(); usage != nil {
		a.tokenTotal = usage.PromptTokens + usage.CompletionTokens
		return
	}
	a.tokenTotal = compact.EstimateTokensFromMessages(a.messages)
}

func (a *Agent) tokenUsageRatio() float64 {
	if a.tokenLimit == 0 {
		return 0
	}
	return float64(a.tokenTotal) / float64(a.tokenLimit)
}

// forceSnip 强制确定性裁剪
func (a *Agent) forceSnip() {
	snipped, removed := compact.Snip(a.messages, a.tokenLimit)
	if removed > 0 {
		log.Printf("[agent] snip 完成: 移除了 %d 条消息", removed)
		a.messages = snipped
		a.updateTokenEstimate()
	}
}

// modelCompact 通过模型摘要压缩
func (a *Agent) modelCompact() {
	// 只有消息足够多时才 compact
	if len(a.messages) < 10 {
		return
	}

	// 找到最早的安全分割点
	splitIdx := compact.FindSafeSplitPoint(a.messages)
	if splitIdx <= 2 {
		log.Printf("[agent] compact: 未找到安全分割点")
		return
	}

	// 取出早期消息用于摘要
	oldMessages := a.messages[:splitIdx]
	recentMessages := a.messages[splitIdx:]

	// 调用模型生成摘要
	summary, err := compact.ModelCompact(a.client, oldMessages)
	if err != nil {
		log.Printf("[agent] model compact 失败: %v，降级为 snip", err)
		a.forceSnip()
		return
	}

	// 将摘要作为系统消息注入，替换旧消息
	compactMsg := types.Message{
		Role:    "system",
		Content: fmt.Sprintf("[上下文摘要]\n%s\n[/上下文摘要]", summary),
	}

	newMessages := make([]types.Message, 0, len(recentMessages)+2)
	// 保留原始 system prompt（如果存在）
	if len(a.messages) > 0 && a.messages[0].Role == "system" {
		newMessages = append(newMessages, a.messages[0])
	}
	newMessages = append(newMessages, compactMsg)
	newMessages = append(newMessages, recentMessages...)

	a.messages = newMessages
	a.updateTokenEstimate()
	log.Printf("[agent] model compact 完成: %d 条消息 → 摘要, 剩余 %d 条", splitIdx, len(a.messages))
}

// ──── 大输出管理 ────

func (a *Agent) truncatePreview(output string) string {
	preview := a.cfg.MaxOutputPreview
	if len(output) <= preview {
		return output
	}
	tail := 500
	if len(output)-preview < tail {
		tail = len(output) - preview
	}
	return output[:preview] + fmt.Sprintf("\n\n...[截断 %d 字符]...\n\n", len(output)-preview-tail) + output[len(output)-tail:]
}

func (a *Agent) saveToDisk(toolName, content string) string {
	path := compact.SaveToolResult(toolName, content)
	return path
}

// ──── 辅助函数 ────

func getStr(params map[string]any, key string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "?"
}

// ──── 外部接口 ────

// GetDiskOutput 根据 tool_call_id 获取已落盘工具输出的路径
func (a *Agent) GetDiskOutput(toolCallID string) (string, bool) {
	path, ok := a.diskOutputs[toolCallID]
	return path, ok
}

// ClearDiskOutputs 清理落盘输出记录
func (a *Agent) ClearDiskOutputs() {
	a.diskOutputs = map[string]string{}
}

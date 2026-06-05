// Package tui 提供 GoCoder 的终端用户界面。
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/kiritosuki/gocoder/config"
	"github.com/kiritosuki/gocoder/internal/agent"
	"github.com/kiritosuki/gocoder/internal/compact"
	"github.com/kiritosuki/gocoder/internal/llm"
	"github.com/kiritosuki/gocoder/internal/mcp"
	"github.com/kiritosuki/gocoder/internal/permission"
	"github.com/kiritosuki/gocoder/internal/session"
	"github.com/kiritosuki/gocoder/internal/skills"
	toolpkg "github.com/kiritosuki/gocoder/internal/tools"
	"github.com/kiritosuki/gocoder/internal/types"
)

// ──── State ────

type State int

const (
	stateIdle State = iota
	stateStreaming
	stateExecuting
	stateAwaitingPermission
)

// streamChunkMsg SSE 流式增量，由 agent goroutine 推送
type streamChunkMsg struct {
	content    string
	toolStatus string
	err        error
}

// agentDoneMsg agent.Run 完成后的消息
type agentDoneMsg struct {
	finalContent string
	err          error
}

// notifyMsg 系统通知（MCP 连接等），直接追加到聊天记录
type notifyMsg struct{ text string }

type permissionRequestMsg struct {
	req  types.PermissionRequest
	resp chan types.PermissionDecision
}

// ──── Model ────

type model struct {
	agent         *agent.Agent
	llmClient     *llm.Client
	toolRegistry  *toolpkg.Registry
	session       *session.Session
	savedMessages int
	skillMgr      *skills.Manager
	mcpMgr        *mcp.Manager
	permRunner    *permission.Runner

	textInput  textinput.Model
	spinner    spinner.Model
	mdRenderer *glamour.TermRenderer
	program    *tea.Program

	state        State
	chatHistory  string // 完整对话历史
	streamBuf    string // 流式增量（打字机效果）
	toolStatus   string
	errorMsg     string
	pendingPerm  *types.PermissionRequest
	permDecision chan types.PermissionDecision

	scrollOffset int
	maxScroll    int
	termWidth    int
	termHeight   int
	appConfig    types.AppConfig
	modelConfig  types.ModelConfig
	quitting     bool
}

// ──── 初始化 ────

func NewModel(appConfig types.AppConfig, modelConfig types.ModelConfig) *model {
	ti := textinput.New()
	ti.Placeholder = "输入编码任务..."
	ti.Focus()
	ti.Prompt = "> "

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = SpinnerStyle

	r, _ := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(80))

	client := llm.NewClient(modelConfig)

	registry := toolpkg.NewRegistry()
	toolpkg.RegisterBuiltins(registry, toolpkg.BuiltinConfig{
		WorkDir:       appConfig.Preferences.ProjectDir,
		ToolResultDir: getToolResultDir(),
	})

	cfg := types.NewAgentConfig()
	cfg.WorkDir = appConfig.Preferences.ProjectDir
	cfg.MaxToolRounds = appConfig.Preferences.MaxToolRounds
	cfg.CompactThreshold = appConfig.Preferences.CompactThreshold
	cfg.ModelMaxTokens = modelConfig.MaxTokens

	a := agent.New(client, registry, cfg)
	a.SetSystemPrompt(buildSystemPrompt(""))

	sm := skills.NewManager()
	sm.LoadAll()

	return &model{
		agent:        a,
		llmClient:    client,
		toolRegistry: registry,
		skillMgr:     sm,
		mcpMgr:       mcp.NewManager(),
		permRunner:   permission.NewRunner(),
		textInput:    ti,
		spinner:      s,
		mdRenderer:   r,
		state:        stateIdle,
		appConfig:    appConfig,
		modelConfig:  modelConfig,
	}
}

// ──── Bubble Tea 接口 ────

func (m *model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick, m.connectMCPServers())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		m.textInput.Width = msg.Width - 4
		return m, nil

	case tea.MouseMsg:
		return m, m.handleMouse(msg)

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			m.quitting = true
			m.cleanup()
			return m, tea.Quit
		case tea.KeyCtrlS:
			return m, m.saveSession()
		case tea.KeyPgUp:
			m.scrollOffset = min(m.scrollOffset+m.termHeight/2, m.maxScroll)
			return m, nil
		case tea.KeyPgDown:
			m.scrollOffset = max(m.scrollOffset-m.termHeight/2, 0)
			return m, nil
		}

	case streamChunkMsg:
		return m.handleStreamChunk(msg)
	case agentDoneMsg:
		return m.handleAgentDone(msg)
	case notifyMsg:
		m.addToHistory(SystemMsgStyle.Render(msg.text))
		return m, nil
	case permissionRequestMsg:
		m.pendingPerm = &msg.req
		m.permDecision = msg.resp
		m.state = stateAwaitingPermission
		return m, nil
	}

	switch m.state {
	case stateIdle:
		return m, m.idleUpdate(msg)
	case stateStreaming:
		return m, m.streamingUpdate(msg)
	case stateExecuting:
		return m, m.executingUpdate(msg)
	case stateAwaitingPermission:
		return m, m.permissionUpdate(msg)
	}
	return m, nil
}

// ──── View ────

func (m *model) View() string {
	if m.quitting {
		return ""
	}

	content := m.buildContent()
	bottom := m.buildBottom()

	// 计算可见区域
	bottomRows := strings.Count(bottom, "\n") + 1
	contentHeight := m.termHeight - bottomRows
	if contentHeight < 1 {
		contentHeight = 1
	}

	lines := strings.Split(content, "\n")
	m.maxScroll = max(len(lines)-contentHeight, 0)
	m.scrollOffset = clamp(m.scrollOffset, m.maxScroll)
	if m.state != stateIdle {
		m.scrollOffset = 0
	}

	start := max(len(lines)-contentHeight-m.scrollOffset, 0)
	end := min(start+contentHeight, len(lines))
	visible := lines[start:end]

	// 内容不够填满时顶部补空行
	for len(visible) < contentHeight {
		visible = append([]string{""}, visible...)
	}

	return strings.Join(visible, "\n") + "\n" + bottom
}

func (m *model) buildContent() string {
	var sb strings.Builder

	if m.chatHistory != "" {
		sb.WriteString(m.chatHistory)
		if !strings.HasSuffix(m.chatHistory, "\n") {
			sb.WriteString("\n")
		}
	}

	if m.state == stateStreaming {
		if m.streamBuf != "" {
			sb.WriteString(m.streamBuf)
			sb.WriteString("\n")
		}
		sb.WriteString(m.spinner.View())
		sb.WriteString(" ")
		sb.WriteString(SystemMsgStyle.Render("生成中..."))
		sb.WriteString("\n")
	}

	if m.state == stateExecuting {
		sb.WriteString(m.spinner.View())
		sb.WriteString(" ")
		sb.WriteString(ToolExecutingStyle.Render(m.toolStatus))
		sb.WriteString("\n")
	}

	if m.errorMsg != "" {
		sb.WriteString(ErrorStyle.Render(m.errorMsg))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m *model) buildBottom() string {
	var sb strings.Builder

	if m.state == stateAwaitingPermission && m.pendingPerm != nil {
		sb.WriteString(PermissionStyle.Render(formatPermissionPrompt(m.pendingPerm)))
		sb.WriteString("\n")
	}

	if m.state == stateIdle {
		sb.WriteString(m.textInput.View())
	} else {
		sb.WriteString(SystemMsgStyle.Render("..."))
	}

	used, limit := m.agent.TokenUsage()
	sb.WriteString("\n")
	sb.WriteString(HelpStyle.Render(fmt.Sprintf("%s · %s/%s tokens",
		m.modelConfig.Name, formatTokens(used), formatTokens(limit))))

	sb.WriteString("\n")
	sb.WriteString(HelpStyle.Render("enter send · ctrl+s save · ctrl+c quit · wheel/PgUp/PgDn scroll"))

	return sb.String()
}

// ──── 状态更新 ────

func (m *model) idleUpdate(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		m.scrollOffset = 0
		return m.handleSubmit()
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return cmd
}

func (m *model) streamingUpdate(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return cmd
}

func (m *model) executingUpdate(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return cmd
}

func (m *model) permissionUpdate(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch strings.ToLower(keyMsg.String()) {
		case "a":
			if m.permDecision != nil {
				m.permDecision <- types.PermAllow
			}
			m.pendingPerm = nil
			m.permDecision = nil
			m.state = stateStreaming
		case "d":
			if m.permDecision != nil {
				m.permDecision <- types.PermDeny
			}
			m.pendingPerm = nil
			m.permDecision = nil
			m.state = stateStreaming
		}
	}
	return nil
}

// ──── 鼠标滚轮 ────

func (m *model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.scrollOffset = min(m.scrollOffset+1, m.maxScroll)
	case tea.MouseButtonWheelDown:
		m.scrollOffset = max(m.scrollOffset-1, 0)
	}
	return nil
}

// ──── 核心交互 ────

func (m *model) handleSubmit() tea.Cmd {
	input := strings.TrimSpace(m.textInput.Value())
	if input == "" {
		return nil
	}
	m.textInput.SetValue("")

	if strings.HasPrefix(input, "/") {
		m.addToHistory(UserMsgStyle.Render("> " + input))
		return m.handleCommand(input)
	}

	m.addToHistory(UserMsgStyle.Render("> " + input))
	m.streamBuf = ""
	m.errorMsg = ""
	m.state = stateStreaming

	if skill := m.skillMgr.Match(input); skill != nil {
		m.skillMgr.Activate(skill.Name)
		if sp := m.skillMgr.BuildSystemPrompt(); sp != "" {
			m.agent.SetSystemPrompt(buildSystemPrompt(sp))
		}
	}

	return tea.Batch(m.spinner.Tick, m.runAgent(input))
}

func (m *model) addToHistory(line string) {
	if m.chatHistory != "" && !strings.HasSuffix(m.chatHistory, "\n") {
		m.chatHistory += "\n"
	}
	m.chatHistory += line + "\n"
}

func (m *model) runAgent(input string) tea.Cmd {
	return func() tea.Msg {
		p := m.program
		if p == nil {
			content, err := m.agent.Run(input)
			return agentDoneMsg{finalContent: content, err: err}
		}
		m.agent.SetCallbacks(
			func(content string, done bool, err error) {
				p.Send(streamChunkMsg{content: content, err: err})
			},
			func(status, toolName, detail string) {
				msg := streamChunkMsg{}
				switch status {
				case "executing":
					msg.toolStatus = "⚙ " + toolName + "..."
				case "completed":
					msg.toolStatus = "✓ " + toolName + " 完成"
				case "denied":
					msg.toolStatus = "✗ " + toolName + " 被拒绝"
				}
				p.Send(msg)
			},
			func(req types.PermissionRequest) types.PermissionDecision {
				if req.RiskLevel == "readonly" {
					return types.PermAllow
				}
				resp := make(chan types.PermissionDecision, 1)
				p.Send(permissionRequestMsg{req: req, resp: resp})
				return <-resp
			},
		)
		content, err := m.agent.Run(input)
		return agentDoneMsg{finalContent: content, err: err}
	}
}

func (m *model) handleStreamChunk(msg streamChunkMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.errorMsg = fmt.Sprintf("错误: %v", msg.err)
		return m, m.spinner.Tick
	}
	if msg.toolStatus != "" {
		if strings.HasPrefix(msg.toolStatus, "⚙") {
			m.state = stateExecuting
		}
		m.toolStatus = msg.toolStatus
		return m, m.spinner.Tick
	}
	if msg.content != "" {
		rendered, err := m.mdRenderer.Render(msg.content)
		if err == nil {
			m.streamBuf = rendered
		} else {
			m.streamBuf = msg.content
		}
		m.state = stateStreaming
	}
	return m, m.spinner.Tick
}

func (m *model) handleAgentDone(msg agentDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.errorMsg = fmt.Sprintf("错误: %v", msg.err)
	} else if m.streamBuf != "" {
		m.addToHistory(m.streamBuf)
	} else if msg.finalContent != "" {
		rendered, err := m.mdRenderer.Render(msg.finalContent)
		if err == nil {
			m.addToHistory(rendered)
		} else {
			m.addToHistory(msg.finalContent)
		}
	}
	m.streamBuf = ""
	m.toolStatus = ""
	m.state = stateIdle
	m.errorMsg = ""
	m.scrollOffset = 0
	return m, textinput.Blink
}

// ──── 内置命令 ────

func (m *model) handleCommand(input string) tea.Cmd {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}

	switch parts[0] {
	case "/help":
		m.addToHistory(helpText())
	case "/save":
		return m.saveSession()
	case "/resume":
		if len(parts) == 1 {
			return m.listSessions()
		}
		return m.resumeSession(parts[1])
	case "/skill":
		if len(parts) > 1 {
			if skill, err := m.skillMgr.Activate(parts[1]); err == nil {
				m.addToHistory("✓ Skill 已激活: " + skill.Name)
				if sp := m.skillMgr.BuildSystemPrompt(); sp != "" {
					m.agent.SetSystemPrompt(buildSystemPrompt(sp))
				}
			} else {
				m.errorMsg = err.Error()
			}
		} else {
			m.addToHistory(fmt.Sprintf("可用 Skills: %v", m.skillMgr.ListNames()))
		}
	case "/models":
		m.addToHistory(fmt.Sprintf("模型: %s | %d tokens | %s",
			m.modelConfig.Name, m.modelConfig.MaxTokens, m.modelConfig.Endpoint))
	case "/tokens":
		used, limit := m.agent.TokenUsage()
		m.addToHistory(fmt.Sprintf("Token: %s / %s (%.1f%%)",
			formatTokens(used), formatTokens(limit), float64(used)/float64(limit)*100))
	case "/mcp":
		m.addToHistory(fmt.Sprintf("MCP Servers: %v", m.mcpMgr.ConnectedServers()))
	}
	m.scrollOffset = 0
	return nil
}

// ──── 会话 ────

func (m *model) saveSession() tea.Cmd {
	return func() tea.Msg {
		if m.session == nil {
			s, err := session.New()
			if err != nil {
				return nil
			}
			m.session = s
		}
		msgs := m.agent.Messages()
		if m.savedMessages > len(msgs) {
			m.savedMessages = 0
		}
		for _, msg := range msgs[m.savedMessages:] {
			if err := m.session.AppendAgentMessage(msg); err != nil {
				m.errorMsg = err.Error()
				return nil
			}
		}
		m.savedMessages = len(msgs)
		m.addToHistory("✓ 会话已保存: " + m.session.ID())
		return nil
	}
}

func (m *model) listSessions() tea.Cmd {
	return func() tea.Msg {
		paths, err := session.ListSessions()
		if err != nil {
			m.errorMsg = err.Error()
			return nil
		}
		if len(paths) == 0 {
			m.addToHistory("暂无已保存会话。使用 /save 保存当前会话。")
			return nil
		}
		var sb strings.Builder
		sb.WriteString("可恢复会话:\n")
		limit := min(len(paths), 10)
		for i := 0; i < limit; i++ {
			id := session.SessionIDFromPath(paths[i])
			sb.WriteString(fmt.Sprintf("- %s  (%s)\n", id, shortPath(paths[i])))
		}
		sb.WriteString("使用 /resume <id> 恢复，例如 /resume ")
		sb.WriteString(session.SessionIDFromPath(paths[0]))
		m.addToHistory(sb.String())
		return nil
	}
}

func (m *model) resumeSession(ref string) tea.Cmd {
	return func() tea.Msg {
		path, err := session.ResolveSessionPath(ref)
		if err != nil {
			m.errorMsg = err.Error()
			return nil
		}
		events, err := session.LoadEvents(path)
		if err != nil {
			m.errorMsg = err.Error()
			return nil
		}
		msgs := session.EventsToMessages(events)
		m.agent.SetMessages(msgs)

		if m.session != nil {
			_ = m.session.Close()
		}
		s, err := session.Open(path)
		if err != nil {
			m.errorMsg = err.Error()
			return nil
		}
		m.session = s
		m.savedMessages = len(m.agent.Messages())
		m.chatHistory = renderRestoredMessages(m.mdRenderer, msgs)
		m.addToHistory(fmt.Sprintf("✓ 已恢复会话: %s (%d messages)", session.SessionIDFromPath(path), len(msgs)))
		m.scrollOffset = 0
		return nil
	}
}

func renderRestoredMessages(renderer *glamour.TermRenderer, msgs []types.Message) string {
	var sb strings.Builder
	for _, msg := range msgs {
		switch msg.Role {
		case "user":
			sb.WriteString(UserMsgStyle.Render("> " + msg.Content))
			sb.WriteString("\n")
		case "assistant":
			if msg.Content != "" {
				content := msg.Content
				if renderer != nil {
					if rendered, err := renderer.Render(msg.Content); err == nil {
						content = rendered
					}
				}
				sb.WriteString(content)
				if !strings.HasSuffix(content, "\n") {
					sb.WriteString("\n")
				}
			}
			for _, tc := range msg.ToolCalls {
				sb.WriteString(ToolExecutingStyle.Render(fmt.Sprintf("↳ tool call: %s %s", tc.Function.Name, tc.Function.Arguments)))
				sb.WriteString("\n")
			}
		case "tool":
			name := msg.Name
			if name == "" {
				name = msg.ToolCallID
			}
			preview := msg.Content
			if len(preview) > 500 {
				preview = preview[:500] + "\n...[truncated in restored view]..."
			}
			sb.WriteString(SystemMsgStyle.Render(fmt.Sprintf("↳ tool result: %s", name)))
			sb.WriteString("\n")
			sb.WriteString(preview)
			if !strings.HasSuffix(preview, "\n") {
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

func helpText() string {
	return strings.Join([]string{
		"命令:",
		"  /help              显示帮助",
		"  /save              保存当前会话到 ~/.gocoder/sessions",
		"  /resume            列出最近会话",
		"  /resume <id|path>  恢复指定会话",
		"  /skill             列出可用 skills",
		"  /skill <name>      激活 skill",
		"  /models            显示当前模型配置",
		"  /tokens            显示 token 估算",
		"  /mcp               显示 MCP 连接状态",
		"",
		"快捷键: Enter 发送 · Ctrl+S 保存 · Ctrl+C 退出 · PgUp/PgDn 滚动",
	}, "\n")
}

func formatPermissionPrompt(req *types.PermissionRequest) string {
	var sb strings.Builder
	sb.WriteString("⚠ ")
	sb.WriteString(req.Description)
	if req.RiskLevel != "" {
		sb.WriteString(" [")
		sb.WriteString(req.RiskLevel)
		sb.WriteString("]")
	}
	if req.Command != "" {
		sb.WriteString("\n$ ")
		sb.WriteString(req.Command)
	}
	if req.Diff != "" {
		diff := req.Diff
		if len(diff) > 1200 {
			diff = diff[:1200] + "\n...[diff truncated]..."
		}
		sb.WriteString("\n")
		sb.WriteString(diff)
	}
	sb.WriteString("\n[A]llow [D]eny")
	return sb.String()
}

func shortPath(path string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + rel
		}
	}
	return path
}

// ──── MCP ────

func (m *model) connectMCPServers() tea.Cmd {
	return func() tea.Msg {
		servers := m.appConfig.MCPServers
		if len(servers) == 0 {
			return nil
		}
		m.program.Send(notifyMsg{text: fmt.Sprintf("⏳ 正在连接 %d 个 MCP server...", len(servers))})

		for name, cfg := range servers {
			serverName := name
			serverCfg := cfg
			go func(sName string, sCfg types.MCPConfig) {
				if err := m.mcpMgr.Connect(sName, sCfg); err != nil {
					m.program.Send(notifyMsg{text: fmt.Sprintf("✗ MCP [%s] 连接失败: %v", sName, err)})
					return
				}
				count := 0
				for _, t := range m.mcpMgr.GetAllTools() {
					if _, ok := m.toolRegistry.Get(t.Name); ok {
						continue
					}
					toolName := t.Name
					m.toolRegistry.Register(&types.RegisteredTool{
						Definition: toolpkg.BuildToolDef(t.Name, t.Description, t.InputSchema),
						Executor: func(ctx types.ToolContext, input map[string]any) (string, error) {
							return m.mcpMgr.CallRegisteredTool(toolName, input)
						},
						RequiresApproval: true,
						RiskLevel:        "mcp",
					})
					count++
				}
				m.program.Send(notifyMsg{text: fmt.Sprintf("✓ MCP [%s] 已连接，注册 %d 个工具", sName, count)})
			}(serverName, serverCfg)
		}
		return nil
	}
}

func (m *model) cleanup() {
	if m.permDecision != nil {
		select {
		case m.permDecision <- types.PermDeny:
		default:
		}
		m.permDecision = nil
	}
	if m.session != nil {
		m.session.Close()
	}
	if m.mcpMgr != nil {
		m.mcpMgr.CloseAll()
	}
}

// ──── 辅助 ────

func buildSystemPrompt(skillContent string) string {
	return `You are GoCoder, a coding assistant. Tools: read_file, write_file, edit_file, list_directory, grep, run_command.
- Read files before editing. Use edit_file for precise changes.
- Prefer safe shell commands.
` + skillContent
}

func formatTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func getToolResultDir() string {
	dir, _ := config.ToolResultsDir()
	return dir
}

func clamp(v, maxVal int) int {
	if v > maxVal {
		return maxVal
	}
	if v < 0 {
		return 0
	}
	return v
}

// ──── 入口 ────

func Run(appConfig types.AppConfig, modelConfig types.ModelConfig) error {
	if err := config.EnsureDirs(); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	_ = compact.EstimateTokens

	m := NewModel(appConfig, modelConfig)
	p := tea.NewProgram(m, tea.WithMouseAllMotion())
	m.program = p

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI 错误: %v\n", err)
		os.Exit(1)
	}
	return nil
}

# GoCoder 阅读指南

这份文档帮你用面试视角阅读 GoCoder。重点看 agent 后端，不用把 TUI 细节背下来。

## 1. 这个项目是什么

GoCoder 是一个轻量级终端 Coding Agent：

- 兼容 OpenAI 标准 Chat Completions API，可以接 GPT、DeepSeek 等兼容模型。
- TUI 负责输入、流式展示、权限确认。
- Agent Loop 负责多轮调用模型和工具。
- Tool Registry 统一管理内置工具和 MCP 工具。
- Skills 把流程知识注入 system prompt。
- Session 用 JSONL 记录完整执行轨迹，支持恢复。

一句话介绍：

> GoCoder 是一个用 Go 实现的轻量级终端 Coding Agent，支持流式输出、OpenAI-compatible tool calling、权限审批、MCP 扩展、Skills 流程注入和 JSONL 会话追踪。

## 2. 推荐阅读顺序

不要从 TUI 开始读。建议按这个顺序：

1. `internal/types/types.go`
   先看核心数据结构：`Message`、`ToolCall`、`ToolDefinition`、`SessionEvent`。

2. `internal/agent/agent.go`
   看 Agent Loop 怎么调度模型和工具。

3. `internal/tools/tools.go`
   看工具注册表：模型看到的是 tool schema，agent 执行的是 executor。

4. `internal/tools/file.go` 和 `internal/tools/shell.go`
   看代码读写、diff、workspace 安全边界、shell 执行。

5. `internal/permission/permission.go`
   看权限审批和 shell 风险识别。

6. `internal/mcp/mcp.go`
   看 MCP 怎么把外部 server 的工具接进 Tool Registry。

7. `internal/skills/skills.go`
   看 skill 怎么加载、匹配和注入 prompt。

8. `internal/session/session.go`
   看 JSONL trace 怎么保存和恢复。

9. `internal/compact/compact.go`
   看 token 估算和上下文压缩。

TUI 的 `internal/tui/tui.go` 只需要看三处：

- 启动时注册工具和初始化 agent。
- 权限审批回调怎么传给 agent。
- MCP 工具怎么注册成 executor。

## 3. Agent Loop 核心逻辑

核心文件：`internal/agent/agent.go`

流程如下：

1. 用户输入追加成 `user` message。
2. agent 估算 token，如果超过阈值就 compact。
3. agent 把 messages 和 tools 发给 LLM。
4. 如果 LLM 返回普通文本，追加 assistant message，结束。
5. 如果 LLM 返回 tool calls：
   - 追加 assistant message，里面带 tool_calls。
   - 对每个 tool call 做权限审批。
   - 解析 JSON 参数。
   - 从 Tool Registry 找 executor 并执行。
   - 把结果追加为 `tool` message。
6. 带着 tool result 进入下一轮 LLM 调用。

面试时重点说：

> Tool calling 不是一次调用就结束，而是 LLM -> tool -> tool result -> LLM 的多轮闭环。GoCoder 保证 assistant(tool_calls) 和 tool(result) 成对存在，这样上下文压缩和 session 恢复不会破坏语义。

## 4. Tool Registry 是什么

核心文件：`internal/tools/tools.go`

Tool Registry 做两件事：

- 给模型看的：`ToolDefinition`，也就是 OpenAI function schema。
- 给 agent 执行的：`ToolExecutor`，也就是 Go 函数。

简化理解：

```go
type RegisteredTool struct {
    Definition       ToolDefinition
    Executor         ToolExecutor
    RequiresApproval bool
    RiskLevel        string
}
```

模型只知道工具名字、描述、参数 schema。真正执行由 GoCoder 根据工具名找到 executor。

## 5. 文件工具和安全边界

核心文件：

- `internal/tools/file.go`
- `internal/tools/path.go`

内置文件工具包括：

- `read_file`
- `write_file`
- `edit_file`
- `list_directory`
- `grep`
- `read_tool_result`

关键设计：

- 所有路径都走 `resolveWorkspacePath`。
- 相对路径会拼到 workspace。
- 绝对路径也必须落在 workspace 内。
- `../../` 越界会被拒绝。
- 写文件和编辑文件会生成 diff。
- `edit_file` 要求 `old_string` 精确且唯一，避免误改多处。

面试时可以说：

> 我没有让模型直接随便写路径，而是在工具层做 workspace boundary。所有文件操作都会把目标路径解析成绝对路径，再检查它是否仍在项目目录下。

## 6. 权限审批模型

核心文件：`internal/permission/permission.go`

工具风险分级：

- `readonly`：自动允许，比如 read_file、grep。
- `write`：需要确认，比如 write_file、edit_file。
- `shell`：需要确认，比如 run_command。
- `mcp`：需要确认，因为外部工具能力不完全可控。

shell 命令会额外分析风险：

- `rm` / `rmdir`：destructive
- `sudo`：privileged
- `git reset` / `git clean`：git-history
- `curl` / `wget` / `npm` / `npx`：network
- `curl | sh`：pipe-to-shell
- `>` / `>>`：file-write

面试时可以说：

> 权限不是只靠 UI 提醒，而是在 agent 执行工具前统一拦截。即使工具来自 MCP，也会进入同一套审批模型。

## 7. MCP 是什么

MCP 全称 Model Context Protocol。可以把它理解成：

> 一个让 Agent 接入外部工具和数据源的标准协议。

比如一个 MCP server 可以提供：

- filesystem 工具
- memory 工具
- browser 工具
- database 工具
- GitHub 工具

GoCoder 目前实现的是最小完整 MCP 工具调用链：

1. 启动 stdio MCP server，或连接 HTTP MCP server。
2. 发送 `initialize`。
3. 发送 `notifications/initialized`。
4. 发送 `tools/list` 获取工具列表。
5. 把 MCP 工具注册进 Tool Registry。
6. 模型调用 MCP 工具时，GoCoder 转发为 `tools/call`。

核心文件：`internal/mcp/mcp.go`

重要实现点：

- stdio 使用后台 `readLoop`，不会每次请求重建 reader。
- 能跳过 server 输出的启动日志和 notification。
- 请求有超时，避免卡死。
- `params` 为空时发送 `{}`，兼容真实公开 MCP server。
- 工具重名时用 `server__tool` 命名。
- TUI 注册 MCP 工具时绑定 executor，真正能调用外部工具。

## 8. Skills 是什么

Skill 不是工具。Skill 是流程知识。

MCP 和 Skill 的区别：

- MCP：可执行能力，比如读数据库、访问浏览器、查 memory。
- Skill：指导模型怎么做事，比如代码审查流程、Go 发布流程。

核心文件：`internal/skills/skills.go`

Skill 文件示例：

```markdown
---
name: code-review
description: 代码审查流程
keywords: ["review", "审查", "检查代码"]
---

# Code Review Process

1. 查看变更范围
2. 逐文件检查风险
3. 运行测试
4. 按严重程度输出问题
```

GoCoder 支持：

- 统一从 `~/.gocoder/skills` 加载
- `/skill name` 手动激活
- 根据关键词自动匹配
- 注入 system prompt

仓库里的 `config/config.yaml` 只是默认配置模板，首次运行时会写入 `~/.gocoder/config.yaml`。运行时配置和 skills 都统一放在用户目录下，避免项目目录和用户目录互相覆盖。

面试时可以说：

> Skills 不增加执行权限，只改变模型的工作流程。这样 MCP 和 Skills 的职责是分离的：一个管能力，一个管方法论。

## 9. Session / Trace

核心文件：`internal/session/session.go`

GoCoder 用 JSONL 保存事件：

- session_start
- message
- tool_call
- tool_result
- permission
- compact

为什么用 JSONL：

- 追加写简单。
- 崩溃时已有事件不丢。
- 每行都是独立事件，方便 replay。

关键点：

- 普通 message 保存为 message event。
- assistant 里的 tool_calls 保存为 tool_call event。
- tool result 保存为 tool_result event。
- 恢复时再组装回 OpenAI messages。
- `/save` 会把当前 agent messages 追加写入 `~/.gocoder/sessions/session_<id>.jsonl`。
- `/resume` 不带参数会列出最近会话。
- `/resume <id|path>` 会加载 JSONL，恢复 messages 和聊天展示，但不会立刻调用模型；恢复后等用户下一次输入再继续。

面试时可以说：

> 我没有只保存聊天文本，而是保存 agent 的执行轨迹。这样可以恢复上下文，也可以解释 agent 当时为什么调用某个工具。

## 10. 上下文压缩和 Token

核心文件：`internal/compact/compact.go`

Token 统计策略：

1. 优先使用 provider 返回的 usage。
2. 如果 provider 没返回，就本地估算。
3. 中文按约 0.6 token/字估算，英文按约 0.25 token/字符估算。

压缩策略：

- `Snip`：确定性裁剪早期消息。
- `ModelCompact`：调用模型把早期消息总结成结构化摘要。

关键规则：

- assistant(tool_calls) 和对应 tool(result) 不能拆开。
- 最近几轮工具调用优先保留。
- 被裁剪内容会留下 boundary message，提醒模型必要时重新读文件。

面试时可以说：

> 压缩不是简单删除历史，而是先把消息分组，保证 tool_call 和 tool_result 不被切断，否则 OpenAI tool calling 上下文会变得不合法。

## 11. 如何降低幻觉

GoCoder 主要靠四点降低幻觉：

1. 让模型用工具读文件，而不是凭记忆猜代码。
2. 文件工具返回行号，方便定位。
3. 编辑必须基于精确 old_string。
4. 修改后可以运行测试，把失败结果再喂回模型。

面试时可以说：

> Coding agent 不能完全消灭幻觉，但可以把关键事实来源从模型记忆转移到工具读取和测试反馈。

## 12. 你应该重点掌握的代码

面试前重点读这些函数：

- `Agent.Run`
- `Agent.agentLoop`
- `Agent.requestApproval`
- `Registry.Register`
- `Registry.Execute`
- `makeReadFile`
- `makeEditFile`
- `resolveWorkspacePath`
- `AnalyzeShellCommand`
- `Manager.Connect`
- `Client.Initialize`
- `Client.DiscoverTools`
- `Client.CallTool`
- `Manager.CallRegisteredTool`
- `Manager.GetAllTools`
- `skills.Manager.LoadAll`
- `skills.Manager.Match`
- `Session.AppendAgentMessage`
- `EventsToMessages`
- `compact.BuildMessageGroups`
- `compact.Snip`

TUI 不用深背。你只需要知道 TUI 把用户输入交给 agent，把权限审批结果回传给 agent。

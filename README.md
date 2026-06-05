# GoCoder

轻量级终端 AI Coding Agent，兼容 OpenAI API 规范，支持 GPT、DeepSeek 及任何兼容模型。

## 特性

- **Agent 循环引擎** — 多轮 Tool Calling 自动调度，tool_call+result 原子语义单元
- **分级上下文压缩** — Snip(确定性裁剪) + Model Compact(LLM 摘要) + Context Collapse(投影)，三级非破坏性降级
- **Token 感知调度** — Provider usage 优先，多级估算降级，自动触发 compact
- **Event Sourcing 会话** — JSONL 追加式事件流，支持 fork/resume/replay
- **MCP 协议接入** — JSON-RPC 2.0 Client，stdio/HTTP 双模式，动态工具注册
- **Skills 流程知识** — SKILL.md 注入，分离"可执行能力"和"流程知识"
- **权限审批框架** — 分级审批(自动/白名单/确认)，文件 diff 预览，shell 审查
- **冷热分离** — 大工具结果自动落盘，上下文保留 preview
- **零 SDK 依赖** — 纯 `net/http`，无缝切换 OpenAI/DeepSeek/任何兼容服务

## 快速开始

### 安装

```bash
git clone https://github.com/kiritosuki/gocoder.git
cd gocoder
go build -o gocoder .
```

### 配置 API Key

```bash
export GOCODER_API_KEY="sk-..."
```

### 启动

```bash
./gocoder
```

首次运行会自动在 `~/.gocoder/config.yaml` 创建默认配置。

## 使用

```bash
gocoder                 # 启动交互式 TUI
gocoder config show     # 查看当前配置
gocoder config reset    # 重置配置
gocoder version         # 版本信息
```

### TUI 操作

| 按键 | 功能 |
|------|------|
| Enter | 发送消息 |
| Ctrl+S | 保存会话 |
| Ctrl+C/D | 退出 |
| A/D | 权限审批（Allow/Deny）|

### 内置命令

| 命令 | 说明 |
|------|------|
| `/help` | 显示帮助 |
| `/save` | 保存当前会话 |
| `/resume` | 列出最近会话 |
| `/resume <id\|path>` | 恢复指定会话 |
| `/skill <name>` | 激活 Skill |
| `/models` | 列出模型 |
| `/tokens` | Token 用量 |
| `/mcp` | MCP 连接状态 |

## 架构

```
main.go → TUI (Bubble Tea) → Agent Loop → LLM Client (HTTP/SSE)
                                  ↓
                            Tool Registry
                           ├── read_file
                           ├── write_file  ← Permission
                           ├── edit_file   ← Permission
                           ├── list_directory
                           ├── grep
                           ├── run_command ← Permission
                           └── MCP Tools   (动态注册)
                                  ↓
                            Context Manager
                           ├── Token Counting
                           ├── Snip Compact
                           └── Model Compact
```

## 配置

编辑 `~/.gocoder/config.yaml`，修改 `model` 部分即可切换模型：

```yaml
# ~/.gocoder/config.yaml
preferences:
  max_tool_rounds: 15
  compact_threshold: 0.7
  project_dir: .

model:
  name: gpt-4o
  endpoint: https://api.openai.com/v1/chat/completions
  auth_env_var: GOCODER_API_KEY
  max_tokens: 128000

mcp_servers: []
```

## Skills

在 `~/.gocoder/skills/` 中放置 Skill markdown 文件：

```markdown
---
name: go-release
description: Go 项目发布流程。当用户提到"发布"、"release"时使用。
---

# Go Release Process

1. 确认测试通过: `go test ./...`
2. 更新 CHANGELOG.md
3. 创建 tag: `git tag -a vX.Y.Z`
4. 推送: `git push --tags`
```

## 技术栈

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI 框架
- [Glamour](https://github.com/charmbracelet/glamour) — Markdown 渲染
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — 终端样式
- [Cobra](https://github.com/spf13/cobra) — CLI
- [go-diff](https://github.com/sergi/go-diff) — Diff 生成
- 纯 `net/http` — LLM API 调用

## 学习和面试准备

- [项目阅读指南](docs/PROJECT_GUIDE.md) — 按核心模块讲解 Agent Loop、Tools、MCP、Skills、Session、Compact。
- [面试 QA](docs/INTERVIEW_QA.md) — 用面试官问答形式准备项目介绍、亮点、设计取舍和关键概念。
- [保姆级教学](docs/BABY_GUIDE.md) — 超级详细的教学文档，面向小白。

## License

MIT

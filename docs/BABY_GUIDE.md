# GoCoder 保姆级教学：从零理解一个 AI Coding Agent

> **面向读者**：完全没有接触过 Agent 开发的程序员。只要你懂 Go 基础语法，这篇文档就能带你一行一行读懂整个项目。
>
> **阅读建议**：按顺序读，每一章都建立在前一章的基础上。代码摘取时省略了不重要的 UI 细节，聚焦核心业务逻辑。

---

## 目录

- [第 0 章：前言——什么是 Coding Agent？](#第0章)
- [第 1 章：项目鸟瞰——GoCoder 是什么](#第1章)
- [第 2 章：类型系统——一切数据结构的基石](#第2章)
- [第 3 章：配置管理——程序怎么知道自己该干什么](#第3章)
- [第 4 章：LLM 客户端——怎么跟大模型说话](#第4章)
- [第 5 章：Agent 循环——整个项目的心脏](#第5章)
- [第 6 章：工具系统——Agent 的手和脚](#第6章)
- [第 7 章：路径安全——为什么不能让 AI 乱跑](#第7章)
- [第 8 章：权限审批——谁同意 AI 动的？](#第8章)
- [第 9 章：MCP 协议——接入外部工具的标准](#第9章)
- [第 10 章：Skills 系统——教 AI 怎么做事](#第10章)
- [第 11 章：会话管理——记住发生过什么](#第11章)
- [第 12 章：上下文压缩——上下文太长怎么办](#第12章)
- [第 13 章：TUI 界面——用户怎么跟 Agent 交互](#第13章)
- [第 14 章：启动流程——从头到尾串一遍](#第14章)
- [第 15 章：面试指南——怎么跟面试官讲这个项目](#第15章)

---

## <a id="第0章"></a>第 0 章：前言——什么是 Coding Agent？

### 0.1 从一个简单问题开始

假设你对 ChatGPT 说："帮我写一个 Go 的 HTTP 服务器"。ChatGPT 会给你一段代码。你复制粘贴到文件里，运行，发现有 bug，再让它改。

这中间，**你**是那个执行者——复制代码、创建文件、运行测试、把错误贴回去。

Coding Agent 的目标是：**让 AI 自己完成这个闭环**。你说一句话，它自己读文件、写代码、运行测试、修 bug，你只需要看着。

### 0.2 Agent 和普通聊天机器人区别在哪？

| | 普通聊天机器人 | Coding Agent |
|---|---|---|
| 交互次数 | 一问一答 | 多轮自动循环 |
| 能做什么 | 生成文本 | 调用工具（读文件、写文件、执行命令） |
| 谁执行 | 你 | 它 |
| 上下文 | 你手动管理 | 它自动管理 |

打个比方：聊天机器人是"给你建议的顾问"，Coding Agent 是"帮你干活的员工"。

### 0.3 Agent 的核心机制：Tool Calling

Tool Calling 是让大模型调用工具的关键机制。流程是这样的：

```
你: "帮我读一下 main.go"
    ↓
Agent 把这句话发给 LLM
    ↓
LLM 思考："我需要用 read_file 工具，参数是 path='main.go'"
    ↓
LLM 返回的不是文字，而是一个 tool_call:
    { name: "read_file", arguments: {path: "main.go"} }
    ↓
Agent 收到 tool_call，执行真正的 read_file("main.go")
    ↓
Agent 把结果发回给 LLM: "文件内容是 package main..."
    ↓
LLM 看到结果，给你生成回答: "main.go 的内容是..."
```

这就是整个 Agent 的核心循环——**LLM 负责决策"该调什么工具"，Agent 负责执行，执行结果再喂回 LLM**。

### 0.4 GoCoder 在这个生态里的位置

市面上的 Coding Agent 很多：Claude Code、Cursor、GitHub Copilot、Aider。

GoCoder 的定位是：**用最少的依赖、最清晰的代码，实现一个完整的 Coding Agent 核心**。它不去做复杂的功能，而是让你能看懂每一行代码在干什么。

约 5000 行核心 Go 代码，零 SDK 依赖，纯 `net/http` 调 API。读完这个项目，你就真正理解了 Agent 的底层原理。

---

## <a id="第1章"></a>第 1 章：项目鸟瞰——GoCoder 是什么

### 1.1 一句话概括

GoCoder 是一个用 Go 语言写的**终端 AI 编程助手**。你在终端里告诉它要做什么，它自动调用大模型、读写文件、执行命令，完成编程任务。

### 1.2 核心能力清单

| 能力 | 说明 |
|------|------|
| Agent 循环 | 多轮自动调用 LLM + 工具，直到任务完成 |
| 文件工具 | 读文件、写文件、编辑文件、列目录、搜索 |
| Shell 工具 | 执行终端命令（需要审批） |
| 权限审批 | 写操作、shell 命令必须用户确认 |
| MCP 扩展 | 可以接外部的 MCP 工具服务器 |
| Skills 注入 | 把"代码审查流程"等知识注入给 AI |
| 会话保存 | 用 JSONL 记录整个对话过程，支持恢复 |
| 上下文压缩 | 对话太长时自动裁剪/摘要早期内容 |

### 1.3 目录结构一览

```
GoCoder/
├── main.go                        # 程序入口，CLI 命令
├── config/
│   ├── config.yaml                # 默认配置（编译进二进制）
│   └── config.go                  # 配置加载/保存逻辑
├── internal/
│   ├── types/
│   │   └── types.go               # 全部数据结构定义
│   ├── agent/
│   │   └── agent.go               # Agent 循环引擎（心脏）
│   ├── llm/
│   │   └── client.go              # HTTP 客户端，调 OpenAI API
│   ├── tools/
│   │   ├── tools.go               # 工具注册表
│   │   ├── file.go                # 文件读写工具
│   │   ├── shell.go               # Shell 命令工具
│   │   └── path.go                # 路径安全检查
│   ├── permission/
│   │   └── permission.go          # 权限审批框架
│   ├── mcp/
│   │   └── mcp.go                 # MCP 协议客户端
│   ├── skills/
│   │   └── skills.go              # Skills 流程知识
│   ├── session/
│   │   └── session.go             # JSONL 会话存储
│   ├── compact/
│   │   └── compact.go             # 上下文压缩和 token 估算
│   └── tui/
│       ├── tui.go                 # 终端界面（Bubble Tea）
│       └── styles.go              # 界面颜色定义
├── docs/
│   ├── BABY_GUIDE.md              # 你正在看的这份文档
│   ├── PROJECT_GUIDE.md           # 项目阅读指南
│   └── INTERVIEW_QA.md            # 面试问答
└── test_mcp_server.py             # MCP 测试用 Python 服务器
```

### 1.4 数据流全景图

```
                      ┌─────────────────────┐
                      │       用户输入        │
                      └─────────┬───────────┘
                                │
                      ┌─────────▼───────────┐
                      │      TUI 界面        │
                      │  (Bubble Tea 框架)   │
                      └─────────┬───────────┘
                                │ user message
                      ┌─────────▼───────────┐
                      │    Agent 循环引擎     │  ←── 核心
                      │   agentLoop()       │
                      │                     │
                      │  1. token 检查       │
                      │  2. 调 LLM           │
                      │  3. 有 tool_call?    │──→ 执行工具 ──→ 回到步骤1
                      │  4. 没有? → 返回文本  │
                      └─────────┬───────────┘
                                │
                ┌───────────────┼───────────────┐
                │               │               │
        ┌───────▼──────┐ ┌─────▼─────┐ ┌──────▼──────┐
        │   Tool       │ │ Permission│ │   Context   │
        │   Registry   │ │  Runner   │ │   Manager   │
        │ (内置+MCP)   │ │ (审批框架) │ │ (compact)   │
        └──────────────┘ └───────────┘ └─────────────┘
```

---

## <a id="第2章"></a>第 2 章：类型系统——一切数据结构的基石

> **文件**：`internal/types/types.go`
>
> **为什么先看这个**：就像盖房子要先看图纸，理解代码前要先理解数据结构。这个文件定义了整个项目所有模块共用的类型，是项目的"词汇表"。

### 2.1 OpenAI 消息格式

大模型 API 的核心是**消息（Message）**。跟人聊天一样，你跟 AI 的对话就是一组消息的列表。

```go
// Message 对话消息，兼容 OpenAI Chat Completion 格式
type Message struct {
    Role       string     `json:"role"`                   // 谁说的：system/user/assistant/tool
    Content    string     `json:"content,omitempty"`      // 说了什么
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // AI 想调什么工具
    ToolCallID string     `json:"tool_call_id,omitempty"` // 工具结果关联哪个调用
    Name       string     `json:"name,omitempty"`         // 工具名称（tool 角色时可选）
}
```

**四种角色（Role）的含义：**

| Role | 谁 | 什么时候用 |
|------|-----|----------|
| `system` | 系统指令 | 在一开始告诉 AI "你是一个 Go 编程助手，用中文回答" |
| `user` | 用户 | 你说的话 |
| `assistant` | AI | AI 的回答（可能不包含文本而包含 tool_calls） |
| `tool` | 工具结果 | 工具执行完返回的内容，需要关联到对应的 tool_call |

**一个完整的对话例子（这就是发给 API 的 messages 数组）：**

```
[system]   "你是一个 Go 编程助手..."
[user]     "帮我读一下 main.go"
[assistant] { tool_calls: [{ function: { name: "read_file", arguments: {path:"main.go"} } }] }
[tool]     "第1行: package main\n第2行: import (\n..."
[assistant] "main.go 的内容如上，你需要修改什么？"
```

**核心理解**：`Content` 和 `ToolCalls` 是互斥的吗？不是。某些模型在返回 tool_calls 的同时也会有一段文字（比如"我来帮你读文件"）。所以 assistant 消息可能同时有 Content 和 ToolCalls。

### 2.2 ToolCall 结构——AI 怎么告诉你它想调什么工具

```go
type ToolCall struct {
    Index    int          `json:"index,omitempty"` // 流式响应中的序号
    ID       string       `json:"id"`              // 唯一标识，用于关联 tool result
    Type     string       `json:"type"`            // 固定值 "function"
    Function FunctionCall `json:"function"`        // 函数名和参数
}

type FunctionCall struct {
    Name      string `json:"name"`      // 工具名，比如 "read_file"
    Arguments string `json:"arguments"` // JSON 编码的参数，注意是字符串不是对象！
}
```

**关键概念**：`Arguments` 是一个 **JSON 字符串**，不是 map 对象。比如内容是 `"{\"path\":\"main.go\",\"offset\":10}"`。

**为什么是字符串？** 因为流式传输时（SSE），参数是分片到达的：
```
chunk1: {"index":0, "function": {"name": "read_", "arguments": "file"}}
chunk2: {"index":0, "function": {"arguments": "{\"path\":"}}
chunk3: {"index":0, "function": {"arguments": "\"main.go\"}"}}
```
如果是 map 对象就无法拼接，而字符串可以简单做 `+=`。

### 2.3 ToolDefinition——给 LLM 看的"工具说明书"

```go
type ToolDefinition struct {
    Type     string         `json:"type"` // 固定 "function"
    Function FunctionSchema `json:"function"`
}

type FunctionSchema struct {
    Name        string         `json:"name"`        // 工具名
    Description string         `json:"description"` // 一句话描述工具功能
    Parameters  map[string]any `json:"parameters"`  // JSON Schema 定义参数
}
```

这是发给 LLM 的。以 `read_file` 为例，LLM 会看到：

```json
{
    "type": "function",
    "function": {
        "name": "read_file",
        "description": "读取文件内容，返回带行号的文本。支持指定行范围。",
        "parameters": {
            "type": "object",
            "properties": {
                "path":   { "type": "string",  "description": "文件路径（相对于项目目录）" },
                "offset": { "type": "integer", "description": "起始行号（1-based，可选）" },
                "limit":  { "type": "integer", "description": "读取行数（可选，默认全部）" }
            },
            "required": ["path"]
        }
    }
}
```

LLM 看到这个 JSON Schema 后就知道：有一个叫 `read_file` 的工具，需要传一个 `path` 参数，可以选传 `offset` 和 `limit`。它不会执行这个工具——它只是"点菜"，真正"做菜"的是 Agent。

**面试考点**：
> ToolDefinition 是给 LLM 看的菜单，模型根据它决定调什么工具、传什么参数。真正的执行逻辑在 GoCoder 手里。这种"声明和执行分离"保证了安全——LLM 不能执行任意代码。

### 2.4 RegisteredTool——工具的完整定义

```go
type RegisteredTool struct {
    Definition       ToolDefinition  // 给 LLM 看的说明书
    Executor         ToolExecutor    // 真正执行的 Go 函数
    RequiresApproval bool            // 是否需要用户审批
    RiskLevel        string          // 风险等级："readonly" / "write" / "shell"
}

// ToolExecutor 是工具执行函数的统一签名
type ToolExecutor func(ctx ToolContext, input map[string]any) (string, error)

// ToolContext 提供工具执行的上下文信息
type ToolContext struct {
    WorkDir   string // 当前工作目录
    SessionID string // 当前会话 ID
}
```

**三层信息**：
1. `Definition`：给 LLM 的
2. `Executor`：给 GoCoder 执行引擎的
3. `RequiresApproval` + `RiskLevel`：给权限系统的

### 2.5 Agent 状态机

```go
type AgentState int

const (
    StateIdle             AgentState = iota // 等待用户输入
    StateStreaming                          // LLM 正在流式返回
    StateExecuting                          // 正在执行工具
    StateAwaitingPermission                 // 等待用户审批
    StateError                              // 错误状态
)
```

Agent 在任何时刻只处于一种状态。这个状态机决定了 UI 显示什么、能否接收用户输入：

```
Idle → (用户输入) → Streaming → (有 tool_call) → Executing
                                    ↘ (需要审批) → AwaitingPermission
                                                       ↓ 批准
                                                  Executing
                                    ↓ 执行完
                                 Streaming (LLM 看到 tool_result 继续)
                                    ↓ 返回文字
                                  Idle
```

### 2.6 Session Event——对话的完整记录（事件溯源）

```go
type SessionEvent struct {
    Type      EventType  `json:"type"`      // 事件类型
    Timestamp int64      `json:"ts"`        // Unix 时间戳
    Data      any        `json:"data"`      // 具体事件载荷（不同类型有不同结构）
}
```

支持的事件类型：

| 事件类型 | 含义 | 数据载荷 |
|---------|------|---------|
| `session_start` | 会话开始 | session_id, version |
| `message` | 用户/AI 消息 | role, content |
| `tool_call` | AI 请求调用工具 | call_id, tool_name, arguments |
| `tool_result` | 工具执行结果 | call_id, output, duration_ms |
| `permission` | 权限审批记录 | call_id, tool_name, decision |
| `compact` | 上下文压缩记录 | method, collapsed_span, summary |

**为什么用 JSONL（每行一个 JSON）**：
- 追加写入，程序崩溃时已写入的事件不丢
- 每行独立，方便逐行 replay
- 不需要一次性把整个会话序列化成大 JSON

### 2.7 配置类型

```go
type AppConfig struct {
    Model       ModelConfig          `yaml:"model"`
    Preferences Preferences          `yaml:"preferences"`
    MCPServers  map[string]MCPConfig `yaml:"mcp_servers"`
    Version     string               `yaml:"config_format_version"`
}

type ModelConfig struct {
    Name       string `yaml:"name"`         // 模型名，如 gpt-4o, deepseek-chat
    Endpoint   string `yaml:"endpoint"`     // API 地址
    AuthEnvVar string `yaml:"auth_env_var"` // API Key 环境变量名
    MaxTokens  int    `yaml:"max_tokens"`   // 模型上下文窗口大小
}

type Preferences struct {
    MaxToolRounds    int     `yaml:"max_tool_rounds"`    // 最大工具调用轮数，默认 15
    CompactThreshold float64 `yaml:"compact_threshold"`  // 触发 compact 的阈值，默认 0.7
    ProjectDir       string  `yaml:"project_dir"`        // 项目工作目录
}
```

### 2.8 本章小结

这章我们建立了项目的"词汇表"。核心概念回顾：

1. **Message**：对话的基本单元，四种角色 system/user/assistant/tool
2. **ToolCall**：LLM 的"点菜单"——告诉 Agent 要调什么工具，Arguments 是 JSON 字符串（因为流式分片）
3. **ToolDefinition**：工具的"菜单描述"，给 LLM 看的 JSON Schema
4. **RegisteredTool**：工具的完整定义，包含真正的执行函数、风险等级、是否需要审批
5. **Agent 状态机**：Idle → Streaming → Executing/AwaitingPermission → 循环
6. **SessionEvent**：对话记录的原子单位，用 JSONL 存储

---

## <a id="第3章"></a>第 3 章：配置管理——程序怎么知道自己该干什么

> **文件**：`config/config.go`、`config/config.yaml`

### 3.1 配置放在哪

GoCoder 的配置和数据统一放在用户目录下 `~/.gocoder/`：

```go
const (
    configRelPath       = ".gocoder/config.yaml"         // 配置
    backupConfigRelPath = ".gocoder/.backup-config.yaml" // 备份
    toolResultsDir      = ".gocoder/tool_results"        // 大工具输出落盘
    sessionsDir         = ".gocoder/sessions"            // 会话记录
    skillsDir           = ".gocoder/skills"              // Skills 目录
)
```

**为什么不放在项目目录**：项目目录是 AI 操作的目标，放配置可能被 AI 误改或误删。放在用户目录下更安全。

### 3.2 内嵌默认配置（关键 Go 特性：embed）

```go
//go:embed config.yaml
var embeddedConfig []byte
```

`//go:embed` 是 Go 1.16 引入的特性。它会在**编译时**把 `config.yaml` 的内容嵌入到二进制文件中。运行时不需要去找配置文件——首次运行时自动把默认配置写出来。

**面试话术**：
> 我用 Go 的 embed 特性把默认配置编译进二进制，用户首次运行时自动创建 `~/.gocoder/config.yaml`。这样分发一个二进制就够了，不需要附带配置文件。

### 3.3 配置加载流程

```go
func LoadAppConfig() (types.AppConfig, error) {
    fullPath, err := fullPath(configRelPath)  // 得到 ~/.gocoder/config.yaml
    if _, err := os.Stat(fullPath); os.IsNotExist(err) {
        return createDefaultConfig(configRelPath)  // 不存在 → 用内嵌默认值创建
    }
    return loadExistingConfig(configRelPath)        // 存在 → 读取并解析 YAML
}
```

逻辑很简单：文件在就加载，不在就用默认值创建。

### 3.4 安全性：环境变量注入

```go
// main.go 中的关键代码
apiKey := os.Getenv(modelConfig.AuthEnvVar)  // 从环境变量读取真正的 Key
if apiKey == "" {
    printAPIKeyNotSet(modelConfig)  // 没设置 → 友好提示并退出
    os.Exit(1)
}
modelConfig.AuthEnvVar = apiKey  // 把实际 Key 值存到这个字段里（字段名有误导性）
```

**重要设计细节**：配置文件只存环境变量**名称**（如 `GOCODER_API_KEY`），不存实际的 Key。实际 Key 从环境变量读取后**复用** `AuthEnvVar` 字段传递给 LLM 客户端。这样永远不会把 Key 明文写入配置文件。

### 3.5 默认配置内容

```yaml
# ~/.gocoder/config.yaml
preferences:
  max_tool_rounds: 15       # 最多 15 轮工具调用，防止死循环
  compact_threshold: 0.7    # token 用量到 70% 时触发 compact
  project_dir: .            # 默认当前目录

model:
  name: gpt-4o              # 默认模型
  endpoint: https://api.openai.com/v1/chat/completions
  auth_env_var: GOCODER_API_KEY
  max_tokens: 128000        # GPT-4o 的上下文窗口

mcp_servers: {}             # 默认不连 MCP
```

---

## <a id="第4章"></a>第 4 章：LLM 客户端——怎么跟大模型说话

> **文件**：`internal/llm/client.go`
>
> **前置知识**：你需要了解 HTTP POST、JSON、SSE（Server-Sent Events）。如果不知道 SSE，可以先理解成"服务端一直推送数据，客户端逐个接收，像水管流水一样"。

### 4.1 背景：OpenAI Chat Completions API

大模型的 HTTP API 长这样：

```
POST https://api.openai.com/v1/chat/completions
Authorization: Bearer sk-xxxx
Content-Type: application/json

{
    "model": "gpt-4o",
    "messages": [
        {"role": "system", "content": "你是一个编程助手"},
        {"role": "user",   "content": "帮我读 main.go"}
    ],
    "tools": [
        {"type": "function", "function": {"name": "read_file", ...}}
    ],
    "stream": true
}
```

如果 `stream: true`，API 不会一次性返回完整 JSON。而是返回 SSE 流：

```
data: {"choices":[{"delta":{"content":"我来"}}]}

data: {"choices":[{"delta":{"content":"帮你"}}]}

data: {"choices":[{"delta":{"content":"读"}}]}

data: [DONE]
```

这就是**流式输出（streaming）**——服务端不断推送小片段，GoCoder 逐个接收并展示，实现"打字机效果"。

如果 `stream: false`，API 一次性返回：

```json
{
    "choices": [{
        "message": {
            "role": "assistant",
            "content": "我来帮你读"
        }
    }]
}
```

### 4.2 Client 结构体

```go
type Client struct {
    config         types.ModelConfig      // 模型配置：endpoint、key 等
    httpClient     *http.Client           // HTTP 客户端，超时 180 秒
    streamCallback types.StreamCallback   // 流式输出回调 → 通知 UI 更新
    messages       []types.Message        // 消息历史引用（由外部 Agent 管理）
    lastUsage      *types.Usage           // 最后一次请求的 token 用量
}
```

**为什么 HTTP 超时是 180 秒**：大模型生成一次回答可能要 30-120 秒，工具执行完再回来又要一段时间。180 秒给了充足的缓冲。

### 4.3 一次 Chat 调用的完整流程

```go
func (c *Client) Chat(messages []types.Message, tools []types.ToolDefinition) (*ChatResult, error) {
    req := types.ChatRequest{
        Model:       c.config.Name,
        Messages:    messages,
        Tools:       tools,           // 把可用工具列表告诉 LLM
        MaxTokens:   4096,            // 单次回复最多 4096 token
        Temperature: 0,               // 温度 0 = 确定性输出，编程场景不需要创意
        Stream:      true,            // 始终使用流式
    }

    result, err := c.chatWithRetry(req)  // 带重试
    return result, nil
}
```

**两个关键设计决策**：

1. **Temperature = 0**：Temperature 控制输出的随机性（0 = 确定性，1 = 更有创意）。编程场景需要准确、确定的结果，所以设为 0。

2. **MaxTokens = 4096**：这是**单次回复**的最大 token 数，不是整个上下文的。模型一次回答通常不需要太长，设太大了浪费 token。

### 4.4 重试机制（指数退避）

```go
const (
    maxRetries     = 3
    retryBaseDelay = 1 * time.Second
)

func (c *Client) chatWithRetry(req types.ChatRequest) (*ChatResult, error) {
    var lastErr error
    for attempt := 0; attempt <= maxRetries; attempt++ {
        if attempt > 0 {
            delay := retryBaseDelay * time.Duration(1<<(attempt-1))
            // attempt=1 → 等 1s
            // attempt=2 → 等 2s
            // attempt=3 → 等 4s
            time.Sleep(delay)
        }

        result, err := c.streamChat(req)
        if err == nil {
            return result, nil
        }
        lastErr = err

        // 只重试服务器错误和限流（500/502/503/429）
        if strings.Contains(err.Error(), "500") || ... {
            continue
        }
        // 客户端错误（400/401/403）不重试——重试也没用
        if strings.Contains(err.Error(), "400") || ... {
            return nil, err
        }
    }
    return nil, fmt.Errorf("请求失败（已重试%d次）: %w", maxRetries, lastErr)
}
```

**面试话术**：
> LLM 客户端实现了指数退避重试：服务器错误（5xx）和限流（429）重试（最多 3 次），客户端错误（4xx，比如 API Key 不对）直接返回不重试。退避间隔是 1s → 2s → 4s。这样在网络抖动时不会直接失败，同时也不会无限重试。

### 4.5 SSE 流式解析——客户端最核心的代码

```go
func (c *Client) processStream(body io.Reader) (*ChatResult, error) {
    result := &ChatResult{
        ToolCalls: []types.ToolCall{},
    }

    scanner := bufio.NewScanner(body)
    scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 每行最多 1MB

    toolCallAccum := map[int]*types.ToolCall{} // 关键：按 index 累积 tool_call 片段

    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())

        if line == ""                         { continue }  // 空行 → 跳过
        if line == "data: [DONE]"             { break }      // 流结束标志
        if !strings.HasPrefix(line, "data:")  { continue }   // 不是数据行

        payload := strings.TrimPrefix(line, "data: ")
        payload = strings.TrimPrefix(payload, "data:")  // 兼容不同实现（有无空格）

        var chunk types.ChatStreamChunk
        if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
            continue // 解析失败的行跳过（可能是日志或心跳）
        }

        // 收集 usage（某些 provider 在最后一个 chunk 返回）
        if chunk.Usage != nil {
            result.Usage = chunk.Usage
        }

        if len(chunk.Choices) == 0 { continue }
        choice := chunk.Choices[0]

        // ── 累积文本内容 ──
        if choice.Delta.Content != "" {
            result.Content += choice.Delta.Content     // 拼接到完整回答
            if c.streamCallback != nil {
                c.streamCallback(result.Content, false, nil)  // 通知 UI 更新！
            }
        }

        // ── 累积 tool_calls（关键！）──
        for _, tc := range choice.Delta.ToolCalls {
            idx := tc.Index
            if _, ok := toolCallAccum[idx]; !ok {
                // 第一次出现 → 创建
                toolCallAccum[idx] = &types.ToolCall{
                    ID:   tc.ID,
                    Type: "function",
                    Function: types.FunctionCall{
                        Name: tc.Function.Name,
                    },
                }
            }
            // 更新 ID 和 Name（后续 chunk 可能覆盖）
            if tc.ID != "" { toolCallAccum[idx].ID = tc.ID }
            if tc.Function.Name != "" {
                toolCallAccum[idx].Function.Name = tc.Function.Name
            }
            // 累积参数——字符串拼接！
            toolCallAccum[idx].Function.Arguments += tc.Function.Arguments
        }
    }

    // 错误检查
    if err := scanner.Err(); err != nil {
        return nil, fmt.Errorf("读取流失败: %w", err)
    }

    // 空响应检测
    if result.Content == "" && len(toolCallAccum) == 0 {
        return nil, fmt.Errorf("空响应: 模型未返回任何内容")
    }

    // 按 index 顺序收集 tool_calls
    for i := 0; i < len(toolCallAccum); i++ {
        if tc, ok := toolCallAccum[i]; ok {
            result.ToolCalls = append(result.ToolCalls, *tc)
        }
    }

    // 流结束通知
    if c.streamCallback != nil {
        c.streamCallback(result.Content, true, nil)
    }

    return result, nil
}
```

**逐行精讲**：

1. **`scanner.Buffer(...)`**：设置每行最大 1MB，防止 SSE 的某一行特别大撑爆内存。

2. **`toolCallAccum` 是 map[int]\*ToolCall**：key 是 Index。一个 LLM 响应中可能有多个 tool_call（比如 AI 同时想读两个文件），Index 区分是哪个 tool_call 的片段。

3. **文本增量累积**：`result.Content += choice.Delta.Content`。每个 chunk 只带很少的文本，要全部拼起来。同时通过 `streamCallback` 通知 UI——这就是打印效果的来源。

4. **ToolCall 分片累积（关键理解！）**：
   ```
   chunk1: index=0, id="call_123", function.name="read_", function.arguments="file"
   chunk2: index=0, function.arguments="{\"path\":\"main.go\"}"
   ─────────────────────────────────────────────────────
   最终结果: index=0, id="call_123", name="read_file",
            arguments="file{\"path\":\"main.go\"}"
   
   等等，name 是 "read_file" 因为后续 chunk 覆盖了前半部分！
   实际中 arguments 不会和 name 混在一起，这个例子只是示意分片的概念。
   ```
   
   实际上，name 和 arguments 是分开的字段。流式传输中：
   - 第一个 chunk 可能给出 name 和 arguments 的第一段
   - 后续 chunk 继续给 arguments 的后续段（用 `+=` 拼接）

5. **`"data: "` vs `"data:"`**：OpenAI 规范是 `data: `（有空格），但有些代理可能有差异。所以两种都尝试去掉。

### 4.6 ChatCompact——不带工具的简单请求

```go
func (c *Client) ChatCompact(messages []types.Message, prompt string) (string, error) {
    req := types.ChatRequest{
        Model:       c.config.Name,
        Messages:    compactMessages,
        MaxTokens:   1024,       // 摘要不需要很长
        Temperature: 0,
        Stream:      false,      // 非流式，简单请求不需要流式
    }
    // ... 发 HTTP POST，解析 JSON 响应，返回 Choices[0].Message.Content
}
```

和 Chat 的三个区别：
- 不带 Tools（不需要工具调用）
- 非流式（Stream=false，因为摘要很短不需要打字机效果）
- MaxTokens 较小（1024，摘要不需要很长）

---

## <a id="第5章"></a>第 5 章：Agent 循环——整个项目的心脏

> **文件**：`internal/agent/agent.go`
>
> **这是整个项目最重要的文件**。理解了 agent.go，你就理解了 Coding Agent 的本质。后续面试中 90% 的问题都会围绕这一章展开。

### 5.1 Agent 结构体

```go
type Agent struct {
    client       *llm.Client          // LLM 客户端
    registry     *tools.Registry      // 工具注册表（"工具箱"）
    perm         *permission.Runner   // 权限审批器

    cfg          types.AgentConfig    // 运行时配置

    messages     []types.Message      // 消息历史（Agent 的"记忆"）
    systemPrompt string               // 系统提示词

    diskOutputs  map[string]string    // 工具输出落盘路径记录

    // 回调函数（由 TUI 层注入）
    streamCB     types.StreamCallback     // 流式内容回调
    toolCB       types.ToolStatusCallback // 工具状态回调
    permCB       func(req types.PermissionRequest) types.PermissionDecision // 权限回调

    tokenTotal   int   // 当前 token 估算
    tokenLimit   int   // 模型窗口上限
}
```

**核心理解**：
- `messages` 是整个 Agent 的"记忆"——LLM 能看到的所有上下文
- `registry` 是 Agent 的"工具箱"——LLM 可以请求调用的工具
- `perm` 是"安全闸"——写文件和执行命令前必须通过

### 5.2 Agent 创建和初始化

```go
func New(client *llm.Client, registry *tools.Registry, cfg types.AgentConfig) *Agent {
    // 填充默认值（防止用户配置漏了某项）
    if cfg.MaxToolRounds == 0      { cfg.MaxToolRounds = 15 }
    if cfg.CompactThreshold == 0   { cfg.CompactThreshold = 0.7 }
    if cfg.ForceSnipThreshold == 0 { cfg.ForceSnipThreshold = 0.9 }
    if cfg.MaxOutputPreview == 0   { cfg.MaxOutputPreview = 2000 }
    if cfg.MaxOutputDisk == 0      { cfg.MaxOutputDisk = 5000 }
    if cfg.ModelMaxTokens == 0     { cfg.ModelMaxTokens = 128000 }

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
```

**注意**：默认值是安全兜底。比如 `MaxToolRounds=15` 防止无限循环，`CompactThreshold=0.7` 及时触发压缩。

### 5.3 依赖反转：回调注入

```go
func (a *Agent) SetCallbacks(
    streamCB types.StreamCallback,    // 通知 UI: "LLM 说了新内容"
    toolCB   types.ToolStatusCallback, // 通知 UI: "正在执行 read_file..."
    permCB   func(req types.PermissionRequest) types.PermissionDecision, // 问用户: "允许吗？"
) {
    a.streamCB = streamCB
    a.toolCB = toolCB
    a.permCB = permCB
    a.client.SetStreamCallback(streamCB)  // 同时传给 LLM 客户端
}
```

这是**依赖反转**的典范。Agent 不知道 UI 是什么（TUI？Web？CLI？），只管调用回调接口。UI 层通过 `SetCallbacks` 注入自己的实现。

### 5.4 Agent 入口：Run 方法

```go
func (a *Agent) Run(userInput string) (string, error) {
    // 第一步：把用户输入追加为一条 user 消息
    a.messages = append(a.messages, types.Message{
        Role:    "user",
        Content: userInput,
    })

    // 第二步：进入 Agent Loop 核心
    return a.agentLoop()
}
```

超级简单！就是把用户输入放进消息列表，然后启动循环。

### 5.5 Agent Loop 核心——逐行精读

这是整个项目最重要的函数。一共约 100 行，我们逐段讲解。

```go
func (a *Agent) agentLoop() (string, error) {
    // 外层 for：工具调用轮数控制
    for round := 0; round < a.cfg.MaxToolRounds; round++ {
```

**首先，有循环上限**。为什么需要？因为不能让 Agent 无限调用工具。如果模型陷入某种循环（比如反复读同一个文件），`MaxToolRounds`（默认 15）会强制终止。

```go
        // ── 第一步：Token 检查 ──
        a.updateTokenEstimate()

        // 超过 90%：强制 Snip（确定性裁剪，快但粗糙）
        if a.tokenUsageRatio() >= a.cfg.ForceSnipThreshold {
            log.Printf("[agent] token 用量 %.0f%%, 触发强制 snip", a.tokenUsageRatio()*100)
            a.forceSnip()
        } else if a.tokenUsageRatio() >= a.cfg.CompactThreshold {
            // 超过 70%：尝试 Model Compact（LLM 摘要，慢但保留语义）
            log.Printf("[agent] token 用量 %.0f%%, 触发 model compact", a.tokenUsageRatio()*100)
            a.modelCompact()
        }
```

**上下文压缩是 Agent 的生命线**。Token 就像"内存"，一旦用完就不能继续了。GoCoder 的分级策略：
- < 70%：正常执行
- 70% ~ 90%：用模型生成摘要来压缩（效果好但有开销）
- > 90%：强制确定性裁剪（简单粗暴但不费 token）

```go
        // ── 第二步：调用 LLM ──
        toolDefs := a.registry.List()  // 把所有可用工具列出来
        log.Printf("[agent] 第 %d 轮 LLM 调用，消息数: %d，工具数: %d",
            round+1, len(a.messages), len(toolDefs))

        result, err := a.client.Chat(a.messages, toolDefs)
        if err != nil {
            return "", fmt.Errorf("LLM 调用失败: %w", err)
        }
```

**这就是为什么要用 `registry.List()`**：每次调用 LLM 时都带上最新的工具列表（包括 MCP 动态注册的工具）。

```go
        // 更新 token 用量（优先用 provider 返回的精确值）
        if result.Usage != nil {
            a.tokenTotal = result.Usage.PromptTokens + result.Usage.CompletionTokens
        }
```

```go
        // ── 第三步：判断 LLM 的响应类型 ──

        // 情况 1：模型返回纯文本，无工具调用 → 对话结束！
        if len(result.ToolCalls) == 0 {
            a.messages = append(a.messages, types.Message{
                Role:    "assistant",
                Content: result.Content,
            })
            return result.Content, nil  // 返回给用户看
        }
```

这是最简单的路径——LLM 觉得不需要调工具了，直接给了回答。追加 assistant 消息并退出循环。

```go
        // 情况 2：模型请求了工具调用 → 需要执行工具
        // 构建 assistant 消息（包含 tool_calls）
        assistantMsg := types.Message{
            Role:      "assistant",
            Content:   result.Content,     // 可能有一段文字："我来读文件"
            ToolCalls: result.ToolCalls,   // 包含工具调用请求
        }
        a.messages = append(a.messages, assistantMsg)
```

**重要**：assistant 消息同时可能有 Content 和 ToolCalls。有些模型会说"我来帮你读文件"然后附带 tool_call。

```go
        // 执行每个工具调用
        for _, tc := range result.ToolCalls {
            funcName := tc.Function.Name

            // ── 权限审批拦截 ──
            if a.registry.RequiresApproval(funcName) {
                approved := a.requestApproval(tc)
                if !approved {
                    // 被用户拒绝 → 插入一条拒绝消息作为 tool_result
                    denyMsg := fmt.Sprintf("工具 %s 被用户拒绝执行。请尝试其他方案。", funcName)
                    a.messages = append(a.messages, types.Message{
                        Role:       "tool",
                        ToolCallID: tc.ID,
                        Name:       funcName,
                        Content:    denyMsg,
                    })
                    continue  // 跳过这个工具，继续下一个
                }
            }
```

**这里有个精妙设计**：被拒绝的工具也返回一条 tool 消息！为什么？因为 OpenAI API 要求每个 tool_call 必须有对应的 tool_result。如果不插入拒绝消息，下一轮 LLM 调用会因为找不到 tool result 而报错。同时，拒绝消息的内容让 LLM 知道"此路不通"，可以尝试其他方案。

```go
            // ── 解析 LLM 传来的 JSON 参数 ──
            var params map[string]any
            if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
                params = map[string]any{} // 解析失败 → 用空参数兜底
            }

            // ── 真正执行工具！──
            ctx := types.ToolContext{
                WorkDir:   a.cfg.WorkDir,
                SessionID: "",
            }
            output, err := a.registry.Execute(funcName, ctx, params)
            if err != nil {
                output = fmt.Sprintf("工具执行错误: %v", err)
            }
```

从 Registry 找到真正的执行函数，传入参数，拿到结果。执行错误不中断——把错误信息作为 output 返回给 LLM，让 LLM 知道出了什么问题并重新规划。

```go
            // ── 大输出处理（冷热分离）──
            diskPath := ""
            truncated := false
            if len(output) > a.cfg.MaxOutputDisk {  // 默认 > 5000 字符
                diskPath = a.saveToDisk(funcName, output)  // 完整内容落盘
                output = a.truncatePreview(output)           // 上下文保留预览
                truncated = true
                a.diskOutputs[tc.ID] = diskPath
            }

            // 把工具结果追加为 tool 消息
            toolResult := output
            if truncated {
                toolResult = fmt.Sprintf("%s\n\n[完整输出已保存至: %s]", output, diskPath)
            }

            a.messages = append(a.messages, types.Message{
                Role:       "tool",
                ToolCallID: tc.ID,     // 关联到对应的 tool_call
                Name:       funcName,
                Content:    toolResult,
            })
        }

        // 继续下一轮循环！
        // LLM 在下一轮会看到所有 tool_result，然后决定继续调工具还是给出最终文字回答
    }

    // 循环耗尽 → 报错
    return "", fmt.Errorf("达到最大工具调用轮数 (%d)，Agent 循环终止", a.cfg.MaxToolRounds)
}
```

### 5.6 Agent Loop 完整时序图

这是理解 Agent 运作最直观的方式：

```
用户: "帮我重构 main.go 里的 handleRequest 函数"
  │
  ▼
═══════════════ Round 1 ═══════════════
  Agent → LLM: messages=[system prompt, user:"重构 main.go..."], tools=[read_file, write_file, edit_file, ...]
  LLM → Agent: tool_call read_file(path="main.go")
  Agent: 执行 read_file → 得到文件内容（200行）
  Agent: tool_result 追加到 messages
  │
  ▼
═══════════════ Round 2 ═══════════════
  Agent → LLM: messages=[..., tool_call read_file, tool_result: 文件内容], tools=[同上]
  LLM → Agent: tool_call edit_file(path="main.go", old_string="...", new_string="...")
  Agent: 审批 → 用户确认 → 执行 edit_file → 生成 diff
  Agent: tool_result(含 diff) 追加到 messages
  │
  ▼
═══════════════ Round 3 ═══════════════
  Agent → LLM: messages=[..., tool_result: "文件已修改\ndiff:..."], tools=[同上]
  LLM → Agent: "重构完成。改动：1. 提取了 validateInput 函数 2. 简化了错误处理..."
  Agent: 追加 assistant 消息
  Agent: return result.Content → 返回给用户
  │
  ▼
用户看到结果 ✓
```

**面试话术（核心！）**：
> Agent Loop 的核心是 LLM ↔ Tool 的多轮闭环。每轮把 messages 和 tools 发给 LLM，LLM 返回 tool_calls 或文字。有 tool_calls 就执行工具、审批检查、把 tool_result 追加到上下文，然后进入下一轮。这个循环直到 LLM 返回纯文本结束，或者达到最大轮数。过程中还有上下文压缩（token 过高时触发）和权限审批（写文件和 shell 需要确认）。

### 5.7 Resume——恢复会话

```go
func (a *Agent) SetMessages(messages []types.Message) {
    restored := make([]types.Message, 0, len(messages)+1)
    // 恢复时自动补上 system prompt（如果之前的消息列表里没有）
    if a.systemPrompt != "" && (len(messages) == 0 || messages[0].Role != "system") {
        restored = append(restored, types.Message{Role: "system", Content: a.systemPrompt})
    }
    restored = append(restored, messages...)
    a.messages = restored
    a.updateTokenEstimate()
}
```

关键是：恢复消息时自动补上 system prompt。因为保存会话时不存 system prompt，恢复时需要重新注入，确保 AI 记得自己的角色。

---

## <a id="第6章"></a>第 6 章：工具系统——Agent 的手和脚

> **文件**：`internal/tools/tools.go`、`internal/tools/file.go`、`internal/tools/shell.go`

### 6.1 为什么需要工具系统

LLM 本身只会生成文本。没有工具，它只能"建议"你写什么。有了工具，它能**实际去做**——读文件、写代码、运行命令、检查结果。

工具系统要解决两个问题：
1. **告诉 LLM 有什么工具可用**（ToolDefinition → JSON Schema）
2. **真正执行工具**（ToolExecutor → Go 函数）

这两个问题分开解决，就是"声明和执行分离"。

### 6.2 Registry——工具注册表

```go
type Registry struct {
    tools map[string]*types.RegisteredTool  // 工具名 → 工具（O(1) 查找）
    order []string                           // 保持注册顺序（确定性）
}
```

**为什么需要 order**：保证 `List()` 返回的工具顺序是确定的。如果每次顺序不同，LLM 可能产生不同的行为，不方便调试。

核心方法：

```go
// 注册一个工具
func (r *Registry) Register(rt *types.RegisteredTool) {
    name := rt.Definition.Function.Name
    if _, exists := r.tools[name]; !exists {
        r.order = append(r.order, name)  // 新名字才追加到顺序列表
    }
    r.tools[name] = rt  // 同名注册会覆盖 map 中的工具，但不会重复追加 order
}

// 给 LLM 看的工具列表（只返回 Definition，不暴露 Executor）
func (r *Registry) List() []types.ToolDefinition {
    defs := make([]types.ToolDefinition, 0, len(r.order))
    for _, name := range r.order {
        if t, ok := r.tools[name]; ok {
            defs = append(defs, t.Definition)
        }
    }
    return defs
}

// 真正执行工具
func (r *Registry) Execute(name string, ctx types.ToolContext, input map[string]any) (string, error) {
    t, ok := r.tools[name]
    if !ok {
        return "", fmt.Errorf("未知工具: %s", name)
    }
    start := time.Now()
    output, err := t.Executor(ctx, input)
    elapsed := time.Since(start)
    log.Printf("[tool] %s 执行完成，耗时 %v", name, elapsed)
    return output, err
}
```

### 6.3 内置工具一览

GoCoder 注册了 7 个内置工具：

| 工具名 | 风险 | 需审批 | 做什么 |
|--------|------|--------|--------|
| `read_file` | readonly | 否 | 读取文件内容，带行号，支持 offset/limit |
| `write_file` | write | 是 | 创建或覆盖文件，自动生成 diff |
| `edit_file` | write | 是 | 精确字符串替换（old_string → new_string） |
| `list_directory` | readonly | 否 | 列出目录内容，支持递归深度 |
| `grep` | readonly | 否 | 正则搜索文件内容，最多返回 50 条 |
| `run_command` | shell | 是 | 执行 shell 命令，有超时控制和输出截断 |
| `read_tool_result` | readonly | 否 | 读取之前因过大而落盘的工具输出 |

### 6.4 edit_file——最精妙的工具

```go
func makeEditFile(cfg BuiltinConfig) types.ToolExecutor {
    return func(ctx types.ToolContext, input map[string]any) (string, error) {
        path    := getString(input, "path")
        oldStr  := getString(input, "old_string")  // 要被替换的文本
        newStr  := getString(input, "new_string")  // 替换后的文本

        // 读取原文件
        data, err := os.ReadFile(fullPath)
        content := string(data)

        // ── 唯一性检查（关键安全机制！）──
        count := strings.Count(content, oldStr)
        if count == 0 {
            return "", fmt.Errorf("未找到匹配的 old_string，请确认内容精确匹配")
        }
        if count > 1 {
            return "", fmt.Errorf("old_string 匹配到 %d 处，请提供更精确的上下文以确保唯一匹配", count)
        }

        newContent := strings.Replace(content, oldStr, newStr, 1)

        // 生成 diff 预览
        diff := generateDiff(path, content, newContent)

        // 写入文件
        os.WriteFile(fullPath, []byte(newContent), 0644)

        return diff + "\n\n文件 " + path + " 已修改", nil
    }
}
```

**为什么要求唯一匹配？** 防止 LLM 不小心改了多处。比如 LLM 要改 `func main()`，但文件里有 `func main()` 和 `// func main() is the entry point` 两处匹配。如果不够精确，会改错位置。唯一性检查强制 LLM 提供更多上下文（比如加上花括号），确保精确命中。

这是 Claude Code 的核心编辑模式——edit_file 工具加唯一性约束。面试时可以强调这个设计。

### 6.5 为什么 Arguments 是 JSON 字符串

回到第 2 章的问题，现在你有完整的理解了：

```go
// agent.go 中解析 LLM 返回的参数
var params map[string]any
json.Unmarshal([]byte(tc.Function.Arguments), &params)
// Arguments 是 string: "{\"path\":\"main.go\"}"
// 解析后 params 是 map: {"path": "main.go"}

// 然后传给 executor
output, err := a.registry.Execute(funcName, ctx, params)
```

整个链路：LLM 返回 JSON 字符串 → Agent 解析成 map → 传到 Executor → Executor 用 `getString(input, "path")` 取值。

### 6.6 run_command 的安全设计

```go
func runForeground(command, workDir string, timeout time.Duration) (string, error) {
    execCtx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()

    cmd := exec.CommandContext(execCtx, "sh", "-c", command)
    cmd.Dir = workDir
    cmd.Env = filteredEnv()  // 关键：环境变量白名单！

    output, err := cmd.CombinedOutput()  // 合并 stdout + stderr
    // ... 错误处理、输出截断
}
```

**`filteredEnv()` 是安全关键**：

```go
func filteredEnv() []string {
    // 白名单：只传递这些安全的环境变量
    whitelist := map[string]bool{
        "PATH": true, "HOME": true, "USER": true, "SHELL": true,
        "LANG": true, "TERM": true, "PWD": true,
        "GOPATH": true, "GOROOT": true, "GOBIN": true,
        "JAVA_HOME": true, "NODE_PATH": true, "PYTHONPATH": true,
        // ...
    }
    var env []string
    for _, e := range os.Environ() {
        parts := strings.SplitN(e, "=", 2)
        if whitelist[parts[0]] {
            env = append(env, e)
        }
    }
    return env
}
```

**为什么这样做？** 如果直接用 `os.Environ()` 传递所有环境变量，AI 执行 `echo $GOCODER_API_KEY` 就能偷到你的密钥。白名单只传递编程需要的安全变量，不会泄露 API Key、数据库密码等。

---

## <a id="第7章"></a>第 7 章：路径安全——为什么不能让 AI 乱跑

> **文件**：`internal/tools/path.go`

### 7.1 问题场景

AI 可以通过 LLM 构造各种路径请求：

```
read_file("../../etc/passwd")          # 相对路径越界
read_file("/etc/passwd")               # 绝对路径访问
read_file("secret_link")               # 符号链接 → /etc/passwd
write_file("../malicious.sh", "...")    # 写文件越界
```

### 7.2 resolveWorkspacePath——安全边界的实现

```go
func resolveWorkspacePath(workDir, path string) (string, error) {
    // 步骤1：把 workDir 解析为绝对路径（并解析符号链接）
    root, err := filepath.Abs(workDir)
    root, err = filepath.EvalSymlinks(root)  // 解析符号链接

    // 步骤2：如果目标路径是相对的，拼接到 root；如果是绝对的，直接用
    target := path
    if !filepath.IsAbs(target) {
        target = filepath.Join(root, target)
    }
    target, err = filepath.Abs(target)

    // 步骤3：解析目标路径中已存在部分的符号链接
    //        这样 symlink → /etc 这样的攻击也被拦截
    existing := target
    for {
        if _, err := os.Stat(existing); err == nil {
            if resolved, err := filepath.EvalSymlinks(existing); err == nil {
                target = filepath.Join(resolved, strings.TrimPrefix(target, existing))
            }
            break
        }
        parent := filepath.Dir(existing)
        if parent == existing { break }
        existing = parent
    }

    // 步骤4：关键检查——目标路径是否在 root（工作目录）之内？
    rel, err := filepath.Rel(root, filepath.Clean(target))
    if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
        return "", fmt.Errorf("路径越界: %s 不在 workspace %s 内", path, root)
    }

    return filepath.Clean(target), nil
}
```

**面试话术**：
> GoCoder 有一个核心安全机制叫 workspace boundary。所有文件路径操作都经过 resolveWorkspacePath：把路径解析成绝对路径、处理符号链接、然后用 filepath.Rel 计算相对路径。只要结果包含 `..` 就说明越界了，直接拒绝。这样 AI 只能操作项目目录内的文件。

---

## <a id="第8章"></a>第 8 章：权限审批——谁同意 AI 动的？

> **文件**：`internal/permission/permission.go`

### 8.1 四级风险模型

| 风险等级 | 处理方式 | 哪些工具 |
|---------|---------|---------|
| `readonly` | 自动允许（默认白名单） | read_file, grep, list_directory |
| `write` | 需要用户审批 | write_file, edit_file |
| `shell` | 需要审批 + 风险分析 | run_command |
| `mcp` | 需要审批（外部工具不可控） | MCP 注册的工具 |

### 8.2 Runner 核心逻辑

```go
type Runner struct {
    alwaysAllow map[string]bool                     // 永久白名单
    alwaysDeny  map[string]bool                     // 永久黑名单
    decisions   map[string]types.PermissionDecision // call_id → 审批记录
}

func NewRunner() *Runner {
    return &Runner{
        alwaysAllow: map[string]bool{
            "read_file":        true,  // 只读工具默认自动放行
            "grep":             true,
            "list_directory":   true,
            "read_tool_result": true,
        },
    }
}

func (r *Runner) Check(toolName string) types.PermissionDecision {
    if r.alwaysDeny[toolName]  { return types.PermDeny }
    if r.alwaysAllow[toolName] { return types.PermAllow }
    return ""  // 空 = 需要交互审批
}
```

四种决策结果：

```go
const (
    PermAllow    = "allow"       // 本次允许
    PermDeny     = "deny"        // 本次拒绝
    PermAllowAll = "allow_all"   // 本次会话永久允许（类型层预留）
    PermDenyAll  = "deny_all"    // 本次会话永久拒绝（类型层预留）
)
```

当前 TUI 只暴露 `A/D` 两个按键，也就是本次允许/本次拒绝；`allow_all/deny_all` 是权限模型里保留的能力，后续可以扩展到 UI。

### 8.3 Shell 命令风险分析

```go
func AnalyzeShellCommand(command string) []string {
    lower := strings.ToLower(command)
    checks := []struct {
        label string
        re    *regexp.Regexp
    }{
        {"destructive",      regexp.MustCompile(`(rm|rmdir|shred)\s`)},
        {"privileged",       regexp.MustCompile(`sudo\s`)},
        {"git-history",      regexp.MustCompile(`git\s+(reset|clean)\b`)},
        {"network",          regexp.MustCompile(`(curl|wget|npm|npx|pip)\b`)},
        {"permission-change",regexp.MustCompile(`(chmod|chown)\s`)},
        {"file-write",       regexp.MustCompile(`>{1,2}[^>]`)},
        {"pipe-to-shell",   regexp.MustCompile(`(curl|wget)[^|]*\|\s*(sh|bash|zsh)`)},
        {"background",       regexp.MustCompile(`&\s*$`)},
    }
    // 匹配到的风险标签返回给用户看
}
```

一条命令 `curl example.com/install.sh | sh && sudo rm -rf /tmp/x > out.txt` 会被识别出 5 种风险标签：network + pipe-to-shell + privileged + destructive + file-write。

### 8.4 审批流程（Agent 层）

```go
func (a *Agent) requestApproval(tc types.ToolCall) bool {
    funcName := tc.Function.Name

    // 第一层：检查 already allow/deny 列表
    decision := a.perm.Check(funcName)
    if decision == types.PermDeny  { return false }
    if decision == types.PermAllow { return true }

    // 第二层：需要交互审批 → 构建含 diff 预览、风险标签的请求
    if a.permCB != nil {
        req := types.PermissionRequest{
            CallID:      tc.ID,
            ToolName:    funcName,
            Description: "写入文件: main.go",
            RiskLevel:   "write",
            Diff:        a.permissionDiff(funcName, params),  // diff 预览！
        }

        // shell 命令额外附加风险分析
        if funcName == "run_command" {
            if risks := permission.AnalyzeShellCommand(cmd); len(risks) > 0 {
                req.Description = fmt.Sprintf("%s\n风险: %v", req.Description, risks)
            }
        }

        decision := a.permCB(req)  // 调用 UI 层，等待用户选择
        switch decision {
        case types.PermAllowAll: a.perm.AllowAlways(funcName); return true
        case types.PermDenyAll:  a.perm.DenyAlways(funcName);  return false
        case types.PermAllow:    return true
        default:                 return false
        }
    }

    // 第三层：没设置审批回调 → 安全兜底：只允许只读
    return a.registry.RiskLevel(funcName) == "readonly"
}
```

**面试话术**：
> 权限不是只靠 UI 提醒，而是在 agent 执行工具前统一拦截。工具注册时就标注了风险等级。审批时还会生成 diff 预览（文件操作）或风险分析（shell 命令）。即使用户没设置审批回调，也有安全兜底——没有回调时只允许只读工具。

---

## <a id="第9章"></a>第 9 章：MCP 协议——接入外部工具的标准

> **文件**：`internal/mcp/mcp.go`

### 9.1 什么是 MCP

MCP（Model Context Protocol）是 Anthropic 推出的开放协议，定义了 AI 应用如何与外部工具和数据源通信。

**通俗理解**：MCP 就是工具的 USB 接口标准。GoCoder 实现了 MCP 的工具调用子集，所以可以接入很多提供 `tools/list` 和 `tools/call` 的 MCP 工具服务器，比如文件系统、memory、浏览器、GitHub API 等。当前没有实现 MCP resources/prompts 等更完整的能力。

### 9.2 协议流程

```
GoCoder (Client)                    MCP Server
    │                                    │
    │─── initialize ───────────────────→│  握手：协议版本 + 能力声明
    │←── capabilities + serverInfo ─────│
    │                                    │
    │─── notifications/initialized ────→│  通知：初始化完成（不需要响应）
    │                                    │
    │─── tools/list ───────────────────→│  问：你有什么工具？
    │←── [echo, get_time, ...] ────────│
    │                                    │
    │─── tools/call("echo", args) ─────→│  执行工具
    │←── "ECHO: hello" ────────────────│
```

通信格式是 **JSON-RPC 2.0**：

```json
// 请求（有 id，期待响应）
{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}

// 响应
{"jsonrpc": "2.0", "id": 1, "result": {"tools": [...]}}

// 通知（无 id，不需要响应）
{"jsonrpc": "2.0", "method": "notifications/initialized"}
```

**关键区别**：请求有 `id`（需要匹配响应），通知没有 `id`。GoCoder 的 `readLoop` 只处理有 id 的响应，通知会被忽略。

### 9.3 Stdio 模式——进程管道通信

```go
func NewStdioClient(name string, cfg types.MCPConfig) (*Client, error) {
    // 启动 MCP Server 进程
    cmd := exec.Command(cfg.Command, cfg.Args...)
    stdin,  _ := cmd.StdinPipe()   // GoCoder → MCP Server（发送请求）
    stdout, _ := cmd.StdoutPipe()  // MCP Server → GoCoder（接收响应）
    stderr, _ := cmd.StderrPipe()  // MCP Server 日志（丢弃）
    cmd.Start()

    c := &Client{...}
    go c.readLoop()   // 后台 goroutine 持续读 stdout
    go drain(stderr)  // 后台 goroutine 消费 stderr（防止缓冲区满导致 server 阻塞）
    return c, nil
}
```

**核心并发模型**：

```go
// readLoop：后台 goroutine，持续从 stdout 读取
func (c *Client) readLoop() {
    reader := bufio.NewReader(c.stdout)
    for {
        line, err := reader.ReadString('\n')
        var resp types.JSONRPCResponse
        json.Unmarshal([]byte(line), &resp)

        // 按 id 找到对应的等待通道
        c.pendingMu.Lock()
        ch := c.pending[resp.ID]
        c.pendingMu.Unlock()
        if ch != nil {
            ch <- &resp  // 通知等待者
        }
    }
}

// stdioCall：发送请求并等待匹配的响应
func (c *Client) stdioCall(req types.JSONRPCRequest) (*types.JSONRPCResponse, error) {
    ch := make(chan *types.JSONRPCResponse, 1)

    // 注册等待通道
    c.pending[req.ID] = ch

    // 写入 stdin（发送请求），注意用写锁保护
    c.writeMu.Lock()
    c.stdin.Write(data)
    c.writeMu.Unlock()

    // 等待响应（或超时 30s）
    select {
    case resp := <-ch: return resp, nil
    case <-timer.C:    return nil, fmt.Errorf("请求超时")
    case <-c.done:     return nil, errors.New("连接已关闭")
    }
}
```

**用图表示**：

```
stdioCall goroutine           readLoop goroutine
     │                              │
     ├─ 注册 ch = pending[id] ──┐   │
     ├─ 写 stdin ──────────────→│   │
     │                           ├─ 读 stdout ← MCP Server
     │                           ├─ 解析 JSON
     │                           ├─ 找到 pending[id]
     │   pending[id] ← ch ←─────┤
     ├─ 收到响应                  │
     └─ 返回                     └─ 继续读下一行
```

### 9.4 Manager——管理多个 MCP Server + 工具名去重

```go
type Manager struct {
    clients  map[string]*Client      // server 名 → 客户端
    bindings map[string]ToolBinding  // GoCoder 中的工具名 → 来源
}

type ToolBinding struct {
    ServerName     string  // 来自哪个 server
    OriginalName   string  // server 端的原始名
    RegisteredName string  // GoCoder 中的注册名（可能改名）
}
```

**工具名冲突处理**：

```
第一个 server 注册 "echo"  → 直接叫 "echo"
第二个 server 注册 "echo"  → 改名为 "server2__echo"
第三个不同 server 注册 "echo" → 改名为 "server3__echo"
如果同一个前缀也冲突，才会追加数字后缀，例如 "server3__echo_2"
```

### 9.5 兼容性问题：nil params → {}

```go
func encodeParams(params any) (json.RawMessage, error) {
    if params == nil {
        return json.RawMessage(`{}`), nil  // nil → 空 JSON 对象
    }
    data, err := json.Marshal(params)
    return data, err
}
```

这是实际踩过的坑。规范允许 `params` 为 null，但真实公开 MCP server `@modelcontextprotocol/server-memory` 只接受 `params: {}`。兼容性比完美遵循规范更重要。

**面试话术**（如果有面试官问遇到的困难）：

> MCP 兼容性是个实际问题。公开的 memory MCP server 对 `params: null` 没响应，但对 `params: {}` 正常。所以我加了 nil→{} 的编码处理，并写了 opt-in 的公开 server 测试来验证兼容性。协议实现不能只看规范，还要用真实服务验证。

---

## <a id="第10章"></a>第 10 章：Skills 系统——教 AI 怎么做事

> **文件**：`internal/skills/skills.go`

### 10.1 MCP vs Skills：面试最常问的区分

| | MCP | Skills |
|---|---|---|
| 本质 | **可执行能力**（能做什么） | **流程知识**（怎么做） |
| 例子 | 读写文件系统、查数据库、调 API | 代码审查流程、Go 发布流程、重构规范 |
| 实现 | 注册为工具，AI 调用 executor | 注入 system prompt，影响 AI 思考方式 |
| 审批 | 需要（mcp 风险等级） | 不需要（只是 prompt 指令） |

**一句话**：MCP 给 Agent 装"手"，Skills 给 Agent 装"脑"。

### 10.2 Skill 文件格式

Skill 文件统一放在 `~/.gocoder/skills/` 目录下，例如 `~/.gocoder/skills/go-release.md`。GoCoder 不再自动扫描项目目录里的 `skills/`，这样可以避免用户级配置和项目目录互相覆盖。

```markdown
---
name: go-release
description: Go 项目发布流程。当用户提到"发布"、"release"时使用。
keywords: ["发布", "release", "发版"]
---

# Go Release Process

1. 运行测试: `go test ./...`
2. 更新 CHANGELOG.md
3. 创建 tag: `git tag -a vX.Y.Z`
4. 推送到远程: `git push --tags`
5. 创建 GitHub Release
```

`---` 之间是 YAML frontmatter（元数据），后面是 Markdown body（注入给 AI 的指令）。

### 10.3 关键词匹配算法

```go
func matchScore(input string, s *Skill) int {
    score := 0
    // 名称匹配：+5 分
    if strings.Contains(input, strings.ToLower(s.Name)) {
        score += 5
    }
    // 关键词匹配：长关键词 +4 分，短关键词 +3 分
    for _, kw := range s.Keywords {
        if strings.Contains(input, strings.ToLower(kw)) {
            if len([]rune(kw)) >= 4 { score += 4 } else { score += 3 }
        }
    }
    return score  // 总分 ≥ 3 才匹配成功
}
```

举例：用户输入 "帮我发布一个新版本"
- `go-release` 名称不在输入中 → +0
- 关键词 "发布" 命中（2字，<4）→ +3
- 关键词 "release" 不在输入中 → +0
- 总分 3 = 阈值 3 → 匹配成功 ✓

### 10.4 激活后的 Prompt 注入

```go
func (m *Manager) BuildSystemPrompt() string {
    return fmt.Sprintf(`[Active Skill: %s]
The user has activated the skill "%s". Follow the instructions below.

%s

Use the skill only when it helps the current request.`,
        s.Name, s.Name, s.Content)
}
```

注入后的 system prompt 类似：

```
You are GoCoder, a coding assistant. Tools: read_file, ...

[Active Skill: go-release]
The user has activated the skill "go-release". Follow the instructions below.

# Go Release Process
1. 运行测试: `go test ./...`
2. 更新 CHANGELOG.md
...
```

AI 看到这个 prompt 后，会按照 skill 定义的流程一步步执行。但 skill 不会增加新工具——它只是改变了 AI 做事的步骤。

**面试话术**：
> MCP 和 Skills 的职责分离是关键设计。MCP 管理"能做什么"——可执行工具通过注册表接入。Skills 管理"怎么做"——流程知识通过 system prompt 注入。两者都能扩展 Agent，但边界清晰，不会把能力和方法论混在一起。

---

## <a id="第11章"></a>第 11 章：会话管理——记住发生过什么

> **文件**：`internal/session/session.go`

### 11.1 JSONL 格式示例

```jsonl
{"type":"session_start","ts":1717700000,"data":{"session_id":"20250605_210500","version":"1.0"}}
{"type":"message","ts":1717700001,"data":{"role":"user","content":"帮我重构 main.go"}}
{"type":"tool_call","ts":1717700002,"data":{"call_id":"call_123","tool_name":"read_file","arguments":{"path":"main.go"}}}
{"type":"tool_result","ts":1717700003,"data":{"call_id":"call_123","tool_name":"read_file","output":"package main\n...","duration_ms":12}}
{"type":"tool_call","ts":1717700004,"data":{"call_id":"call_456","tool_name":"edit_file","arguments":{"path":"main.go","old_string":"...","new_string":"..."}}}
{"type":"tool_result","ts":1717700005,"data":{"call_id":"call_456","tool_name":"edit_file","output":"文件已修改","duration_ms":5}}
{"type":"message","ts":1717700006,"data":{"role":"assistant","content":"重构完成..."}}
```

**JSONL 的优势**：
- 追加写入，崩溃时已有事件不丢
- 每行一个独立事件，方便逐行 replay
- 不需要把整个会话加载到内存再序列化

### 11.2 保存时的消息处理

```go
func (s *Session) AppendAgentMessage(msg types.Message) error {
    // system prompt 不保存——恢复时会重新注入
    if msg.Role == "system" {
        return nil
    }

    // assistant 有 tool_calls：拆成多个事件保存
    if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
        if msg.Content != "" {
            s.AppendMessage(msg.Role, msg.Content)  // 文字部分
        }
        for _, tc := range msg.ToolCalls {
            s.AppendToolCall(tc.ID, tc.Function.Name, args)  // 每个 tool_call
        }
        return nil
    }

    // tool 消息
    if msg.Role == "tool" {
        return s.AppendToolResult(msg.ToolCallID, msg.Name, msg.Content, ...)
    }

    // 普通消息
    return s.AppendMessage(msg.Role, msg.Content)
}
```

### 11.3 恢复：JSONL → Messages

```go
func EventsToMessages(events []types.SessionEvent) []types.Message {
    var msgs []types.Message

    for _, ev := range events {
        switch ev.Type {
        case types.EventMessage:
            // user/assistant 消息 → 直接恢复
            msgs = append(msgs, types.Message{
                Role: data["role"], Content: data["content"],
            })

        case types.EventToolCall:
            // tool_call → 恢复为 assistant 消息（带单个 tool_call）
            msgs = append(msgs, types.Message{
                Role: "assistant",
                ToolCalls: []types.ToolCall{{ID: callID, Function: ...}},
            })

        case types.EventToolResult:
            // 如果结果有落盘文件，优先读取完整内容
            if diskPath := data["disk_path"]; diskPath != "" {
                output = readDiskFile(diskPath)
            }
            msgs = append(msgs, types.Message{
                Role: "tool", ToolCallID: callID, Content: output,
            })

        case types.EventCompact:
            // compact 事件不恢复（压缩是运行时行为，恢复不需要）
        }
    }
    return msgs
}
```

**恢复时不自动调用 LLM**。`/resume` 之后，Agent 的消息历史被恢复了，但不会自动继续推理。用户输入下一句话后，Agent 才带着之前的完整上下文继续执行。

TUI 里的用法是：

```text
/save              保存当前会话
/resume            列出最近会话
/resume <id|path>  恢复指定会话
```

---

## <a id="第12章"></a>第 12 章：上下文压缩——上下文太长怎么办

> **文件**：`internal/compact/compact.go`

### 12.1 问题：Token 窗口是有限的

GPT-4o 的上下文窗口是 128K token。每轮对话（用户消息 + AI 回答 + tool_calls + tool_results）都在消耗这个空间。当接近上限时，需要压缩早期内容。

### 12.2 Token 估算（因为没有 SDK 的 tokenizer）

```go
func EstimateTokens(text string) int {
    chars := utf8.RuneCountInString(text)
    cjkCount := 0
    for _, r := range text {
        if (r >= 0x4E00 && r <= 0x9FFF) { cjkCount++ }  // 中文
    }
    nonCJK := chars - cjkCount
    // 中文: ~0.6 token/字, 英文: ~0.25 token/字符
    return int(float64(cjkCount)*0.6 + float64(nonCJK)*0.25)
}
```

**为什么不用精确 tokenizer？** 精确 tokenizer（如 tiktoken）需要额外依赖，且不同模型的 tokenizer 也不同。GoCoder 定位轻量级，估算足够做压缩触发判断，优先使用 provider 返回的精确 usage。

### 12.3 MessageGroup——保护工具轮次的原子性

这是压缩的核心设计：

```go
func BuildMessageGroups(msgs []types.Message) []MessageGroup {
    var groups []MessageGroup
    i := 0
    for i < len(msgs) {
        msg := msgs[i]

        // assistant 有 tool_calls → 后面的 tool 结果都属于这一组
        if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
            end := i + 1
            callIDs := map[string]bool{}
            for _, tc := range msg.ToolCalls {
                callIDs[tc.ID] = true
            }
            // 收集所有关联的 tool 结果
            for end < len(msgs) && msgs[end].Role == "tool" && callIDs[msgs[end].ToolCallID] {
                end++
            }
            groups = append(groups, MessageGroup{
                Type: "tool_round", Messages: msgs[i:end],
            })
            i = end
        } else {
            // 普通消息（user/assistant/system），单独一组
            groups = append(groups, MessageGroup{
                Type: "simple", Messages: msgs[i : i+1],
            })
            i++
        }
    }
    return groups
}
```

**为什么这很重要**：OpenAI API 要求 `assistant(tool_calls)` 必须在对应的 `tool(result)` 前面。如果你单独删掉了 assistant 的 tool_call 但保留了 tool result，API 会因为消息结构不合法而报错。

### 12.4 两级压缩策略

```go
// agent.go 中的触发逻辑
if a.tokenUsageRatio() >= a.cfg.ForceSnipThreshold {   // > 90%
    a.forceSnip()          // 快速但粗糙：直接裁剪早期消息组
} else if a.tokenUsageRatio() >= a.cfg.CompactThreshold { // > 70%
    a.modelCompact()       // 慢但保留语义：让 LLM 总结早期对话
}
```

**Snip（确定性裁剪）**：
- 保留最近 3 轮 tool_round
- 移除早期消息组
- 插入 boundary message 告诉 AI 发生了什么

**Model Compact（LLM 摘要）**：
```go
func ModelCompact(client CompactClient, oldMessages []types.Message) (string, error) {
    prompt := `Please create a structured summary of the conversation segment above. 
Output JSON: {"task_goal":..., "decisions_made":[...], "files_modified":{...}, ...}`
    summary, err := client.ChatCompact(oldMessages, prompt)
    return summary, nil
}
```
- 把早期消息发给 LLM
- LLM 返回结构化 JSON（任务目标、决策、文件修改、关键发现）
- 将摘要作为 system 消息注入，替换旧消息

**如果 model compact 失败怎么办？** 代码有降级：
```go
summary, err := compact.ModelCompact(a.client, oldMessages)
if err != nil {
    log.Printf("[agent] model compact 失败: %v，降级为 snip", err)
    a.forceSnip()  // 降级为确定性裁剪
    return
}
```

### 12.5 Boundary Message——给 AI 的 "上下文提示"

```go
func BuildSnipBoundary(removedCount int, removed []types.Message) string {
    // 提取关键信息：用户的提问、AI 调了哪些工具
    var keyInfo []string
    for _, m := range removed {
        if m.Role == "user" {
            keyInfo = append(keyInfo, "用户问: "+truncate(m.Content, 80))
        }
        if m.Role == "assistant" && len(m.ToolCalls) > 0 {
            keyInfo = append(keyInfo, "调用工具: "+toolNames)
        }
    }
    return fmt.Sprintf(
        "[Snipped earlier conversation segment]\n"+
        "%d earlier messages were removed.\n\n"+
        "Key moments:\n%s\n\n"+
        "If you need information from the earlier part, use your tools to re-read files.",
        removedCount, keyText)
}
```

这条消息让 AI 知道：上下文被裁剪了，但关键事件还在。如果需要被裁剪的细节，应该主动用工具重新读取。

**面试话术**：
> 上下文压缩不是简单删除历史。我先把消息按 tool_call+tool_result 原子组打包，保证 tool calling 消息结构不被破坏。然后分两级：70% 以上调 LLM 生成语义摘要（保留理解），90% 以上强制确定性裁剪（快速释放空间）。压缩后注入 boundary message，让模型知道上下文被裁剪了，需要时可以重新读文件。

---

## <a id="第13章"></a>第 13 章：TUI 界面——用户怎么跟 Agent 交互

> **文件**：`internal/tui/tui.go`
>
> **这一章不是面试重点**。面试官基本不会问 TUI 细节。但你需要知道 TUI 是怎么把 Agent 和用户连接起来的。这里只讲核心的连接部分。

### 13.1 Bubble Tea 架构（Elm 架构）

```
Model（状态） → Update（更新） → View（渲染）
     ↑                              │
     └──────────────────────────────┘
```

- **Model**：所有界面状态（对话历史、输入框内容、当前阶段等）
- **Update**：收到消息（按键、数据到达等）后如何更新状态
- **View**：把当前状态渲染成终端上的字符串

### 13.2 TUI Model 的关键字段

```go
type model struct {
    agent        *agent.Agent          // 核心 Agent
    llmClient    *llm.Client           // LLM 客户端
    toolRegistry *toolpkg.Registry     // 工具注册表
    session      *session.Session      // 当前会话
    skillMgr     *skills.Manager       // Skills 管理器
    mcpMgr       *mcp.Manager          // MCP 管理器

    state        State                 // idle / streaming / executing / permission
    chatHistory  string                // 已渲染的对话历史（Markdown）
    streamBuf    string                // 流式内容缓冲区（打字机效果）
}
```

### 13.3 用户输入的完整处理链路

```go
func (m *model) handleSubmit() tea.Cmd {
    input := strings.TrimSpace(m.textInput.Value())

    // /命令 → 特殊处理
    if strings.HasPrefix(input, "/") {
        return m.handleCommand(input)
    }

    // 普通输入 → 检查 Skill 自动匹配
    if skill := m.skillMgr.Match(input); skill != nil {
        m.skillMgr.Activate(skill.Name)
        m.agent.SetSystemPrompt(buildSystemPrompt(skillContent))
    }

    // 启动 Agent 作为后台任务
    return tea.Batch(m.spinner.Tick, m.runAgent(input))
}
```

### 13.4 runAgent——连接 Agent 和 UI 的桥梁

```go
func (m *model) runAgent(input string) tea.Cmd {
    return func() tea.Msg {
        p := m.program

        // 注入三个回调
        m.agent.SetCallbacks(
            // 回调1：流式内容（通知 UI 更新打字机效果）
            func(content string, done bool, err error) {
                p.Send(streamChunkMsg{content: content, err: err})
            },
            // 回调2：工具状态（通知 UI 显示 "⚙ read_file... / ✓ read_file 完成"）
            func(status, toolName, detail string) {
                p.Send(streamChunkMsg{toolStatus: ...})
            },
            // 回调3：权限审批（发送到 TUI，等待用户按 A/D）
            func(req types.PermissionRequest) types.PermissionDecision {
                resp := make(chan types.PermissionDecision, 1)
                p.Send(permissionRequestMsg{req: req, resp: resp})
                return <-resp
            },
        )

        // 真正执行 Agent
        content, err := m.agent.Run(input)
        return agentDoneMsg{finalContent: content, err: err}
    }
}
```

**并发模型**：`runAgent` 返回的是一个 `tea.Cmd`，它会在独立的 goroutine 中运行。当需要更新 UI 时，通过 `p.Send()` 把消息推回 Bubble Tea 的主事件循环。这是 Go 并发在 UI 编程中的经典模式。

### 13.5 MCP 工具的注册

```go
// 在 connectMCPServers 中，MCP 工具注册的 Executor 是一个闭包
for _, t := range m.mcpMgr.GetAllTools() {
    toolName := t.Name  // 捕获变量！
    m.toolRegistry.Register(&types.RegisteredTool{
        Definition: BuildToolDef(t.Name, t.Description, t.InputSchema),
        Executor: func(ctx types.ToolContext, input map[string]any) (string, error) {
            return m.mcpMgr.CallRegisteredTool(toolName, input)
            // ↑ toolName 被闭包捕获，调用时转发到 MCP Manager
        },
        RequiresApproval: true,
        RiskLevel:        "mcp",
    })
}
```

**注意**：`toolName := t.Name` 这一行很关键。如果不复制变量，Go 闭包会捕获循环变量的引用，导致所有 executor 都调用最后一个工具。

---

## <a id="第14章"></a>第 14 章：启动流程——从头到尾串一遍

> **文件**：`main.go`

### 14.1 CLI 结构

```
gocoder
├── gocoder (默认)        → 启动 TUI
├── gocoder config show   → 显示当前配置
├── gocoder config reset  → 重置配置为默认值
├── gocoder config revert → 恢复到备份配置
└── gocoder version       → 显示版本号
```

使用 [Cobra](https://github.com/spf13/cobra) CLI 框架。

### 14.2 完整启动流程

```
gocoder 命令（无子命令）
  │
  ├─ 步骤1：LoadAppConfig()
  │     └─ 读取 ~/.gocoder/config.yaml，不存在则用内嵌默认值创建
  │
  ├─ 步骤2：GetModelConfig(appConfig)
  │     └─ 提取模型配置（name, endpoint, max_tokens 等）
  │
  ├─ 步骤3：os.Getenv(AuthEnvVar)
  │     └─ 从环境变量读取真正的 API Key（如 GOCODER_API_KEY）
  │     └─ 如果没设置 → 打印友好提示并退出
  │     └─ 把 Key 注入到 modelConfig.AuthEnvVar 字段
  │
  ├─ 步骤4：设置工作目录
  │     └─ ProjectDir 为 "." 或空 → 使用当前目录
  │
  ├─ 步骤5：EnsureDirs()
  │     └─ 创建 ~/.gocoder/tool_results/
  │     └─ 创建 ~/.gocoder/sessions/
  │     └─ 创建 ~/.gocoder/skills/
  │
  └─ 步骤6：tui.Run(appConfig, modelConfig)
        │
        ├─ NewModel() 初始化：
        │   ├─ 创建 textinput（输入框）
        │   ├─ 创建 spinner（加载动画）
        │   ├─ 创建 glamour renderer（Markdown 渲染）
        │   ├─ 创建 LLM Client
        │   ├─ 创建 Tool Registry + 注册 7 个内置工具
        │   ├─ 创建 Agent（MaxToolRounds=15, CompactThreshold=0.7）
        │   ├─ 设置 System Prompt
        │   ├─ 加载 Skills（从 ~/.gocoder/skills/）
        │   └─ 创建 MCP Manager
        │
        ├─ tea.NewProgram(m) 创建 Bubble Tea 程序
        ├─ m.program = p     把 Program 引用存到 model
        │
        └─ p.Run() 启动 Bubble Tea 主循环
              │
              ├─ Init(): 启动输入框闪烁 + spinner + 连接 MCP servers
              │
              └─ 进入事件循环，等待用户输入...
```

### 14.3 退出清理

```go
func (m *model) cleanup() {
    if m.session != nil { m.session.Close() }  // 关闭会话文件
    if m.mcpMgr != nil   { m.mcpMgr.CloseAll() } // 断开 MCP 连接
}
```

---

## <a id="第15章"></a>第 15 章：面试指南——怎么跟面试官讲这个项目

### 15.1 30 秒版本（电梯游说）

> GoCoder 是我用 Go 写的轻量级终端 Coding Agent，兼容 OpenAI 标准接口。核心是一个多轮 Agent Loop：LLM 返回 tool_calls，程序执行工具后把结果喂回 LLM 继续推理。有权限审批、MCP 外部工具扩展、Skills 流程注入和 JSONL 会话追踪。零 SDK 依赖，纯 net/http 调 API，约 5000 行核心 Go 代码。

### 15.2 1 分钟版本

按这个顺序展开：

1. **Agent Loop**："核心是一个 for 循环。把 messages 和 tools 发给 LLM。LLM 返回 tool_calls 就执行工具、审批、把 tool_result 追加到上下文，进入下一轮。LLM 返回文字就结束。最多 15 轮防死循环。"

2. **工具系统**："7 个内置工具。声明（给 LLM 看的 JSON Schema）和执行（Go 函数）分离。文件操作有 workspace boundary，路径越界被拒绝。"

3. **安全**："三级防护：workspace boundary 限制文件范围、权限审批拦截写/shell/mcp 操作、shell 风险分析匹配危险关键词。"

4. **MCP + Skills**："MCP 接外部可执行工具，Skills 注入流程知识。两者职责分离——一个管能力，一个管方法论。"

5. **上下文管理**："Token 估算 + 两级压缩。70% 触发模型摘要，90% 强制裁剪。压缩前按 tool_call+result 分组保证消息结构合法。"

### 15.3 面试高频问题速答

**Q: Agent Loop 怎么防止死循环？**

> `MaxToolRounds`（默认 15）。每轮都要调 LLM，消耗 token 和时间。到了上限就强制退出。

**Q: 怎么保证 AI 不乱改文件？**

> 三层：workspace boundary（所有路径检查是否在项目目录内）、权限审批（写和 shell 需要确认）、diff 预览（修改前让用户看到将要发生什么）。

**Q: MCP 和 Skills 的区别？**

> MCP 是可执行能力——新增"手"，通过 JSON-RPC 调外部工具。Skills 是流程知识——改造"脑"，通过 system prompt 注入。前者需要审批，后者不需要。

**Q: 上下文压缩怎么做？**

> 先把消息分组——assistant(tool_calls) 和 tool(result) 绑在一起。然后按 token 使用率触发：>70% 让 LLM 生成结构化摘要，>90% 直接裁剪早期分组。压缩后插入 boundary message。

**Q: 为什么不用 OpenAI Go SDK？**

> 想理解底层。SSE 流式解析、消息管理、tool calling 自己实现才能透彻理解 Agent 的运作机制。而且零 SDK 依赖切换 provider 更方便——改个 endpoint 就行。

**Q: 遇到什么技术难点？**

> MCP 协议兼容性。真实公开 server 对规范中 "params 可为 null" 不兼容，只接受 `{}`。反映了协议落地中的现实问题：协议是理想，实现是妥协。我通过 opt-in 的真实 server 测试来持续验证。

**Q: 和 Codex / Claude Code 这种生产级工具比，还差什么？**

> GoCoder 的定位是轻量级教学和简历项目，重点是把 coding agent 的核心闭环做清楚。和生产级工具比，差距主要在：更强的 patch 引擎、自动测试修复闭环、代码索引和语义检索、更完整的 MCP resources/prompts、更严格的 sandbox/policy、以及系统化 eval benchmark。

**Q: 后续你会怎么改进？**

> 我会分阶段做。第一阶段补 patch 可靠性：标准 unified diff、dry-run、冲突检测和回滚。第二阶段补测试反馈闭环：修改后自动选择测试、解析失败日志、限制 N 轮自动修复。第三阶段做生产化能力：代码索引、MCP resources/prompts、安全策略和 eval harness。优先级最高的是 patch 引擎和测试修复循环，因为它们最直接影响 coding agent 的真实任务成功率。

### 15.4 生产级差距：你要怎么理解这个问题

不要把“还差什么”说成项目失败。正确说法是：

> 这个项目已经覆盖了 coding agent 的核心链路，但它是轻量级实现，不是商用级产品。生产级工具做得更深的是可靠性、规模化和用户体验。

可以按六个方向回答：

1. **编辑可靠性**
   - 当前：`edit_file` 精确字符串替换。
   - 生产级：标准 patch 引擎、多文件 patch、上下文偏移、冲突恢复、撤销回滚。

2. **验证闭环**
   - 当前：Agent 可以调用 `run_command` 运行测试。
   - 生产级：自动判断该跑哪些测试，解析失败日志，继续修复，最后报告验证结果。

3. **上下文和检索**
   - 当前：热上下文 + token 估算 + snip/model compact。
   - 生产级：代码索引、语义检索、文件相关性排序、长期项目记忆、更精确 tokenizer。

4. **MCP 完整协议**
   - 当前：实现 MCP tools 子集，支持 `tools/list` 和 `tools/call`。
   - 生产级：还会支持 resources、prompts、sampling、streamable HTTP/SSE、server health 等。

5. **安全和审计**
   - 当前：workspace boundary、权限审批、shell 风险标签。
   - 生产级：系统级 sandbox、网络访问策略、命令 AST 解析、企业 policy、审计日志。

6. **评测体系**
   - 当前：单元测试、mock MCP、公开 MCP opt-in 测试。
   - 生产级：coding task benchmark，统计任务成功率、工具调用次数、耗时、失败类型。

一句面试总结：

> 我后续会优先做 patch 引擎和测试修复循环。因为对于 coding agent 来说，能不能稳定、可验证地改代码，比继续堆 UI 或接更多 provider 更重要。

### 15.5 代码重点回顾（面试前重读）

| 优先级 | 函数 | 在哪个文件 | 为什么重要 |
|--------|------|-----------|----------|
| ★★★★★ | `agentLoop()` | agent.go | 整个 Agent 的心脏 |
| ★★★★★ | `requestApproval()` | agent.go | 安全审批的入口 |
| ★★★★ | `processStream()` | client.go | SSE 流式解析 + tool_call 累积 |
| ★★★★ | `resolveWorkspacePath()` | path.go | 安全边界实现 |
| ★★★★ | `Execute()` | tools.go | 工具执行调度 |
| ★★★ | `AnalyzeShellCommand()` | permission.go | Shell 风险识别 |
| ★★★ | `BuildMessageGroups()` | compact.go | 上下文压缩前的原子分组 |
| ★★★ | `EventsToMessages()` | session.go | JSONL 恢复到 OpenAI 格式 |
| ★★★ | `Manager.Connect()` | mcp.go | MCP 连接 + 工具发现 |
| ★★ | `matchScore()` | skills.go | Skill 关键词匹配 |

---

## 附录 A：Go 语言知识点速查

这份代码用到的 Go 特性：

| 知识点 | 在哪用 | 一句话说明 |
|--------|--------|----------|
| `//go:embed` | config.go | 编译时把 config.yaml 嵌入二进制 |
| `goroutine + channel` | mcp.go, tui.go | 后台读 MCP 响应、异步执行 Agent |
| `sync.Mutex` | mcp.go | 保护对 stdin 的并发写入 |
| `sync.RWMutex` | skills.go, mcp.go | 多读单写的并发保护 |
| `atomic.Int64` | mcp.go | 原子递增请求 ID |
| 闭包 | tools.go, tui.go | 工具执行器、MCP 转发 |
| `interface` | compact.go | `CompactClient` 解耦 compact 和 llm |
| `defer` | 多处 | 资源清理 |
| `bufio.Scanner` | client.go, session.go | 流式读取 |
| `json.RawMessage` | types.go, mcp.go | 延迟解析 JSON（保留原始字节） |
| `context.WithTimeout` | shell.go, mcp.go | 超时控制 |

---

## 附录 B：延伸阅读

- [OpenAI Chat Completions API 文档](https://platform.openai.com/docs/guides/chat-completions) — 理解消息格式和 tool calling 的权威参考
- [MCP 协议规范](https://spec.modelcontextprotocol.io/) — MCP 的完整规范
- [Bubble Tea 教程](https://github.com/charmbracelet/bubbletea) — TUI 框架文档
- [SSE 协议 (MDN)](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events) — 理解流式传输的底层协议
- [Google Diff Match Patch](https://github.com/google/diff-match-patch) — diff 算法
- [JSON-RPC 2.0 规范](https://www.jsonrpc.org/specification) — MCP 的通信格式基础

---

> **写到最后**：这个项目虽然只有约 5000 行核心 Go 代码，但覆盖了 Coding Agent 的所有核心概念——Agent Loop、Tool Calling、权限审批、MCP 扩展、上下文压缩、会话管理。理解它，你就理解了 Claude Code、Cursor、Aider 这些工具的底层原理。它们本质上都遵循相同的 Agent Loop 模式——只是工具更多、体验更精致。
>
> 祝你学习愉快，面试顺利！🎉

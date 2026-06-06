# GoCoder 面试 QA

## 1. 你这个项目简单介绍一下？

GoCoder 是一个用 Go 写的轻量级终端 Coding Agent。它兼容 OpenAI 标准 Chat Completions API，所以可以接 GPT、DeepSeek 这类 OpenAI-compatible 模型。

核心能力包括：

- 终端 TUI 交互和流式输出。
- 多轮 Agent Loop。
- OpenAI tool calling。
- 文件读写、grep、shell 等内置工具。
- 权限审批和 shell 风险识别。
- MCP 外部工具接入。
- Skills 流程知识注入。
- JSONL session trace 和恢复。
- token 估算和上下文压缩。

我做这个项目主要是为了理解一个 coding agent 的核心链路，而不是只做一个 API wrapper。

## 2. 这个项目最大的亮点是什么？

我觉得亮点有三个：

第一，Agent Loop 是完整闭环。模型可以返回 tool_calls，GoCoder 执行工具后把 tool_result 继续喂回模型，直到模型给出最终回答。

第二，有安全边界。文件工具限制在 workspace 内，写操作和 shell/MCP 工具需要审批，shell 命令会识别高风险关键词。

第三，MCP 和 Skills 做了职责拆分。MCP 是外部可执行工具，Skills 是流程知识注入，两者都能扩展 agent，但边界不同。

## 3. 为什么用 Go 写？

Go 比较适合写 CLI/TUI 工具：

- 二进制分发简单。
- 并发和进程管理方便。
- 标准库的 HTTP、JSON、os/exec 足够实现核心逻辑。
- Bubble Tea 生态适合做终端 UI。

另外我想尽量少依赖 SDK，自己实现 OpenAI-compatible 请求、SSE streaming 和 MCP JSON-RPC，这样能更清楚理解底层。

## 4. Agent Loop 是怎么设计的？

Agent Loop 的核心逻辑在 `internal/agent/agent.go`：

1. 用户输入追加成 `user` message。
2. agent 把当前 messages 和 tools 发送给模型。
3. 如果模型返回普通文本，就结束。
4. 如果模型返回 tool_calls，就先追加 assistant tool_call message。
5. 对每个 tool_call 做权限审批。
6. 执行工具，拿到结果。
7. 把结果追加成 `tool` message。
8. 进入下一轮模型调用。

这样形成 `LLM -> tool -> result -> LLM` 的闭环。

## 5. tool_call 和 tool_result 为什么要成对保存？

因为 OpenAI tool calling 的消息结构要求 assistant 的 tool_call 后面要有对应的 tool result。

如果上下文压缩或者 session 保存时把它们拆开，会出现两个问题：

- 恢复后的消息结构不合法。
- 模型看不到工具执行结果，会误以为工具还没执行。

所以 GoCoder 在 compact 里把 assistant(tool_calls) 和后续 tool(result) 当成不可切割的 message group。

## 6. Tool Registry 是什么？

Tool Registry 是工具注册表。

它同时保存两类信息：

- 给模型看的 function schema：工具名、描述、参数。
- 给 GoCoder 执行的 executor：真正的 Go 函数。

模型只会返回工具名和参数，agent 根据工具名从 registry 找 executor 执行。

## 7. 文件编辑是怎么做的？

主要有 `write_file` 和 `edit_file`。

`write_file` 是创建或覆盖文件，会生成 diff。

`edit_file` 是精确字符串替换：

- 读取文件。
- 检查 old_string 是否存在。
- 要求 old_string 只能匹配一次。
- 替换成 new_string。
- 生成 diff。
- 写回文件。

这样比让模型直接生成 patch 更轻量，也比整文件覆盖更安全。

## 8. 如何保证模型不会乱改文件？

主要靠三层：

第一，workspace boundary。所有文件路径都会解析成绝对路径，然后检查是否仍在项目目录内。

第二，权限审批。写文件、编辑文件、shell、MCP 默认都要用户确认。

第三，diff 预览。write/edit 在执行前可以给出 diff，让用户看到将要修改什么。

## 9. Shell 命令怎么做安全控制？

`run_command` 默认需要审批。

审批前会分析命令风险，比如：

- `rm`：destructive
- `sudo`：privileged
- `git reset`：git-history
- `curl` / `wget` / `npm` / `npx`：network
- `curl | sh`：pipe-to-shell
- `>` / `>>`：file-write

另外 shell 的 working_dir 也必须在 workspace 内。

## 10. MCP 是什么？

MCP 是 Model Context Protocol，可以理解成 agent 连接外部工具和数据源的协议。

它让不同工具服务通过统一协议暴露能力，比如 filesystem、memory、browser、database、GitHub 等。

GoCoder 实现了最小完整 MCP 工具链：

- `initialize`
- `notifications/initialized`
- `tools/list`
- `tools/call`

然后把 MCP server 提供的工具注册进本地 Tool Registry。

## 11. 你是怎么实现 MCP 的？

核心在 `internal/mcp/mcp.go`。

stdio MCP：

- 用 `os/exec` 启动 MCP server。
- stdin 发送 JSON-RPC request。
- stdout 后台 readLoop 读取 response。
- 用 request id 匹配 pending response。
- stderr 单独 drain，避免 server 日志阻塞。

HTTP MCP：

- POST JSON-RPC 请求。
- 默认尝试 `/mcp` 路径，也兼容直接 root 路径。

工具注册：

- 连接 server。
- initialize。
- tools/list。
- 对每个工具生成本地 registered tool。
- 模型调用时通过 `tools/call` 转发给 MCP server。

## 12. MCP 工具重名怎么办？

Manager 维护一个 tool binding 表。

如果第一个 server 有 `echo`，它可以直接注册为 `echo`。

如果第二个 server 也有 `echo`，就注册成 `server__echo`。

调用时再通过 binding 找到真实 server 和原始工具名。

## 13. 真实公开 MCP server 测过吗？

测过。

项目里有 opt-in 测试：

```bash
GOCODER_PUBLIC_MCP_TEST=1 go test -run 'TestPublic(Memory|Filesystem)MCP' -v ./internal/mcp
```

覆盖了：

- `@modelcontextprotocol/server-memory`
- `@modelcontextprotocol/server-filesystem`

普通 `go test ./...` 不会联网，这两个测试默认跳过。

## 14. Skills 是什么？

Skills 是流程知识，不是工具。

比如 code review skill 会告诉模型：

- 先看 diff。
- 再逐文件审查。
- 再运行测试。
- 最后按严重程度输出问题。

它不会新增执行权限，只是改变模型做事的流程。

## 15. MCP 和 Skills 的区别？

MCP 是可执行能力，Skills 是流程知识。

举例：

- MCP filesystem：可以真的读写文件。
- Skill code-review：告诉模型按什么步骤审查代码。

所以 MCP 需要权限审批，Skill 主要是 prompt 注入。

这个职责拆分能避免把“能做什么”和“应该怎么做”混在一起。

## 16. Skills 是怎么匹配的？

Skill 文件有 frontmatter：

```yaml
name: code-review
description: 代码审查流程
keywords: ["review", "审查"]
```

GoCoder 统一从 `~/.gocoder/skills` 加载 skills。用户可以：

- `/skill code-review` 手动激活。
- 输入里命中关键词时自动匹配。

激活后，skill 内容会注入 system prompt。

## 17. 上下文怎么压缩？

有两种方式：

第一种是 Snip，确定性裁剪早期消息。

第二种是 Model Compact，用模型把早期上下文总结成结构化摘要。

压缩前会先分组，assistant(tool_calls) 和 tool(result) 作为不可切割的一组。

压缩后会加入一条 system boundary message，告诉模型早期上下文被裁剪了，如果需要事实，应该重新读文件或询问用户。

## 18. token 怎么统计？

优先用 provider 返回的 usage。

如果 provider 没返回 usage，就本地估算：

- 英文约 `字符数 / 4`。
- 中文约 `字符数 * 0.6`。
- 每条 message 额外加少量格式开销。
- tool_call 的函数名、参数也计入估算。

这不是精确 tokenizer，但足够做轻量级 compact 触发判断。

## 19. 如何解决幻觉问题？

不能完全消灭幻觉，但可以降低。

GoCoder 的策略是：

- 让模型通过工具读文件，而不是凭记忆猜。
- 文件读取返回行号。
- 编辑必须基于精确 old_string。
- 修改后可以运行测试。
- 工具失败会把错误返回给模型，让模型继续修正。

核心思想是把事实来源从模型记忆转移到工具和测试。

## 20. 如何管理记忆？

GoCoder 有两类记忆：

短期记忆是当前 messages，也就是热上下文。

长期/可恢复记忆是 session JSONL trace，包括用户消息、assistant 消息、tool_call、tool_result、permission、compact 等事件。

用户可以用 `/save` 保存当前会话，用 `/resume` 列出最近会话，用 `/resume <id|path>` 恢复指定会话。恢复时不会立刻让模型继续生成，而是先恢复上下文，等用户下一次输入后再继续 agent loop。

MCP memory server 也可以作为外部记忆接入，但它属于扩展能力，不是 GoCoder 内部必需依赖。

## 21. 为什么 session 用 JSONL？

JSONL 很适合事件流：

- 每行一个事件。
- 追加写简单。
- 程序崩溃时已经写入的事件不会丢。
- 方便 replay。
- 不需要一次性把整个会话序列化成大 JSON。

GoCoder 恢复时会把 JSONL 事件重新转换成 OpenAI messages，尤其会保留 assistant 的 tool_call 和对应 tool_result，而不是只恢复聊天文本。

## 22. 这个项目和普通 ChatGPT wrapper 有什么区别？

普通 wrapper 只是把用户输入发给模型，然后展示回答。

GoCoder 多了几层：

- tool calling 闭环。
- 文件和 shell 工具。
- 权限审批。
- MCP 外部工具扩展。
- Skills 流程知识。
- session trace。
- context compact。

所以它更接近一个 coding agent，而不是聊天壳。

## 23. 为什么不做得更大，比如完整 Codex？

这是有意取舍。

我想做的是轻量级、可读、可面试讲清楚的 coding agent，所以只保留核心能力：

- OpenAI-compatible 模型接口。
- 多轮 tool calling。
- 文件/shell 工具。
- 权限审批。
- MCP tools。
- Skills prompt。
- session 和 compact。

没有做插件市场、复杂 eval、完整 SSE MCP、复杂 provider adapter，因为那些会让项目变大，但不一定提升核心理解。

## 24. 如果继续优化，你会做什么？

我会按优先级做：

1. 更标准的 unified diff patch 引擎。
2. 更完整的自动测试修复循环。
3. MCP resources 和 prompts 支持。
4. 小型 coding task eval harness。
5. 更精确的 tokenizer。

但当前版本已经覆盖 coding agent 的核心工程链路。

## 25. 和 Codex / Claude Code 这种生产级工具比，还差什么？

我会先强调这个项目的定位：GoCoder 是轻量级教学和简历项目，目标是把 coding agent 的核心链路讲清楚，不是要在几千行代码里复刻完整商用产品。

如果对齐生产级工具，主要差在这些方面：

1. **编辑能力更强**
   现在主要是 `old_string -> new_string` 的精确替换。生产级工具通常有更完整的 patch 引擎，能处理多文件 patch、上下文偏移、冲突恢复、dry-run 和更标准的 unified diff。

2. **自动验证闭环更完整**
   现在能执行 shell 和测试，但还没有专门的“修改后自动选择测试、解析失败、继续修复”的 workflow。生产级 coding agent 会把 test failure parsing 和下一轮修复做得更系统。

3. **上下文管理更精细**
   GoCoder 有 token 估算、snip 和 model compact，但生产级工具会有更强的代码索引、语义检索、文件相关性排序、长期项目记忆和更精确 tokenizer。

4. **MCP 能力更完整**
   目前 GoCoder 实现的是 MCP tools 子集：`initialize`、`tools/list`、`tools/call`。后续可以支持 resources、prompts、sampling、streamable HTTP/SSE 等更完整协议能力。

5. **安全沙箱更严格**
   现在有 workspace boundary、权限审批和 shell 风险识别。生产级工具还会有更强的命令解析、系统级 sandbox、网络访问策略、审计日志和可配置 policy。

6. **评测体系更成熟**
   GoCoder 有单元测试和 MCP 集成测试，但生产级工具需要 eval harness，比如跑一批真实 coding tasks，统计成功率、测试通过率、平均工具调用次数和失败原因。

7. **用户体验和协作能力**
   当前 TUI 是轻量可用。生产级工具会有更好的 diff 交互、任务计划展示、并发工具状态、撤销/回滚、checkpoint、分支管理和团队配置。

面试时可以这样回答：

> 当前版本我重点实现了 coding agent 的核心闭环：LLM tool calling、工具注册、权限审批、MCP tools、Skills、session trace 和 compact。和 Codex/Claude Code 这种生产级工具比，差距主要在 patch 引擎、自动测试修复闭环、代码索引/检索、更完整 MCP 协议、安全沙箱和 eval 体系。后续我会优先做 patch 引擎和测试修复循环，因为这两项最直接决定 coding agent 能不能稳定完成真实代码任务。

## 26. 如果面试官追问“后续怎么改进”，怎么分阶段说？

可以按三阶段回答：

第一阶段，提升“改代码”的可靠性：

- 标准 unified diff patch 引擎。
- patch dry-run 和冲突检测。
- 修改前后 diff preview。
- 失败时让模型基于错误重新生成 patch。

第二阶段，提升“完成任务”的可靠性：

- 自动选择并运行相关测试。
- 解析测试失败日志。
- 限制 N 轮自动修复。
- 最终回答里报告验证结果。

第三阶段，提升“生产化”的可靠性：

- 代码索引和语义检索。
- 更完整 MCP resources/prompts。
- 更严格 sandbox 和 policy。
- eval harness 统计任务成功率。

一句话总结：

> 我不会一上来堆大功能，而是先补最影响真实 coding agent 成功率的两个核心：patch 可靠性和测试反馈闭环。然后再做检索、MCP 完整性、安全策略和 eval。

## 27. 你在项目里遇到过什么问题？

一个典型问题是 MCP 兼容性。

真实公开 memory MCP server 对 `tools/list` 的 `params: null` 没有正常响应。后来我手动用 JSON-RPC 验证，发现它对 `params: {}` 可以返回工具列表。

所以我把 GoCoder 的 nil params 编码成 `{}`，并加了真实公开 MCP server 的 opt-in 测试。

这个问题说明 MCP 不是只写一个 mock server 就够，还要用公开 server 做兼容性验证。

## 28. 面试时 1 分钟版本怎么讲？

GoCoder 是我用 Go 写的轻量级终端 Coding Agent，兼容 OpenAI 标准接口，可以接 GPT 和 DeepSeek。它的核心是一个多轮 Agent Loop：模型返回 tool calls，程序做权限审批后执行工具，再把 tool result 喂回模型继续推理。工具系统包括文件读写、grep、shell，也支持 MCP 接入外部工具。为了安全，我做了 workspace 边界、diff 预览、shell 风险识别和审批模型。为了可恢复和可解释，我用 JSONL 记录 message、tool_call、tool_result、permission 等事件。Skills 则用于把代码审查、发布流程这类流程知识注入 system prompt。整体定位是轻量但完整的 coding agent，而不是简单聊天 wrapper。

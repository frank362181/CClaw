# 多 Agent 协作（Coordinator）设计方案

> 状态：设计稿（未落地）
> 适用范围：`cclawservice` 聊天主链路
> 关联文档：[Agent Loop设计机制](./Agent%20Loop设计机制.md)、[工具权限分级设计方案](./工具权限分级设计方案.md)、[prompt_cache原理分析](./prompt_cache原理分析.md)

---

## 0. 结论先行

不另起一套"多 Agent 框架"，而是把 **Coordinator 做成一种 Agent**，通过 **一个内置委派工具 + 一层调度器**，让子 Agent 复用现有的 `Preparer + ToolLoop` 执行栈。

```
一次用户请求
  └─ Coordinator（根 Agent，每轮必经）
       ├─ 自己回答        → 1 次 LLM 调用（与现状等价）
       └─ 委派 N 个 Worker → 并行/串行子 run → 汇总作答
```

三条不可动摇的设计原则：

1. **子 Agent 复用现有执行栈**，不新写一套 LLM + 工具循环；
2. **一次用户对话 = 一份会话历史 + 一棵运行树**，子 Agent 的中间过程不进父历史；
3. **父只消费子 Agent 的摘要**，全文按需惰性读取，防止父上下文线性膨胀。

---

## 1. 已确认的设计决策

| # | 决策项 | 结论 | 影响 |
|---|---|---|---|
| D1 | Coordinator 触发策略 | **统一入口**：所有请求先过 coordinator | 需要"直答快路径"、路由降级为"提示"、失败必须回退旧路由 |
| D2 | 递归深度 | **固定两层**：coordinator → worker，worker 不可再委派 | `delegate_task` 只注入 coordinator；候选集排除 coordinator |
| D3 | 子 Agent 写权限 | **默认只读**（可写入独立产物目录，例外见 §9.3） | 需要工具过滤策略；产物目录隔离 |
| D4 | 产出文档 | 本文档 | — |

---

## 2. 现状盘点（决定方案边界的硬约束）

| 事实 | 位置 | 对方案的含义 |
|---|---|---|
| 一轮对话 = 路由一次 → 一个 Agent → 一个 `TurnState` → 一个 ToolLoop（≤100 轮） | [chatting_service.go](../src/cclawservice/service/chatting_service.go)、[preparer.go](../src/cclawservice/chat/preparer.go)、[tool_looper.go](../src/cclawservice/chat/tool_looper.go) | 必须把"跑一个 Agent 的一轮"抽成可递归用例 |
| 路由 5 级回落：显式 > 会话绑定 > 关键词 > LLM 分类 > 默认 | [agent_router.go](../src/cclawservice/harness/ctxmgr/agent_router.go) | 机制保留，但角色从"选根 Agent"改为"给 coordinator 提供提示 + 逃生通道" |
| 会话绑定 `sessionId → agentId`，TTL 1h，内存实现 | [session_agent_mgr.go](../src/cclawservice/harness/ctxmgr/session_agent_mgr.go) | 绑定语义改为"绑定**根** Agent"；子 Agent 禁止写绑定 |
| **所有工具最终都以 python3 脚本执行**（技能工具 + 系统工具） | [tool_executor_core.go](../src/cclawservice/harness/toolexecutor/tool_executor_core.go)、[script_executor_simple.go](../src/cclawservice/harness/toolexecutor/script_executor_simple.go)、`bin/cclaw/systemtool/*/scripts/*.py` | **最大技术阻碍**：委派必须在进程内调用 Go 用例，脚本做不到 → 需要"内置工具"通道 |
| 同一轮的多个 tool_calls 已并行执行（限流 12，事件串行汇总） | [tool_executor_parallel.go](../src/cclawservice/harness/toolexecutor/tool_executor_parallel.go) | **不需要新造并行**：一次吐 k 个 `delegate_task` 即为 k 路并行 |
| `Session.HasHistory()==false` 时不读历史 | [assembler.go](../src/cclawservice/chat/assembler.go) | 子 run 只要不记账即天然上下文隔离，**零改动** |
| 记账是 session 维度单文件 + `t_turn`；`t_session.agent_id` 建档时写一次 | [memory_service.go](../src/cclawservice/harness/ctxmgr/memory_service.go) | 子 run 若走 Journal 会污染父会话历史与长期记忆 → 子 run 必须走独立存储 |
| 并发闸按 `agentId + max_concurrent` | [chatting_service.go](../src/cclawservice/service/chatting_service.go) | 闸 key 必须分命名空间，否则父子同名 Agent 自锁 |
| `t_agent.subagents`（可派生上限）DDL 已存在但未被使用 | [ddl_sqlite3.sql](../src/data/ddl_sqlite3.sql) | 直接复用，不新增同义字段 |
| SSE 由 `SSEPresenter.emit()` 加锁串行写；前端按 payload 键嗅探 | [sse_presenter.go](../src/cclawservice/controller/sse_presenter.go)、[api.js](../src/cclawchat/scripts/modules/api.js) | 并发送事件安全；但新事件必须避开 `status/delta/phase+name/code+msg/reply/msg` 键 |

---

## 3. 拓扑选型

| 形态 | 特点 | 匹配度 |
|---|---|---|
| Handoff（Swarm 式转交） | 单线程，控制权在 Agent 间移交 | 差：无并行，且与"会话绑定根 Agent"冲突；仅作兜底分支 |
| **Coordinator-Worker（中心调度）** | 中心拆解/派发/汇总，worker 无状态、上下文隔离 | **采用**：`delegate_task` 是工具，直接嵌入现有 ToolLoop |
| Graph / DAG | 显式节点 + 边，可回放、可续跑 | 成本最高，适合稳定多阶段流水线；列为 v2 |

---

## 4. 总体架构

```
ChatController.ChatStream
  └─ ChattingService.Stream
      └─ IAgentRunner.Run(rootReq)                    ← 新：可递归
          ├─ Preparer.PrepareForRun                   ← 新：显式 Agent、跳过路由/绑定
          ├─ 并发闸 / 中断登记 / Journal（仅 root）
          ├─ Assembler.Assemble（按 ContextMode）
          └─ ToolLoop.Run
               ├─ LLM ──> tool_calls
               ├─ IToolRoundExecutor（并行）
               │    ├─ 脚本工具 ──> SimpleScriptExecutor（现状不变）
               │    └─ 内置工具 ──> BuiltinRegistry      ← 新：delegate_task / read_agent_result
               └─ delegate_task
                    ├─ DelegationPolicy   白名单 / 深度 / 配额 / 预算 / 只读
                    ├─ Scheduler          信号量 / 排队 / 超时 / 取消传播
                    └─ IAgentRunner.Run(child)  → 递归（worker 不可再委派）
```

### 4.1 关键重构（P0，不改变任何行为）

把 `ChattingService.execute` 拆成：

```
execute = buildRootRunRequest() + runner.Run() + toReply()
```

`Preparer` 新增：

```go
PrepareForRun(ctx context.Context, req AgentRunRequest) (*TurnState, error)
```

- 不调用 `AgentRouter`（Agent 已由外层给定）
- 不 bind 会话
- 按 `ContextMode` 决定 system prompt 是否附加编排手册 / 父摘要
- 按 `AllowDelegate` 决定是否注入 `delegate_task`

原 `Prepare(ctx, cmd)` 保留为薄包装（内部先 `Resolve` 再 `PrepareForRun`），保证旧路径零变化。

---

## 5. 核心抽象

```go
// cclawservice/chat/agent_runner.go
type ContextMode int

const (
    CtxIsolated      ContextMode = iota // 默认：只有自己的启动文档 + 任务书
    CtxParentSummary                    // + 父的近 N 轮摘要
)

type AgentRunRequest struct {
    RunID, NodeID, ParentNode string
    Depth                     int

    ComCode, Sender, SessionID, TurnID string

    Agent       dataobject.TAgent // 已解析；子 run 由父指定
    Input       string            // 用户消息 / task brief
    ContextMode ContextMode
    ParentBrief string            // 父摘要 / 上游结果
    Budget      RunBudget
    Sink        ISink             // 带 node 维度的 sink
    Streamed    bool

    SkipJournal   bool // 子 run 恒为 true
    AllowDelegate bool // 仅 coordinator 且 depth==0 时为 true
}

type AgentRunResult struct {
    NodeID, AgentID string
    Content   string // 完整答复（落 t_run_node）
    Summary   string // 结构化摘要（父只消费这个）
    Artifacts []Artifact
    Usage     Usage
    Status    RunStatus // ok / failed / canceled / budget_exceeded / need_input
    Err       error
}

type IAgentRunner interface {
    Run(ctx context.Context, req AgentRunRequest) (AgentRunResult, error)
}
```

`RunBudget`：

```go
type RunBudget struct {
    MaxNodes      int           // 单 run 最多节点数
    MaxChildren   int           // 单轮最多并行子任务
    NodeMaxTokens int           // 单节点 token 上限
    Deadline      time.Time     // 由 root run 超时派生
}
```

---

## 6. 内置工具通道（技术关键点）

现状：工具执行链路 `executorCore.executeOne → env.Spec() → IScriptRunner.Run(script)`，一律起 python 进程。

三种接入方式对比：

| 方案 | 做法 | 评价 |
|---|---|---|
| A. 委派也写成 python 脚本，回调内部 HTTP | 脚本 → 本地 HTTP → 内部 API | 需新增内部服务、序列化与超时链路，且拿不到 `Sink`，**否** |
| B. **给工具执行器加“内置工具”分支** | `ToolSpec.Kind = script/builtin`；`ToolEnv` 注入 `BuiltinRegistry` | **推荐**：与现有 ledger / 权限 / 事件完全同构 |
| C. 装饰 `IToolRoundExecutor`，按名字拦截 | 在 chat 层包一层 | 可作为 1 天内的技术验证（spike），但绕过 ledger 与权限，不作最终形态 |

方案 B 的改动面：

```go
// toolexecutor/builtin_tool.go
type ToolKind int
const (
    ToolKindScript ToolKind = iota
    ToolKindBuiltin
)

type ToolCallContext struct {
    Scope    string
    WorkRoot string
    RunID    string
    NodeID   string
    Sink     ToolEventSink
    Emit     func(phase, detail string) // 进度
}

type IBuiltinTool interface {
    Name() string
    Spec() ToolSpec                       // schema 由 Go 注入，无需 SKILL.md / t_tool
    Call(ctx context.Context, args map[string]any, cc ToolCallContext) (string, error)
}

type BuiltinRegistry interface {
    Resolve(name string) (IBuiltinTool, bool)
}
```

`executorCore.executeOne` 在 `env.Spec()` 后按 `Kind` 分派；权限仍走 `env.AdmitRule`，台账仍走 `ledger.Start/Finish`，进度仍走 `sink.OnTool`。

**好处**：`delegate_task` 的 schema 完全由代码控制，不受技能/数据库配置漂移影响；且能拿到 `Sink` 做 UI 事件。

---

## 7. 委派协议

### 7.1 入参

```json
{
  "agent": "agent_001",
  "objective": "对《XX资产包2024年度报告》做财务尽调，输出科目异常点清单",
  "inputs": [
    {"type": "file", "path": "reports/xx2024.pdf"},
    {"type": "node_result", "node_id": "n_03"},
    {"type": "text", "value": "关注：应收账款、关联方往来"}
  ],
  "expected_output": "markdown 表格：科目 | 金额 | 同比 | 异常点",
  "constraints": {"timeout_sec": 180, "max_tokens": 8000},
  "mode": "join"
}
```

- `objective` **必须自包含**（子 Agent 看不到父对话），prompt 中硬性禁止"如上/刚才"等指代。
- `agent` 只接受候选清单中的 AgentId，做**精确匹配校验**（与现有 LLM 分类同样的严格性，绝不信任自由文本）。
- `mode`：`join`（默认，等待结果）/ `handoff`（父终止，子结果即最终答复）。

### 7.2 返回值（回填父上下文的 tool 消息，必须有硬上限）

```json
{
  "node_id": "n_07",
  "agent": "agent_001",
  "status": "ok",
  "summary": "≤1200 字（唯一被父消费的正文）",
  "key_points": ["应收账款周转天数同比 +41%", "..."],
  "artifacts": [{"type": "file", "path": "out/20261106/n_07/尽调.md"}],
  "usage": {"tokens_in": 5210, "tokens_out": 1830, "elapsed_ms": 18400},
  "confidence": 0.82,
  "full_ref": "node:n_07"
}
```

### 7.3 配套内置工具

| 工具 | 作用 |
|---|---|
| `read_agent_result(node_id, section?)` | 惰性拉取全文/片段，避免父窗口爆炸 |
| `list_agent_results()` | 本次 run 的节点清单（父在最终汇总时使用） |

### 7.4 幂等与失败语义

- **幂等**：`idem_key = hash(parent_node + agent + objective + inputs)`；同 key 重复派发直接返回已有 node，不重复扣费。
- **失败**：`status=failed` + `error_code`；父必须显式决策（重试 / 换人 / 自答）。prompt 明令禁止"忽略失败结果继续编"。
- **空结果**：`summary` 为空或低于阈值（如 50 字）视为失败，防止父基于空结果编造。

---

## 8. 数据模型

```sql
-- 1) t_agent 扩展（复用已有 subagents 列，不新增同义字段）
ALTER TABLE t_agent ADD COLUMN agent_role   INTEGER NOT NULL DEFAULT 0; -- 0=worker 1=coordinator
ALTER TABLE t_agent ADD COLUMN can_delegate INTEGER NOT NULL DEFAULT 0; -- 是否可派生（worker 默认 0）
ALTER TABLE t_agent ADD COLUMN max_depth    INTEGER NOT NULL DEFAULT 1; -- 固定两层，保留字段便于将来放开
ALTER TABLE t_agent ADD COLUMN max_children INTEGER NOT NULL DEFAULT 3; -- 单轮最多并行子任务
ALTER TABLE t_agent ADD COLUMN max_nodes    INTEGER NOT NULL DEFAULT 12;-- 单 run 最多节点数

-- 2) 委派白名单（父 → 子）
CREATE TABLE IF NOT EXISTS t_agent_delegation (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id    VARCHAR(16) NOT NULL,
    parent_id   VARCHAR(8)  NOT NULL,
    child_id    VARCHAR(8)  NOT NULL,
    priority    INTEGER NOT NULL DEFAULT 0,
    description VARCHAR(256),                       -- 给 coordinator 的"何时用它"，进 prompt
    disabled    INTEGER NOT NULL DEFAULT 0,
    deleted     INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_agent_delegation ON t_agent_delegation(parent_id, child_id);

-- 3) 运行实例（一次用户请求 = 一个 run）
CREATE TABLE IF NOT EXISTS t_run (
    run_id        VARCHAR(40) PRIMARY KEY,
    session_id    VARCHAR(40) NOT NULL,
    turn_id       VARCHAR(40) NOT NULL,
    root_agent_id VARCHAR(8)  NOT NULL,
    status        INTEGER NOT NULL DEFAULT 0,   -- 0 running / 1 ok / 2 failed / 3 canceled / 4 budget
    begin_ts      INTEGER NOT NULL,
    end_ts        INTEGER,
    nodes         INTEGER DEFAULT 0,
    tokens_in     INTEGER DEFAULT 0,
    tokens_out    INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_run_session ON t_run(session_id, begin_ts);

-- 4) 运行树节点（子 Agent 的完整痕迹 + 审计）
CREATE TABLE IF NOT EXISTS t_run_node (
    node_id     VARCHAR(40) PRIMARY KEY,
    run_id      VARCHAR(40) NOT NULL,
    parent_node VARCHAR(40),
    agent_id    VARCHAR(8)  NOT NULL,
    depth       INTEGER NOT NULL,
    objective   VARCHAR(1024) NOT NULL,
    status      INTEGER NOT NULL DEFAULT 0,
    begin_ts    INTEGER, end_ts INTEGER,
    summary     VARCHAR(4096),
    detail_ref  VARCHAR(256),        -- 全文：文件路径或大字段引用
    tokens_in   INTEGER, tokens_out INTEGER,
    error_code  VARCHAR(64),
    idem_key    VARCHAR(64)
);
CREATE INDEX IF NOT EXISTS idx_run_node_run ON t_run_node(run_id, parent_node);
CREATE INDEX IF NOT EXISTS idx_run_node_idem ON t_run_node(run_id, idem_key);
```

### 8.1 为什么子 Agent 不写 `t_turn`

现有 `MemoryService.AppendTurn` 会把内容追加进**同一会话历史文件**，并且 `t_session.agent_id` 只有一个值。子 Agent 若走 Journal：

- 父下一轮的 replay 会看到子 Agent 的问答（上下文污染）；
- 长期记忆蒸馏会把子 Agent 的中间过程写进 MEMORY.md；
- `t_turn_tool` 的归属变模糊。

因此：

- **父**：`t_session / t_turn / t_turn_tool` 照常。`delegate_task` 作为一条 tool 记录，`tool_resp` 就是 summary —— **父历史里天然带委派摘要**，无需额外机制。
- **子**：只写 `t_run_node`，不碰会话历史。

---

## 9. 上下文与预算

### 9.1 三层预算

| 层级 | 约束 | 超限行为 |
|---|---|---|
| Run | 总 token / 总时长 / 总节点数 | 停止派发，父基于已有结果作答，附 `notice: partial_result` |
| Node | 单子任务 token / 超时 | 该节点 `budget_exceeded`，父可重试或降级 |
| Depth | 固定 1 层（D2） | 拒绝派发并返回结构化错误 |

### 9.2 上下文隔离

- 子 run 默认 `CtxIsolated`：`Session.Recorded=false` → `HasHistory()==false` → 不读父历史（**现有代码零改动即生效**）。
- `CtxParentSummary`：父的压缩摘要 + 任务书，注入 system prompt 的 `DynamicMemory` 段；**不得**把父的 tool_calls 协议消息塞进去（上游会判协议错）。
- 回程压缩：父只吃 `summary`（工具层按 `ToolsReserveTokens` 口径强截断），全文走 `full_ref` + `read_agent_result`。

### 9.3 只读策略的落地（D3）

子 Agent 默认只读，但"只读"必须定义清楚：

| 能力 | 默认 | 说明 |
|---|---|---|
| 读取工作区文件 | 允许 | 路径必须落在自己的 bootstrap root（`cclaw/<owner>/<agentDir>`）内 |
| 写入工作区源文件 | **禁止** | `write / edit / bash` 等 `Sandboxed|Shelled` 工具不下发 |
| 写入**产物目录** | 允许 | 仅限 `out/<runId>/<nodeId>/`，作为 exception 白名单下发 |
| 跨 Agent 读文件 | 需显式传参 | 只能通过 `inputs.file` + `SafeJoinWithRoot` 校验 |

实现：在 `ToolsBundle` 增加过滤步骤 `FilterByPolicy(policy)`，按 `ToolSpec.ReadOnly / Sandboxed / Shelled` 裁剪；`delegate_task` 的 `ToolCallContext` 携带 `OutputDir` 与 `ReadOnly` 传给子 run 的 `Preparer`。

> 风险提示：很多技能工具未显式声明 `read-only:true`，默认只读会导致子 Agent 工具面变窄。建议在实施时统计各 agent 的 `ToolSpec.ReadOnly` 覆盖率，必要时为 `t_agent_delegation` 增加 `tool_policy`（`inherited | read_only | allowlist`）列。

---

## 10. 调度、并发、中断、守卫

### 10.1 Scheduler

- 复用 `concurrency.GetTaskManager()` 的 scope：`run:<runId>:nodes`，宽度 = 剩余节点配额。
- **闸 key 分命名空间**：root 用 `agent:{id}`；子节点用 `agent:{id}:sub`，配额取 `t_agent.subagents`。否则 coordinator 因自身并发额被占满而等待子节点 → 自锁。
- 一次 assistant 消息里的 k 个 `delegate_task` 天然并行（现有 `maxToolFanout=12`），无需额外 batch 工具。

### 10.2 中断级联

- 子 `runCtx` 全部从 root `runCtx` 派生 → 现有 `TurnRegistrator` 取消 root 即自动级联，**无需扩展登记表**。
- 需要补的：Scheduler 在取消时 drain 所有在飞节点，回写 `t_run_node.status = canceled`。

### 10.3 委派守卫（DelegationGuard）

沿用现有 LoopGuard 的设计风格（每次 run 新建实例，禁止全局状态）：

| 规则 | 动作 |
|---|---|
| 同一 `idem_key` 重复派发 | 返回已有 node 结果，不扣费 |
| 同一 `objective` 语义重复（哈希/相似度） | 拒绝并提示复用 node |
| 同一 agent 连续失败 ≥ N | 本轮熔断该 agent |
| "派发→等待→再派发" 超 K 轮 | notice 提示模型收敛 |
| 子结果为空/过短 | 判失败 |
| 节点数 / 深度 / 并行宽度越界 | 拒绝并返回结构化错误 |

---

## 11. 记忆与审计

- **会话历史**：只有 root 写；子 run `SkipJournal=true`。
- **长期记忆蒸馏**：只读 root session → 子 Agent 中间过程不会进 MEMORY.md。若希望"结论沉淀"，父历史中的 `tool_resp(summary)` 已经承载了结论。
- **审计/回放**：`t_run` + `t_run_node` 提供节点、状态、摘要、用量、错误码；`detail_ref` 指向全文。
- **清理**：按 `run.begin_ts` 做 TTL（如 30 天），与现有 `t_turn` 清理策略对齐。

---

## 12. 提示词设计

### 12.1 Coordinator（编排手册，追加在 7 个启动文档之后）

1. **直答优先**：一次能答完的不要委派（降低延迟与成本）。
2. **何时委派**：需要不同专业领域 / 需要并行取证 / 需要长文档分段处理。
3. **objective 写法**：自包含，含目标、范围、输入材料、输出格式、约束；禁止指代。
4. **并行**：互不依赖的必须同批派出；有依赖的分轮。
5. **汇总纪律**：只能引用返回的 `summary / key_points`；失败必须显式说明；不得编造。
6. **预算意识**：剩余节点/预算，何时收手并给部分结论。

### 12.2 候选子 Agent 清单

复用现有摘要优先级（AGENTS.md front matter `summary` > DB `description` > `agent_name`），再叠加 `t_agent_delegation.description`。

**只注入"ID + 一句话 + 何时用"，不注入子 Agent 的启动文档**（否则 coordinator 的 system prompt 先炸）。

```markdown
## 可用协作方
- agent_001（不良资产智能助手）：尽调/估值/处置类问题专用。何时用：涉及债权、抵押物、资产包。
- agent_002（财务报表分析）：财报科目级异常识别。何时用：需要逐科目比对。
```

### 12.3 前缀缓存注意

系统提示每轮重建，因此**候选清单必须按稳定顺序（priority, agentId）排序**，避免无谓的缓存前缀变化（参见 [prompt_cache原理分析](./prompt_cache原理分析.md)）。

### 12.4 Worker

- 单任务、不得反问用户（阻塞时返回 `need_input` 结构化信号，由父汇总后统一问用户）；
- 必须按 `expected_output` 输出，附 ≤N 字摘要；
- 明确告知"你看不到父对话，只能使用任务书中的材料"。

---

## 13. 路由改造（统一入口 D1）

`AgentRouter.Resolve` 的语义从"选根 Agent"改为"产出 RouteDecision":

```go
type RouteSource int
const (
    RouteExplicit RouteSource = iota // UI 显式指定
    RouteSession                     // 会话绑定
    RouteKeyword                     // 关键词规则
    RouteClassify                    // LLM 分类
    RouteDefault                     // 默认
)

type RouteDecision struct {
    Agent  dataobject.TAgent
    Source RouteSource
    Hint   string // 供 coordinator 参考的路由提示（"本问题可能适合 agent_001"）
}
```

| 场景 | 决策 | 说明 |
|---|---|---|
| UI 显式选 Agent | 直接用该 Agent（**逃生通道**） | 保持现有行为；用户可绕过 coordinator |
| 会话已绑定 | 用绑定的根 Agent | 绑定只记根 Agent |
| 关键词命中 | **默认降级为 Hint** 注入 coordinator | 配置 `keyword_rule_mode: hint`（默认）/ `direct`（恢复旧行为） |
| LLM 分类 | **默认降级为 Hint** | 复用现有分类器，输出改为"建议"，不再强制路由 |
| coordinator 不可用 | **回退旧 5 级路由** | 可用性兜底：禁用/删除/无 provider 时按原逻辑选 Agent |
| 子 run | **不路由、不绑定** | 由父显式指定 Agent |

### 13.1 为什么保留"显式 > 统一入口"

- 可用性：coordinator 配置错误时不至于全站不可用；
- 调试：出问题时可直接定位到某个 worker；
- 兼容：现有 UI 的 Agent 选择器语义不变。

### 13.2 直答快路径

统一入口下，**简单问答的成本必须与现状等价**（1 次 LLM 调用）。因此：

- coordinator 的 system prompt 只增加"编排手册 + 候选清单"，不加载子 Agent 文档；
- 模型分层：coordinator 用强模型、worker 用廉价模型（`primary_model` / `fallbacks` 已支持）；
- 若实测 coordinator 过度委派，再加一层"直答判定"轻量分类（仅输出 `self | delegate`），作为可选开关。

---

## 14. UI / SSE 协议

前端按 payload 键嗅探（`api.js` 的 `handleEvent`），因此新事件统一用 `type` 区分，**不占用** `status / delta / phase+name / code+msg / reply / msg` → 旧前端静默忽略，天然向后兼容。

```
{"type":"run.start","run_id":"...","root_agent":{...},"budget":{...}}
{"type":"agent.enter","node_id":"n_2","parent_node_id":"n_1","agent":{"id","name","emoji"},"depth":1,"objective":"..."}
{"type":"agent.status","node_id":"n_2","phase":"thinking|tool|waiting|done","text":"..."}
{"type":"agent.exit","node_id":"n_2","status":"ok|failed|canceled","summary":"...","usage":{...}}
{"type":"run.usage","tokens_in":...,"tokens_out":...,"nodes":5,"elapsed_ms":...}
```

- 现有 `tool` 事件增加可选 `node_id`（root 省略，旧前端无感）。
- 子 Agent 正文默认不转发；`CoordinatorStreamMode: silent | summary | full`，`full` 时使用 `{"type":"agent.delta","node_id","reply"}`。
- 刷新恢复：`GET /api/run/{runId}/tree` 读 `t_run` / `t_run_node`。
- 并发安全：`SSEPresenter.emit` 已加锁串行写，多子节点共用同一 sink 安全。
- 统一入口下**每轮都有树**（简单问答为单节点），UI 需要能优雅降级为"仅 coordinator 一节点"。

---

## 15. 可观测性与评测

- **日志 scope**：现有 `agent=<id>,turn=<id>` 扩展为 `run=<id>,node=<id>,agent=,turn=`。
- **计时**：`TurnTiming` 升维为 run 级树（每节点 route/skill/prompt/llm/tools ms + tokens），沿用 `observatory.WithTurn` 思路加 `WithNode`。
- **指标**：委派率、并行宽度、节点成功率、父上下文占比、单 run 成本、coordinator 直答占比。
- **评测集**（必须含负样本）：
  - 简单问答的委派率（目标 < 10%，统一入口下这是核心 KPI）；
  - 多领域任务完成率 vs 单 Agent 基线；
  - 延迟与成本三元组对比；
  - 中断 / 预算耗尽的降级表现；
  - coordinator 不可用时的回退正确性。

---

## 16. 实施路线图

| 阶段 | 内容 | 验收标准 | 预估 |
|---|---|---|---|
| **P0 抽象** | 抽 `IAgentRunner` / `AgentRunRequest`；`execute` 改薄；行为零变化 | 现有单测全绿 + smoke 结果一致 | 1–2 天 |
| **P1 内置工具通道** | `ToolSpec.Kind` + `BuiltinRegistry`；`delegate_task`（串行、单层）；`t_run_node` 落库 | 手工构造 coordinator → 单子任务 → 结果回填 | 2–3 天 |
| **P2 统一入口** | `RouteDecision` + Hint 降级 + 回退旧路由；coordinator prompt；候选清单注入 | 简单问答 1 次 LLM 调用；coordinator 禁用时旧路由生效 | 3–5 天 |
| **P3 并行与护栏** | 并行派发、Scheduler 配额、RunBudget、DelegationGuard、中断级联、幂等 | 3 子任务并行；中断父→子全停；超预算降级；重复派发不扣费 | 3–5 天 |
| **P4 UI 与审计** | SSE 扩展、前端 Agent 树/时间线、`/api/run/{id}/tree`、`t_run` 落库 | 刷新可恢复树；每节点可查摘要与用量 | 3–5 天 |
| **P5 调优与评测** | 上下文模式、直答判定、只读策略调优、评测集 | 基线对比报告（成功率/延迟/成本） | 持续 |

---

## 17. 风险清单

| 风险 | 触发条件 | 缓解 |
|---|---|---|
| 统一入口拖慢简单问答 | coordinator 用强模型 + prompt 变大 | 直答优先规则、模型分层、直答判定开关；KPI 监控委派率 |
| 成本失控 | 并行宽 + 父上下文膨胀 | RunBudget 硬上限、节点 token 上限、超限降级而非重试 |
| 递归/节点爆炸 | worker 被误配置为可委派 | D2 固定两层；`AllowDelegate = role==coordinator && depth==0`；候选集排除 coordinator |
| 上下文污染 | 子 Agent 写同一 session 历史 | 子 run `SkipJournal`，只落 `t_run_node` |
| 父模型编造子结论 | 子失败但父照常作答 | 工具返回 `status`；prompt 强制引用 summary；空结果判失败 |
| 并发自锁 | 父子同名 Agent 抢同一闸 | 闸 key 分命名空间；子配额用 `subagents` |
| 只读导致子 Agent 能力不足 | 技能工具未声明 read-only | 产物目录例外 + `t_agent_delegation.tool_policy` |
| 并行写冲突 | 多子 Agent 同时写文件 | 默认只读 + 独立产物目录 + 写操作串行化 |
| 跨 Agent 越权读文件 | `inputs.file` 传入任意路径 | `SafeJoinWithRoot` 白名单校验 |
| 调试困难 | 树形调用栈 | run/node 贯穿日志 + 落库 + 回放接口 |
| 旧前端不兼容 | 新 SSE 事件 | 只用 `type` 键，不占用现有嗅探键 |
| 前缀缓存失效 | 候选清单顺序不稳定 | 按 (priority, agentId) 稳定排序 |

---

## 18. 待确认项（实施前）

1. **`keyword_rule_mode` 默认值**：`hint`（推荐）还是 `direct`（保留现有直通）？
2. **coordinator 的模型**：与 worker 同一模型，还是固定使用强模型？
3. **只读例外白名单**：是否允许子 Agent 写 `out/<runId>/<nodeId>/`？产物如何回到父（路径引用 / 内容摘要）？
4. **子 Agent 需要用户输入时**：本轮直接失败，还是暂停等待（需要持久化的"待输入"状态）？建议 v1 直接返回 `need_input` 由父汇总提问。
5. **`t_run_node.detail_ref` 存储形态**：独立文件（`data/run/<runId>/<nodeId>.md`）还是 DB 大字段？
6. **UI 是否需要"计划先行"**：coordinator 在首次委派前先输出结构化计划（`plan` 事件），提升可解释性，但会多一轮输出。

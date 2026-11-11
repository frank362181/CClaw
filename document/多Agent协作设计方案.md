# 多 Agent 协作（Coordinator → Worker）设计方案

> 状态：设计稿（不落地）
> 目标版本：`3.0.0`（对应 [initialization.go](../src/agentService/initialization.go) 的 `feature 1: coordinator-worker 架构支持`）
> 适用范围：`cclawservice` 聊天主链路
> 关联：[Agent Loop设计机制](./Agent%20Loop设计机制.md)、[prompt_cache原理分析](./prompt_cache原理分析.md)、[工具权限分级设计方案](./工具权限分级设计方案.md)

---

## 0. 结论先行

> **已定：采用方案 A（路由内规划）。** 简单问题继续走现有路由，复杂问题在路由的第 ⑤ 段升级为"规划"。简单/复杂的判定见 [简单复杂问题判定设计](./简单复杂问题判定设计.md)。

现有机制（**显式 > 会话绑定 > 关键词 > 意图分类 > 默认兜底**）**一行不改**。Coordinator 只是把第 ⑤ 段"输出一个 AgentId"升级为"输出 single 或 plan"：

```
                    现有路由（前四段不动）
用户消息 ──▶ ① 单Agent短路 → ② 显式 → ③ 会话绑定 → ④ 关键词 ─┬─▶ ⑤ 分诊（1 次调用）
                                                              │      ├─ mode=single → ⑥ 直接执行该 Agent（今天的行为）
                                                              │      └─ mode=delegate
                                                              │              │
                                                              │              ├─ 分层并行派发 Worker
                                                              │              └─ 汇总（Reporter turn）
                                                              │
                                                              └─▶ ⑥ 兜底（default Agent）
                                                                     ▼
                                                          写一条 t_turn（最终答复）
```

三个不变量：

1. **一次用户提问仍然只产生一条 `t_turn`** —— 历史只有一套，Worker 的执行过程不进会话历史。
2. **简单问题的路径、成本、延迟与今天完全一致** —— 第 ⑤ 段本来就要调用一次，只是输出协议变了。
3. **任何新能力都有开关，且关掉后行为逐字节等于今天**。

> 与旧稿（形态 B：coordinator 是一条 `t_agent` 记录）的差别：方案 A 不引入新的 Agent 实体，因此不需要 `agent_role` 列、不需要 coordinator 的启动文档与技能配置、也不会出现"协作轮多一次调用"。代价是 coordinator 在 UI 上没有独立身份（用计划卡片呈现）。

---

## 1. 现状锚点（本方案的所有接缝都基于这些事实）

| # | 事实 | 位置 |
|---|---|---|
| 1 | 路由 6 段优先级 | [agent_router.go:66](../src/cclawservice/harness/ctxmgr/agent_router.go:66) |
| 2 | `len(agents)==1` 直接短路 | 同上（`Resolve` 第 0 段） |
| 3 | 显式 agentId 命中即绑定，未命中报错 | 同上（第 1 段） |
| 4 | 会话绑定是**内存**注册表，TTL 1h | [session_agent_mgr.go](../src/cclawservice/harness/ctxmgr/session_agent_mgr.go) |
| 5 | 关键词规则来自 `t_agent_route_rule`，命中即短路 | [agent_router.go:156](../src/cclawservice/harness/ctxmgr/agent_router.go:156) |
| 6 | 意图分类 = 一次 `SimpleComplete`，输出**一个 AgentId**，严格精确匹配 | [agent_router.go:189](../src/cclawservice/harness/ctxmgr/agent_router.go:189)、[context_assembler.go:187](../src/cclawservice/harness/ctxmgr/context_assembler.go:187) |
| 7 | **UI 已把 `t_session.agent_id` 回传为 `agentId`**（历史会话续用原 Agent） | [api.js:14](../src/cclawchat/scripts/modules/api.js:14)、[t_chat.go](../src/cclawservice/model/dataobject/t_chat.go) |
| 8 | 一轮 = `Prepare`（路由/provider/技能/prompt/工具）→ 并发闸 → 建档 → 装配 → 调模型（ToolLoop 或单次）→ 收尾记账 | [chatting_service.go:185](../src/cclawservice/service/chatting_service.go:185)、[preparer.go:66](../src/cclawservice/chat/preparer.go:66) |
| 9 | `Session.Recorded=false` ⇒ `HasHistory()=false` ⇒ **不读历史**（天然上下文隔离） | [chat_common.go](../src/cclawservice/chat/chat_common.go)、[assembler.go:31](../src/cclawservice/chat/assembler.go:31) |
| 10 | 每 Agent 并发闸 key = `agentId`，上限 `MaxConcurrents` | [chatting_service.go:206](../src/cclawservice/service/chatting_service.go:206) |
| 11 | `t_agent.subagents`（可派生上限）**已存在但未使用** | [ddl_sqlite3.sql](../src/data/ddl_sqlite3.sql) |
| 12 | 裸 `go` 语句被 AST 测试静态禁止 | [goroutine_test.go:23](../src/cclawservice/archtest/goroutine_test.go:23) |
| 13 | `taskScope.Wait()` 会把 scope **永久置为 draining** | [task_scope.go:45](../src/platformutility/concurrency/task_scope.go:45) |
| 14 | `ConcurrentAdvance` 的 limit 由**该 key 首次调用**决定 | [concurrency_advance.go:29](../src/platformutility/concurrency/concurrency_advance.go:29) |
| 15 | 闸位等待上限 10s，超时返回错误 | [concurrency_controller.go:22](../src/platformutility/concurrency/concurrency_controller.go:22) |
| 16 | SSE 由 `emit` 加锁串行写；前端**按 payload 键嗅探** | [sse_presenter.go:243](../src/cclawservice/controller/sse_presenter.go:243)、[api.js](../src/cclawchat/scripts/modules/api.js) |
| 17 | 工具轮次策略已改名为 `IRoundTool` / `TurnState.TurnTool`（原 `IRoundPlanner`/`Plan`，见 `43a53b0`），**`Plan` 一词已空出** | [chat_common.go:56](../src/cclawservice/chat/chat_common.go:56)、[turn_state.go:25](../src/cclawservice/chat/turn_state.go:25) |

---

## 2. 关键判断：把"意图识别"这次调用复用成"规划"调用

今天的第 ⑤ 段是：**一次 LLM 调用 → 输出一个 AgentId**。

协作所需要的"理解意图 + 生成计划"，本质上是**同一次调用换一种输出格式**：

| | 今天 | 协作后 |
|---|---|---|
| 调用次数 | 1 | 1 |
| 输入 | 用户消息 + 候选 Agent 精简目录 | 同左（+ 能力标签） |
| 输出 | `agent_001` | `{"mode":"single","agent":"agent_001"}` 或 `{"mode":"delegate","tasks":[...]}` |

所以**简单问题的成本与延迟完全不变**（都是"1 次分类/规划 + 1 次回答"）。这是本方案能落地的前提：协作能力不是靠"多加一层"，而是靠**复用已有的那次调用**。

```go
// 今天
type ClassifyAgent func(ctx, llm, system, user string, maxTokens int) (LlmReply, error)

// 协作后（签名完全一样，只是 prompt 与输出协议升级）
// 解析失败时的三级退化保证不倒退，见 §6.3
```

---

## 3. 接入形态：已选 A

| 形态 | 说明 | 判定 |
|---|---|---|
| **A 路由内规划** | 第 ⑤ 段输出 `single \| delegate` | **采用**（见 §0），判定细则见 [简单复杂问题判定设计](./简单复杂问题判定设计.md) |
| B Coordinator 是一种真实 Agent | `t_agent.agent_role=1`，路由选中它后再规划 | 未采用：协作轮多一次调用、需要新增 Agent 实体与启动文档 |
| C A+B 双入口 | 自动走 A、显式走 B | 未采用：两套 prompt/解析/降级要同步维护 |

### 3.1 A 的三个代价与各自的处置

| 代价 | 处置 |
|---|---|
| coordinator 在 UI 上没有"一个人" | 用**计划卡片**呈现（`plan` / `task` 事件），显示意图、任务、每个任务由哪个 Agent 执行 |
| 无法通过"显式选择某个 Agent"进入协作 | 保留**逃生通道**：UI 提供"深度分析"开关 → `forceMode=delegate`，等价于显式要求协作 |
| 协作会话的续用语义 | 用 `t_session.routing_mode` 表达"本会话的入口是规划器"（详见判定设计 §7） |

---

## 4. 执行链（形态 A）

```
ChattingService.execute
  ├─ 1  Preparer.Prepare
  │      └─ AgentRouter.ResolveDecision      ★ 唯一改动点
  │           ├─ L0 硬分流（单Agent/显式/绑定/关键词）→ decision.Mode=single
  │           ├─ L1 结构强信号                        → decision.Mode=delegate（强制）
  │           └─ L2 分诊调用（1 次，复用 classifyByLLM 的调用位）→ single | delegate
  │
  ├─ 2  每 Agent 并发闸（root: agent:<id>）    ← 原样
  ├─ 3  中断登记 / 运行超时                     ← 原样
  ├─ 4  Journal.Begin（建档）                  ← 原样（此时决定 routing_mode）
  │
  ├─ 5  ★ 分支：decision.Mode
  │      ├─ single → 6..9 原样（今天的路径，代码一行不动）
  │      └─ delegate →
  │             PlanExecutor.Run（§7）
  │               └─ 每个 Worker = 一次 "无记账 TurnRunner.Run"
  │             Reporter turn（§8，流式、写历史）
  │
  └─ 6 Assembler.Assemble → 7 invoke → 8 收尾记账（原样，作用于 Reporter）
```

要点：

- `ResolveDecision` 里 `mode=single` 的分支**已经选好了 Agent**，因此下游完全不必再判断；
- `Preparer.Prepare` 保留为 `ResolveDecision` 的薄包装（取 `decision.Agent`），现有调用点与测试不受影响；
- 因为分诊调用复用了原来分类调用的位置，**简单路径的调用次数不变**（1 次分诊 + 1 次回答）。

---

## 5. 契约：`ExecutionPlan`

### 5.1 命名

`43a53b0` 已把工具轮次策略改名为 `IRoundTool` / `TurnState.TurnTool`（原 `IRoundPlanner` / `TurnState.Plan`），因此 `Plan` / `Planner` 这两个词**已经空出来了**。但为了避免"任务计划"与"工具轮次"在读代码时再次混淆，本文仍建议：

| 新概念 | 名字 | 不许用 |
|---|---|---|
| 路由决策 | `RouteDecision`（放在 `harness/ctxmgr`） | — |
| 执行计划 | `ExecutionPlan` | 裸 `Plan`（语义太泛） |
| 计划中的任务 | `PlanTask` | `Task`（`concurrency` 包已有 `RoutineSpec`/`TaskStat` 语境） |
| 分诊（简单/复杂判定） | `ITriager` / `Triage` | `Planner`（易与工具轮次混淆） |
| 分派执行器 | `IPlanExecutor` | `Executor`（`toolexecutor` 已占用） |

> 方案 A 里"分诊 + 规划"是同一次 LLM 调用，因此只有一个组件 `ITriager`（它负责判定、并在 delegate 时一并产出计划），**不再单独存在 `ITaskPlanner`**。

### 5.2 结构

```go
// harness/ctxmgr/route_decision.go —— 方案 A 不引入 Agent 实体，只在路由层加"模式"
type RouteMode int

const (
    RouteModeSingle   RouteMode = iota // 今天的行为
    RouteModeDelegate                   // 走规划
)

type RouteDecision struct {
    Mode       RouteMode
    Agent      dataobject.TAgent // Mode=single 时的目标
    Plan       *ExecutionPlan    // Mode=delegate 时的计划（含 Reporter）
    Source     RouteSource       // disabled/forced/explicit/binding/keyword/struct/llm/fallback
    Intent     string
    Signals    ComplexitySignals
    Confidence float64
    ElapseMs   int64
    Note       string // 降级/偏离原因
}

// chat/execution_plan.go
type ExecutionMode int

const (
    ModeSingle   ExecutionMode = iota // 交给一个 Worker 答（不派发 DAG）
    ModeDelegate                      // 多 Worker 协作
)

type ExecutionPlan struct {
    Mode     ExecutionMode
    Intent   string       // 一句话复述用户目标（UI 展示 + 日志归因）
    Tasks    []PlanTask   // Mode==ModeDelegate 时非空
    Reporter string       // 汇总 Agent；空 = 默认 Agent
    Note     string       // 澄清/降级时给用户的说明
}

type PlanTask struct {
    ID        string   // t1、t2…（^t\d+$）
    Agent     string   // 必须在候选目录内
    Action    string   // 自包含任务书
    DependsOn []string // 只引用已存在的 ID；无环
    Priority  int      // 预算截断时保留高分
}

// 一次 Worker 的执行结果
type TaskResult struct {
    ID       string
    AgentID  string
    Status   TaskStatus // ok / failed / skipped / canceled / deferred
    Summary  string     // 回传给 Reporter 的唯一正文（硬截断）
    Artifacts []string
    ErrorCode string
    ElapseMs int64
    TokensIn, TokensOut int
}
```

### 5.3 规划一致性约束

| 字段 | 硬约束 |
|---|---|
| `Mode` | 由程序判定，不完全信任模型：有效任务 <2 ⇒ `ModeSingle`（避免"单任务计划"白花一次汇总） |
| `Tasks[].Agent` | 与今天分类器同样的**严格精确匹配**（忽略首尾空白与大小写），未命中即剔除该任务 |
| `Tasks[].Action` | 非空、≥20 字；**不含指代**（`上面/上述/刚才/前述/上一步/该表/前面的`）；含指代即剔除 |
| `Tasks[].DependsOn` | 指向不存在的 ID → 删该依赖；存在环 → 剔除环上任务；被剔除任务的下游连带剔除（依赖闭包修正） |
| `Tasks` 数量 | 2..`maxTasks`（默认 5），超出按 `Priority` 截尾 |
| `Reporter` | 必须在候选目录内；空/非法 → `IsDefault` 的那个 Agent |

---

## 6. 分诊与规划（`ITriager`）

> 一次 LLM 调用完成两件事：判定 simple/delegate；delegate 时同时产出任务 DAG。
> 完整判据、反例、评测集见 [简单复杂问题判定设计](./简单复杂问题判定设计.md)。

### 6.1 输入

- 用户消息（原文）
- **候选目录**：复用 [agent_router.go:210](../src/cclawservice/harness/ctxmgr/agent_router.go:210) 的摘要取值口径（AGENTS.md front matter `summary` > DB `description` > `agent_name`），**只给"ID + 一句话 + 能力标签"，不给子 Agent 的启动文档**
- 会话近 N 轮摘要（可选，`CtxParentSummary`；默认关闭以省 token）

### 6.2 输出协议（严格 JSON）

```json
{"mode":"single","agent":"agent_001","intent":"编制 2026014 处置报告"}
{"mode":"delegate","intent":"对比 A、B 两个资产包的处置价值",
 "tasks":[
  {"id":"t1","agent":"agent_001","action":"提取 A 包 2024 年资产负债与利润表主要科目，输出 markdown 表（单位万元）","depends_on":[],"priority":10},
  {"id":"t2","agent":"agent_001","action":"提取 B 包 2024 年资产负债与利润表主要科目，输出 markdown 表（单位万元）","depends_on":[],"priority":10},
  {"id":"t3","agent":"agent_002","action":"基于 t1、t2 的科目表计算资产负债率/有息负债率/净利率并列出差异超 20% 的科目","depends_on":["t1","t2"],"priority":10}
 ],
 "reporter":"agent_002"}
```

### 6.3 三级退化（保证"最坏不差于今天"）

```
① 严格 JSON 解析成功 → 校验（§5.3）→ 使用
② JSON 失败 → 整段文本是否精确等于某个 AgentId（= 今天的行为）
③ 仍失败 / 超时 / 空输出 → defaultAgent 单跑（= 今天的兜底）
```

第 ③ 条很重要：**规划失败不能让用户看到错误**，它必须等价于"今天路由到某个 Agent"。方案 A 没有 coordinator 实体，所以不存在"自己答"这一档——所有回落都落到 `defaultAgent`。

### 6.4 校验失败的回喂（可选，建议 v1 就做）

校验失败不抛错终止，而是把结构化错误回喂给模型自纠，最多 2 次：

```json
{"ok":false,"attempt":1,"error":{"code":"NOT_SELF_CONTAINED","task_id":"t3",
 "message":"t3.action 出现指代『上面的表』，且未在 depends_on 声明 t1/t2",
 "hint":"依赖写进 depends_on，action 里改为『基于 t1/t2 的科目表』"}}
```

> 实现提示：这里**不引入内置工具**。做法是分诊器内部做"同消息多轮"（`LlmRequest.Messages` 追加 assistant/tool），与 ToolLoop 无关，工具执行器一行不改。若嫌复杂，v1 可先用"校验失败即回落 single"。

### 6.5 规划提示词要点（可直接照抄进 prompt 文件）

1. **能一次答完就不要拆**（拆解本身有成本，且用户要等更久）；
2. 只有出现"多专业领域 / 并行取证 / 取数后计算"才 `delegate`；
3. **一个任务 = 一个可独立交付的产物**；同专业的连续步骤必须合并；
4. 任务书必须自包含：目标、材料、输出格式、约束；
5. 依赖只能用 `depends_on`；禁止"根据上面的结果"；
6. 判不准时选 `single`：宁可慢一轮再纠正，也不要把简单问题拆碎。

> 完整的判据、反例与输出 schema 见 [简单复杂问题判定设计](./简单复杂问题判定设计.md) §3.3。

---

## 7. 分派执行器（`IPlanExecutor`）

### 7.1 DAG → 分层（wave）

```
for wave := 0; ; wave++ {
    ready := tasks whose deps are all done
    if len(ready)==0 { break }
    scope := concurrency.GetTaskManager().NewScope(fmt.Sprintf("turn:%s:w%d", turnID, wave), width)
    for _, t := range ready { scope.Go(runCtx, RoutineSpec{Name:"task:"+t.ID, Fn: runWorker(t)}) }
    scope.Wait(waveCtx); scope.Cancel()
}
```

**为什么每层必须新建 scope**：`Wait()` 会把 scope 永久置为 draining（事实 13），复用会静默丢掉后续任务。好处是 `RoutineStats("turn:xxx:w2")` 天然按层出统计。

**为什么必须是 scope 而不是 goroutine**：事实 12 会直接让 CI 红。

`width = min(cfg.MaxParallel, len(ready), Σ worker.SubAgents)`。

### 7.2 Worker 的一轮怎么跑

抽出 `chat.TurnRunner`（从 `execute` 里剥出来的 6→8 段）：

```go
type TurnRequest struct {
    Cmd          Command
    Agent        dataobject.TAgent // 已解析；不再路由
    UserMsg      string            // 任务书
    Sink         ISink             // Worker: 只发状态的 sink；Reporter: 真 SSE
    Recordable   bool              // Worker: false；Reporter: true
    DisableTools bool              // Reporter: true
}
```

三处必须做对：

| 要点 | 做法 | 原因 |
|---|---|---|
| 上下文隔离 | `Recordable=false` → `Session.HasHistory()=false` → 不读父历史 | 事实 9，零改动 |
| 不污染历史 | Worker 不调 `journal.Begin/Finish`，不触发 `memoryMaintainer` | 否则父会话回放会看到 Worker 的中间过程 |
| 不写会话绑定 | 完全绕过 `Resolve`，用 `Preparer.PrepareFor(ctx, cmd, agent)` | 否则子任务会把会话绑定改成 Worker，后续轮次跑偏 |

`Preparer.PrepareFor` = 现有 `Prepare` 去掉 `agentRouter.Resolve` 一段，其余（provider / 技能 / 意图门 / prompt / 工具 / 预算）全部复用。**这是本方案对现有代码的唯一结构性前置改动。**

### 7.3 并发闸：必须分命名空间

```go
// root（已有）:  key = "agent:" + agentId              limit = MaxConcurrents
// worker（新增）: key = "agent:" + agentId + "#task"   limit = SubAgents
```

两个已核实的坑：

- **`ConcurrentAdvance` 的 limit 由该 key 首次调用决定**（事实 14）→ 子闸必须用独立 key，否则同 Agent 的根轮与子任务共用一个计数器，coordinator 会被自己的并发额堵死；
- **闸位等待上限 10s**（事实 15）→ 拿不到就返回 `deferred`，**重排到下一层**，不能当失败（否则高峰期大面积误判）。

`SubAgents == 0` 时按 1 处理（保守）。这也顺带让 DDL 里那个沉睡了很久的 `subagents` 列开始有意义。

### 7.4 超时与预算

- 整轮受 `AgentRunTimeout=600s` 约束（事实 8 的 `runCtx`），Worker 全部继承；
- 单任务超时 = `min(perTaskTimeout, 剩余时间)`；
- 每层开始前算剩余预算，按 `Priority` 截尾 → `skipped(BUDGET)`；
- 超预算时推一条 `notice(partial_result)`（复用现有 `ChatNotice*`）。

### 7.5 失败语义

| 情形 | 该任务 | 下游 | 本轮结局 |
|---|---|---|---|
| Worker 报错 / 超时 | `failed` | 依赖它的 `skipped(DEP_FAILED)` | 继续；汇总标缺口 |
| 闸位 10s 未获得 | `deferred` | 不传播 | 重排下一层（最多 1 次） |
| 全部任务失败 | 全部 `failed` | — | **降级：默认 Agent 单跑一次**（今天的兜底语义） |
| 用户中断 / 客户端断开 | 全部 `canceled` | — | 不写历史；已完成任务仍留痕 |
| Reporter 失败 | — | — | 直接把各任务摘要**拼接**作为答复（非 LLM），不空转 |

中心原则：**Reporter 永远执行**（除整体取消），宁可给"部分结论 + 明确缺口"。

### 7.6 深度固定为 1 层

Worker 的 `TurnState` 不带分诊能力（`AllowDelegate=false`），候选目录里也不存在"另一个 coordinator"——因为方案 A 没有 coordinator 实体，**递归在类型上就不可能发生**。这是方案 A 的一个额外好处：不需要额外守卫来防"Worker 又派 Worker"。

---

## 8. 汇总（Reporter）
### 8.1 为什么汇总必须是一个正常 turn

它需要**流式输出**、**写入会话历史**、**触发记忆维护**、**具备合法 Agent 身份**（决定文风与合规口径）。这四件事在现有代码里都挂在 `TurnRunner` 上，绕开它等于重写一遍。

### 8.2 输入模板

```
【用户原问题】
{original}

【子任务结果】
t1（agent_001，成功）：{summary}
t2（agent_001，成功）：{summary}
t3（agent_002，失败：超时）：未产出

请综合以上结果回答用户原问题。
要求：结论先行；失败或缺失的部分必须显式说明为缺口，不得编造。
```

- 不改 Reporter 的 system prompt —— 它仍然是"某个 Agent 在干自己的活"，启动文档、技能、合规全部自动生效；
- `DisableTools=true`：避免重复取证，减少失败面。

### 8.3 Reporter 是谁

v1 固定 `IsDefault` 的 Agent（与"兜底"语义一致）。计划里可指定（白名单校验），用于"报告排版固定交给某个 Agent"。

---

## 9. 上下文与 Token 经济

### 9.1 三个边界

| 方向 | 内容 | 硬约束 |
|---|---|---|
| 下行 父→子 | `Action` + `inputs`（文件路径 / 上游 **摘要** / 文本） | 只给摘要与路径；**禁止**把父的 tool_calls 协议消息塞进去（上游会判协议错） |
| 上行 子→父 | `summary` + `key_points` + `artifacts` + `usage` | `summary` 硬截断（默认 800 rune） |
| 汇总 引擎→Reporter | 每任务一行 | 总量受 Reporter 自己的 `ContextPolicy` 约束 |

### 9.2 摘要过长怎么办

`maxTasks=5` 时 5×800 rune ≈ 4k rune，可接受。任务数放大后启用**两级 reduce**：先按 wave 分组合并，再交 Reporter。

### 9.3 前缀缓存

规划提示词、候选目录都是**静态文本**，位于 system 固定位置；候选目录按 `(priority, agentId)` 稳定排序。否则每轮前缀都变，缓存全废（参见 [prompt_cache原理分析](./prompt_cache原理分析.md)）。

---

## 10. 存储

### 10.1 不新建 run 实体

沿用 `t_session → t_turn → t_turn_tool`，只加两张**从属于 turn** 的表：

```sql
-- 本轮的计划（一个 turn 最多一份）
CREATE TABLE IF NOT EXISTS t_turn_plan (
    ID          INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id     VARCHAR(40) NOT NULL UNIQUE,
    session_id  VARCHAR(40) NOT NULL,
    mode        INTEGER NOT NULL DEFAULT 0,   -- 0 single / 1 delegate
    intent      VARCHAR(512),
    reporter    VARCHAR(8),
    source      INTEGER NOT NULL DEFAULT 0,    -- RouteSource（判定来源，便于归因）
    signals     VARCHAR(64),                   -- "2,2,2,2"（四维计数）
    confidence  REAL,
    plan_json   TEXT,                          -- 原始决策（审计/回放）
    validate_note VARCHAR(512),                -- 被剔除的任务与原因
    attempts    INTEGER NOT NULL DEFAULT 0,    -- 校验回喂次数
    created_at  INTEGER NOT NULL,
    finished_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_t_turn_plan_session ON t_turn_plan(session_id, created_at);

-- 计划中的每个任务（= 一次 Worker 执行）
CREATE TABLE IF NOT EXISTS t_turn_task (
    ID          INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id     VARCHAR(40) NOT NULL,
    seq         INTEGER NOT NULL,              -- 计划内顺序
    task_key    VARCHAR(8)  NOT NULL,          -- t1/t2…
    agent_id    VARCHAR(8)  NOT NULL,
    action      VARCHAR(512) NOT NULL,
    depends     VARCHAR(128),                  -- 逗号分隔的 task_key
    wave        INTEGER,
    status      INTEGER NOT NULL DEFAULT 0,    -- 0 pending/1 running/2 ok/3 failed/4 skipped/5 canceled/6 deferred
    started_at  INTEGER, finished_at INTEGER, elapse_ms INTEGER,
    summary     TEXT,
    tokens_in   INTEGER, tokens_out INTEGER,
    error_code  VARCHAR(64),
    FOREIGN KEY (turn_id) REFERENCES t_turn(turn_id)
);
CREATE INDEX IF NOT EXISTS idx_t_turn_task_turn ON t_turn_task(turn_id, seq);
```

`t_agent` 增加（**不需要 `agent_role`** —— 方案 A 没有 coordinator 实体）：

```sql
ALTER TABLE t_agent ADD COLUMN max_children INTEGER NOT NULL DEFAULT 3; -- 单层最大并行子任务
ALTER TABLE t_agent ADD COLUMN max_tasks    INTEGER NOT NULL DEFAULT 5; -- 单轮最多任务数
```

`t_session` 增加：

```sql
ALTER TABLE t_session ADD COLUMN routing_mode INTEGER NOT NULL DEFAULT 0; -- 0 绑定Agent（今天）/ 1 规划器
```

### 10.2 三条存储纪律

1. **`t_session.routing_mode` 决定"续用语义"**：`0`=绑定 Agent（今天的行为，首轮 single 即写此值）；`1`=规划器（首轮 delegate 时写）。`t_session.agent_id` 填 **Reporter**，这样 UI 的徽标永远有确定值，但 `routing_mode=1` 会让路由器忽略"显式/绑定"短路、每轮重新分诊。详见 [判定设计 §7](./简单复杂问题判定设计.md)。
2. **`t_turn` 没有 agent 列，也不需要**：参与本轮的具体 Agent 在 `t_turn_task` 里；单 Agent 轮看 `t_session.agent_id` 即可。
3. **落库时机与今天一致**：轮末随 `Journal.Finish` 一次性写。原因：`t_turn` 行本身是轮末才插入（[memory_service.go:301](../src/cclawservice/harness/ctxmgr/memory_service.go:301)），执行期写 `t_turn_task` 会撞外键；中断轮复用既有 `FinishPartial` 通道。

### 10.3 与历史回放的一致性

会话历史文件 `memory_*.md` 里**只有一条 assistant 记录**（Reporter 的最终答复）。Worker 的中间过程只进 `t_turn_task`。这样：

- 下一轮 `Assembler.Assemble` 读到的历史干净、token 可控；
- 长期记忆蒸馏（只读会话记录）不会被 Worker 的中间产物污染；
- 前端"历史会话"抽屉（已实现）展示的仍是正常的问答。

---

## 11. UI / SSE

前端按 payload 键嗅探（事实 16），因此新事件**只用 `type` 键**，旧前端静默忽略：

```json
{"type":"plan","intent":"对比 A、B 两个资产包的处置价值","mode":"delegate","tasks":[{"key":"t1","agent":"agent_001","action":"…","depends_on":[]}]}
{"type":"task","key":"t1","agent":"agent_001","status":"running"}
{"type":"task","key":"t1","agent":"agent_001","status":"ok","ms":18400}
{"type":"task","key":"t3","agent":"agent_002","status":"skipped","reason":"dep_failed"}
{"type":"plan_done","ok":2,"failed":1,"ms":42100}
```

- 子任务**不转发正文**（多路交错会把 SSE 写成乱序）；需要时用 `CoordinatorStreamMode: silent|summary|full` 控制；
- 任务级事件的发送频率按**状态变化**，不按 token；
- 复用 `SSEPresenter.emit` 的锁，多 Worker 并发发事件是安全的（事实 16）；
- 已有 `t_session.agent_id` 徽标（[chat.js](../src/cclawchat/scripts/modules/chat.js) 的 `updateChatAgent`）可直接显示"本会话由规划器主理"（`routing_mode=1` 时）。

---

## 12. 配置

```json
"routing": {
  "complexity": {
    "enabled": true,
    "dryRun": true,
    "delegateConfidence": 0.6,
    "strongSignalOverride": true,
    "keywordRuleMode": "hint"
  },
  "coordinator": {
    "maxTasks": 5,
    "maxParallel": 3,
    "taskTimeoutSec": 180,
    "summaryMaxRunes": 800,
    "triageMaxTokens": 1024,
    "plannerCorrectionRetries": 2,
    "reporterAgentId": ""
  }
}
```

- `complexity.enabled=false` ⇒ 完全不判定，退化为今天的路由（一行开关，可灰度）；
- `complexity.dryRun=true` ⇒ 照常判定并落 `t_route_decision`，但执行永远走单 Agent（**推荐的第一个上线形态**）；
- `keywordRuleMode` / `delegateConfidence` / `qualityPreset` 的语义见 [判定设计 §10](./简单复杂问题判定设计.md)。

---

## 13. 代码改动清单

### 13.1 前置条件（已满足）

`MemoryService.ensureSessionAgent` 早前是空操作（两条分支都 return、`session.AgentId = agentId` 自赋值），导致 `t_session.agent_id` 为空的历史会话 `HasHistory()==false` → 历史记忆读不进来。该问题已在 `43a53b0` 修复：

```go
if strings.TrimSpace(session.AgentId) != "" { return session } // 已绑定
session.AgentId = agent.AgentId                                // 用本轮路由结果回填
_ = s.sessionAccessor.SetSessionAgentId(sessionId, session.AgentId)
```

这对协作是**必要条件**：`routing_mode`/`agent_id` 都要靠这一行才能落到存量会话上（§10.2 第 1 条）。落地前请确认线上版本已包含此修复。

### 13.2 新增/修改

| # | 文件 | 改动 | 规模 |
|---|---|---|---|
| 1 | `harness/ctxmgr/complexity.go` **新增** | `ComplexitySignals` + `DetectSignals`（纯函数，无 IO/无 LLM） | 中 |
| 2 | `harness/ctxmgr/route_decision.go` **新增** | `RouteMode` / `RouteSource` / `RouteDecision` | 小 |
| 3 | `harness/ctxmgr/agent_router.go` | 新增 `ResolveDecision`；`Resolve` 改薄包装；L0 顺序按判定表重排 | 中 |
| 4 | `harness/ctxmgr/triager.go` **新增** | `ITriager`：一次调用完成分诊 + 计划产出；三级退化 + 校验回喂 | 中 |
| 5 | `harness/ctxmgr/plan_prompt.go` **新增** | 分诊提示词、候选目录渲染、任务书模板、汇总模板 | 小 |
| 6 | `chat/execution_plan.go` **新增** | `ExecutionPlan` / `PlanTask` / `TaskResult` + 校验（§5.3） | 中 |
| 7 | `chat/plan_executor.go` **新增** | DAG→wave、scope、子闸、超时、失败语义 | 中 |
| 8 | `chat/turn_runner.go` **新增** | 从 `execute` 剥出 6→8 段，支持 `Recordable/DisableTools` | 中 |
| 9 | `chat/preparer.go` | 新增 `PrepareFor(ctx, cmd, agent)`；`Prepare` 保留为薄包装 | 小 |
| 10 | `service/chatting_service.go` | `execute` 改为 `prepare → [plan?] → runner → journal` | 中 |
| 11 | `model/dataobject/t_agent.go` | 加 `MaxChildren/MaxTasks`（**不加 `agent_role`**） | 小 |
| 12 | `model/dataobject/t_chat.go`、`t_turn_plan.go`、`t_turn_task.go` | `TSession.RoutingMode`；两表实体 | 小 |
| 13 | `model/dataaccessor/` | 两表批量写（轮末）+ `SetSessionRoutingMode` | 小 |
| 14 | `data/ddl_sqlite3.sql` | 两表 + `t_agent` 两列 + `t_session.routing_mode` + `t_route_decision` | 小 |
| 15 | `controller/sse_presenter.go` | 新增 `OnPlan/OnTask`（复用 `emit`） | 小 |
| 16 | `chat/chat_common.go` | `TurnTiming` 加 `TriageMs/TasksMs/TaskCount` | 小 |

**明确不动**：`tool_search.go`、`toolexecutor/*`、`security/*`、`assembler.go`、`memory_service.go`（除 §13.1 与 `routing_mode` 读写）、`session_agent_mgr.go`（仅扩缓存条目）、沙箱与权限分级、计费与观测。

---

## 14. 分阶段与验收

| 阶段 | 内容 | 验收 |
|---|---|---|
| **P0**（1–2 天） | `DetectSignals` 纯函数 + 判定用例集单测 | 12 条用例全绿；`go test ./...`、`archtest` 全绿 |
| **P1**（2–3 天） | `RouteDecision` + 分诊 prompt + **dryRun 只落库** | 线上只记录不执行；决策分布可查；行为与今天一致 |
| **P2**（校准后） | 打开 `enabled`（仍只支持 single）+ `t_session.routing_mode` | 简单路径延迟/成本与今天持平；计划率 5%~20% |
| **P3**（3–5 天） | `ModeDelegate` **串行**执行 + Reporter + 汇总 | 3 任务含依赖跑通；任一失败能产出部分结论 |
| **P4**（2–3 天） | 分层并行（scope）+ 子闸 + 超时 + 中断级联 | 中断父轮 → Worker 全停；同 Agent 子任务不超 `subagents` |
| **P5**（2–3 天） | 两表落库 + SSE 计划事件 + UI | 刷新可恢复；中断轮任务仍留痕 |

**P3 必须覆盖的用例**：全并行 3 任务；钻石依赖（t1,t2→t3）跨 2 层；中间任务失败；全部失败降级默认 Agent；环/未知 Agent/指代任务被剔除；用户中途中断；`enabled=false` 时行为与今天一致。

---

## 15. 风险

| 风险 | 表现 | 对策 |
|---|---|---|
| 模型过度规划 | 简单问题也拆 3 个任务 | 提示词硬规则 + "有效任务 <2 ⇒ single" + `confidence` 门槛 + 监控计划率 |
| 模型欠规划 | 复杂问题判成 simple | L1 强信号（能力越界/材料≥3）硬提升；`keywordRuleMode=hint`；UI"用协作重答" |
| 判据不可解释 | 用户不知道为什么变慢 | `t_route_decision` 留痕（source/signals/confidence）+ 计划卡片 |
| 规划僵化 | 执行中发现假设错误 | v1 用汇总标缺口；v2 才考虑 `revise_plan` |
| 父上下文被摘要撑爆 | 5 份摘要过长 | 硬截断 + 两级 reduce |
| 并发自锁 | root 与 Worker 抢同一"Agent"闸 | 子闸 key 加 `#task`；闸位超时转 `deferred` |
| 计划成环 / 死锁 | 任务永远不 ready | Kahn 检测前置（启动前） |
| 历史污染 | Worker 写进父历史 | `Recordable=false` + 不调 journal |
| 事件风暴 | 多路正文交错 | 子任务只发状态；`emit` 已串行化 |
| 与既有绑定冲突 | 协作会话被 Worker 绑定覆盖 | Worker 绕过 `Resolve`，绝不动 `t_session.agent_id` |
| 多轮退化 | 绑定短路让协作只在首轮生效 | `t_session.routing_mode=1` 时每轮重新分诊 |
| 递归失控 | Worker 又派 Worker | 方案 A 没有 coordinator 实体，类型上不可能递归 |

---

## 16. 待确认

1. **`keywordRuleMode` 默认 `hint` 还是 `direct`**？`direct` 完全保持今天的行为，但含关键词的跨领域问题永远进不了协作。
2. **`t_session.routing_mode` 是否接受**？这是唯一需要"改会话语义"的地方（且只影响被判为复杂的会话）。
3. **Reporter 默认用谁**：`IsDefault` 的 Agent（推荐）还是新建一个"综合报告"专用 Agent？
4. **子任务是否允许写文件**：v1 建议完全沿用各 Worker 自身的工具配置与权限分级，不额外加限制；若要收紧，需在 `ToolsBundle` 增加过滤步骤。
5. **计划是否需要用户确认**：v1 不做（生成即执行，UI 只展示）；`plan_mode: auto|confirm` 留位。
6. **`plannerCorrectionRetries` 默认 2 是否够**：建议先 2，按线上数据再调。

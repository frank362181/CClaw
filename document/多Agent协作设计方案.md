# 多 Agent 协作（Coordinator → Worker）设计方案

> 状态：设计稿（不落地）
> 目标版本：`3.0.0`（对应 [initialization.go](../src/agentService/initialization.go) 的 `feature 1: coordinator-worker 架构支持`）
> 适用范围：`cclawservice` 聊天主链路
> 关联：[Agent Loop设计机制](./Agent%20Loop设计机制.md)、[prompt_cache原理分析](./prompt_cache原理分析.md)、[工具权限分级设计方案](./工具权限分级设计方案.md)

---

## 0. 结论先行

现有机制（**显式 > 会话绑定 > 关键词 > 意图分类 > 默认兜底**）**一行不改**。Coordinator 是叠加在这一机制之上的"第三种执行形态"：

```
                    现有路由（不动）
用户消息 ──▶ ① 单Agent短路 → ② 显式 → ③ 会话绑定 → ④ 关键词 → ⑤ 意图分类 → ⑥ 兜底
                                                              │
                                        ┌─────────────────────┴──────────────────────┐
                                        ▼                                            ▼
                              agent_role = worker                          agent_role = coordinator
                              （今天的单 Agent 路径，零改动）                    │
                                                                     ┌───────────┴───────────┐
                                                                     ▼                       ▼
                                                              规划：理解意图            direct：自己答
                                                              + 生成任务 DAG            （无合适 Worker 时）
                                                                     │
                                                              delegate：分层并行派发
                                                                     │
                                                                     ▼
                                                              汇总（Reporter turn）
                                                                     │
                                                                     ▼
                                                     写一条 t_turn（最终答复）
```

三个不变量：

1. **一次用户提问仍然只产生一条 `t_turn`** —— 历史只有一套，Worker 的执行过程不进会话历史。
2. **路由的输入输出不变** —— `Resolve` 仍然只回答"这一轮由哪个 Agent 主理"。
3. **任何新能力都有开关，且关掉后行为逐字节等于今天**。

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
| 17 | 已有 `IRoundPlanner` / `TurnState.Plan`，语义是**工具轮次策略** | [chat_common.go:56](../src/cclawservice/chat/chat_common.go:56)、[tool_choice_adapter.go:148](../src/cclawservice/chat/tool_choice_adapter.go:148) |

---

## 2. 关键判断：把"意图识别"这次调用复用成"规划"调用

今天的第 ⑤ 段是：**一次 LLM 调用 → 输出一个 AgentId**。

协作所需要的"理解意图 + 生成计划"，本质上是**同一次调用换一种输出格式**：

| | 今天 | 协作后 |
|---|---|---|
| 调用次数 | 1 | 1 |
| 输入 | 用户消息 + 候选 Agent 精简目录 | 同左（+ 能力标签） |
| 输出 | `agent_001` | `{"mode":"single","agent":"agent_001"}` 或 `{"mode":"plan",...}` |

所以**简单问题的成本与延迟完全不变**（都是"1 次分类/规划 + 1 次回答"）。这是本方案能落地的前提：协作能力不是靠"多加一层"，而是靠**复用已有的那次调用**。

```go
// 今天
type ClassifyAgent func(ctx, llm, system, user string, maxTokens int) (LlmReply, error)

// 协作后（签名完全一样，只是 prompt 与输出协议升级）
// 解析失败时的三级退化保证不倒退，见 §6.3
```

---

## 3. 接入形态：三选一

### 形态 A：路由内规划（不引入 Agent 实体）

第 ⑤ 段直接输出 `single | plan`。

- ✅ 成本最低（协作轮少一次调用）
- ❌ coordinator 不可见：没有 UI 卡片、不能作为会话绑定、没有自己的启动文档/规则、无法通过"显式选择"进入

### 形态 B：Coordinator 是一种真实 Agent（**推荐**）

`t_agent` 增加 `agent_role`；coordinator 是普通 Agent 行，路由照旧选它；选中后该轮走"规划 → 派发 → 汇总"。

- ✅ 路由**零改动**（第 ⑤ 段还是输出一个 AgentId）
- ✅ coordinator 有身份、有启动文档、可被 UI 选中、可被 `t_agent_route_rule` 命中、可被 `t_session.agent_id` 长期绑定
- ✅ 用户已有能力（"历史会话续用原 Agent"）自动变成"协作会话续用 coordinator"
- ⚠️ 协作轮多一次 LLM 调用（≈1–2k token 的规划调用）。简单问题不受影响，因为它根本不会路由到 coordinator

### 形态 C：A+B 双入口

自动入口走 A，显式入口走 B。

- ✅ 省掉协作轮的那一次调用
- ❌ 两套 prompt、两套解析、两套降级路径要同步维护，长期是负担

**采用 B。** 若后续实测"协作轮的规划调用"成本敏感，再补 C 的自动入口（复用同一个 `Planner` 实现，只是换个调用点），不需要重构。

---

## 4. 执行链（形态 B）

```
ChattingService.execute
  ├─ 1  Preparer.Prepare                      ← 路由（原样）
  ├─ 2  每 Agent 并发闸（root: agent:<id>）    ← 原样
  ├─ 3  中断登记 / 运行超时                     ← 原样
  ├─ 4  Journal.Begin（建档）                  ← 原样
  │
  ├─ 5  ★ 分支：agent.AgentRole == coordinator ?
  │      ├─ no  → 6..9 原样（单 Agent 路径，代码一行不动）
  │      └─ yes → TaskPlanner.MakePlan（1 次调用，§6）
  │                ├─ mode=direct → 6..9（coordinator 自己答，等价今天的单 Agent）
  │                ├─ mode=single → 用 plan.Agent 重跑 PrepareFor 后 6..9（不额外调用）
  │                └─ mode=delegate →
  │                      PlanExecutor.Run（§7）
  │                        └─ 每个 Worker = 一次 "无记账 TurnRunner.Run"
  │                      Reporter turn（§8，流式、写历史）
  │
  └─ 6 Assembler.Assemble → 7 invoke → 8 收尾记账（原样，作用于 Reporter/单 Agent）
```

`mode=single` 的语义要写清楚：**规划调用已经给出了答案用哪个 Agent**，因此直接用 `plan.Agent` 构造 `TurnState`，**不再走一次路由**，也就没有额外调用。

---

## 5. 契约：`ExecutionPlan`

### 5.1 命名（避免与既有 `Plan` 撞车）

仓库里 `TurnState.Plan` / `IRoundPlanner` 已经表示"第 N 轮下发哪些工具"。新概念一律带 `Task`/`Execution` 前缀：

| 新概念 | 名字 | 不许用 |
|---|---|---|
| 执行计划 | `ExecutionPlan` | `Plan` |
| 计划中的任务 | `PlanTask` | `Task`（`concurrency` 包已有 `RoutineSpec`/`TaskStat` 语境） |
| 规划器 | `ITaskPlanner` | `Planner` |
| 分派执行器 | `IPlanExecutor` | `Executor`（`toolexecutor` 已占用） |

### 5.2 结构

```go
// cclawservice/chat/agent_role.go
type AgentRole int

const (
    AgentRoleWorker      AgentRole = 0 // 默认：今天的单 Agent
    AgentRoleCoordinator AgentRole = 1 // 可规划 + 可派发
)

// cclawservice/chat/execution_plan.go
type ExecutionMode int

const (
    ModeDirect   ExecutionMode = iota // coordinator 自己答（无合适 Worker / 澄清用）
    ModeSingle                        // 交给一个 Worker 答（不派发 DAG）
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

## 6. 规划器（`ITaskPlanner`）

### 6.1 输入

- 用户消息（原文）
- **候选目录**：复用 [agent_router.go:210](../src/cclawservice/harness/ctxmgr/agent_router.go:210) 的摘要取值口径（AGENTS.md front matter `summary` > DB `description` > `agent_name`），**只给"ID + 一句话 + 能力标签"，不给子 Agent 的启动文档**
- 会话近 N 轮摘要（可选，`CtxParentSummary`；默认关闭以省 token）

### 6.2 输出协议（严格 JSON）

```json
{"mode":"direct","intent":"用户想查昨天的天气","note":"当前没有天气相关的专家，由我直接回答"}
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
③ 仍失败 / 超时 / 空输出 → coordinator 自己答（ModeDirect），并附一句"未能拆分任务"的说明
```

第 ③ 条很重要：**规划失败不能让用户看到错误**，它等价于"今天路由到某个 Agent 但它答得一般"。

### 6.4 校验失败的回喂（可选，建议 v1 就做）

校验失败不抛错终止，而是把结构化错误作为**同一轮工具循环里的 tool 结果**回喂给模型自纠，最多 2 次：

```json
{"ok":false,"attempt":1,"error":{"code":"NOT_SELF_CONTAINED","task_id":"t3",
 "message":"t3.action 出现指代『上面的表』，且未在 depends_on 声明 t1/t2",
 "hint":"依赖写进 depends_on，action 里改为『基于 t1/t2 的科目表』"}}
```

> 实现提示：这里**不引入内置工具**。做法是 `ITaskPlanner` 内部做一次"同消息多轮"（`LlmRequest.Messages` 追加 assistant/tool），与 ToolLoop 无关，工具执行器一行不改。若嫌复杂，v1 可先用"失败即 ModeDirect"，把回喂放到 v2。

### 6.5 规划提示词要点（可直接照抄进 prompt 文件）

1. **能一次答完就不要拆**（拆解本身有成本，且用户要等更久）；
2. 只有出现"多专业领域 / 并行取证 / 取数后计算"才 `delegate`；
3. **一个任务 = 一个可独立交付的产物**；同专业的连续步骤必须合并；
4. 任务书必须自包含：目标、材料、输出格式、约束；
5. 依赖只能用 `depends_on`；禁止"根据上面的结果"；
6. `direct` 是合法的：没有合适专家时自己答，不要硬凑计划。

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
    AllowDelegate bool             // 固定 false（见 §7.6）
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

`AllowDelegate` 只在 `depth==0 && role==coordinator` 时为 true；候选目录**永远排除 coordinator**。理由：递归会同时放大成本、延迟与死锁面，而真实业务里"两层专家"已经够用。字段留 `max_depth` 便于将来放开。

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
    mode        INTEGER NOT NULL DEFAULT 0,   -- 0 direct / 1 single / 2 delegate
    intent      VARCHAR(512),
    reporter    VARCHAR(8),
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

`t_agent` 增加：

```sql
ALTER TABLE t_agent ADD COLUMN agent_role   INTEGER NOT NULL DEFAULT 0; -- 0 worker / 1 coordinator
ALTER TABLE t_agent ADD COLUMN max_children INTEGER NOT NULL DEFAULT 3; -- 单层最大并行子任务
ALTER TABLE t_agent ADD COLUMN max_tasks    INTEGER NOT NULL DEFAULT 5; -- 单轮最多任务数
```

### 10.2 三条存储纪律

1. **`t_session.agent_id` 永远是"入口 Agent"**：协作会话 = coordinator。这样 UI 的"历史会话续用原 Agent"（事实 7）自动让协作会话每轮都从 coordinator 进入，语义自洽。
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
- 已有 `t_session.agent_id` 徽标（[chat.js](../src/cclawchat/scripts/modules/chat.js) 的 `updateChatAgent`）可直接显示"由 coordinator 主理"。

---

## 12. 配置

```json
"coordinator": {
  "enabled": true,
  "maxTasks": 5,
  "maxParallel": 3,
  "taskTimeoutSec": 180,
  "summaryMaxRunes": 800,
  "planMaxTokens": 1024,
  "plannerCorrectionRetries": 2,
  "reporterAgentId": ""
}
```

`enabled=false` ⇒ 所有 coordinator 的 `agent_role` 被忽略，退化为今天的单 Agent（一行开关，可灰度）。

---

## 13. 代码改动清单

### 13.1 前置修复（与本方案强相关，建议一起做）

`MemoryService.ensureSessionAgent`（[memory_service.go:680](../src/cclawservice/harness/ctxmgr/memory_service.go:680)）目前是**空操作**：

```go
agentId := strings.TrimSpace(session.AgentId)
if agentId == pCommon.BlankString || strings.TrimSpace(session.AgentId) != pCommon.BlankString {
    return session            // 两条分支都返回 → 永远不会回填
}
session.AgentId = agentId     // 自赋值
```

后果：`t_session.agent_id` 为空的历史会话，`Session.HasHistory()` 为 false（事实 9）→ **历史记忆读不进来**。正确写法：

```go
if strings.TrimSpace(session.AgentId) != "" { return session } // 已绑定
session.AgentId = agent.AgentId                                 // 用本轮路由结果回填
if err := s.sessionAccessor.SetSessionAgentId(sessionId, agent.AgentId); err != nil { ... }
```

这对协作尤其重要：coordinator 会话的绑定值就是它的"入口身份"（§10.2 第 1 条）。

### 13.2 新增/修改

| # | 文件 | 改动 | 规模 |
|---|---|---|---|
| 1 | `chat/agent_role.go` **新增** | `AgentRole` 与 `ExecutionMode` 值对象 | 小 |
| 2 | `chat/execution_plan.go` **新增** | `ExecutionPlan` / `PlanTask` / `TaskResult` + 校验（§5.3） | 中 |
| 3 | `harness/ctxmgr/plan_prompt.go` **新增** | 规划提示词、候选目录渲染、任务书模板、汇总模板 | 小 |
| 4 | `chat/task_planner.go` **新增** | `ITaskPlanner` + LLM 调用 + 三级退化 + 回喂 | 中 |
| 5 | `chat/plan_executor.go` **新增** | DAG→wave、scope、子闸、超时、失败语义 | 中 |
| 6 | `chat/turn_runner.go` **新增** | 从 `execute` 剥出 6→8 段，支持 `Recordable/DisableTools` | 中 |
| 7 | `chat/preparer.go` | 新增 `PrepareFor(ctx, cmd, agent)`；`Prepare` 保留为薄包装 | 小 |
| 8 | `service/chatting_service.go` | `execute` 改为 `prepare → [plan?] → runner → journal` | 中 |
| 9 | `model/dataobject/t_agent.go` | 加 `AgentRole/MaxChildren/MaxTasks` | 小 |
| 10 | `model/dataobject/t_turn_plan.go`、`t_turn_task.go` **新增** | 两表实体 | 小 |
| 11 | `model/dataaccessor/` | 两表批量写（轮末） | 小 |
| 12 | `data/ddl_sqlite3.sql` | 两表 + `t_agent` 三列 | 小 |
| 13 | `controller/sse_presenter.go` | 新增 `OnPlan/OnTask`（复用 `emit`） | 小 |
| 14 | `chat/chat_common.go` | `TurnTiming` 加 `PlanMs/TasksMs/TaskCount` | 小 |

**明确不动**：`tool_search.go`、`toolexecutor/*`、`security/*`、`assembler.go`、`memory_service.go`（除 §13.1）、`agent_router.go`、沙箱与权限分级、计费与观测。

---

## 14. 分阶段与验收

| 阶段 | 内容 | 验收 |
|---|---|---|
| **P0**（1–2 天） | 剥 `TurnRunner`；`Preparer.PrepareFor`；`execute` 改薄；§13.1 修复 | 现有单测 + `archtest` 全绿；root 行为零变化 |
| **P1**（2–3 天） | `agent_role` + 规划器（含三级退化）+ 只做 `direct/single` 两种模式 | 简单问题路径不变；`enabled=false` 完全回退 |
| **P2**（3–5 天） | `ModeDelegate` **串行**执行 + Reporter + 汇总 | 3 任务含依赖跑通；任一失败能产出部分结论 |
| **P3**（2–3 天） | 分层并行（scope）+ 子闸 + 超时 + 中断级联 | 中断父轮 → Worker 全停；同 Agent 子任务不超 `subagents` |
| **P4**（2–3 天） | 两表落库 + SSE 计划事件 + UI | 刷新可恢复；中断轮任务仍留痕 |
| **P5** | 调优（`maxTasks`、摘要长度、两级 reduce）+ 评测 | 简单问题不进入规划的比率 ≥95%；复杂任务完成率 ≥ 单 Agent 基线 |

**P2 必须覆盖的用例**：全并行 3 任务；钻石依赖（t1,t2→t3）跨 2 层；中间任务失败；全部失败降级默认 Agent；环/未知 Agent/指代任务被剔除；用户中途中断；`enabled=false` 时行为与今天一致。

---

## 15. 风险

| 风险 | 表现 | 对策 |
|---|---|---|
| 模型过度规划 | 简单问题也拆 3 个任务 | 提示词硬规则 + "有效任务 <2 ⇒ single" + 监控计划率 |
| 协作轮多一次调用 | 延迟 +1~2s | 仅协作轮发生；如需省掉，补形态 C 的自动入口 |
| 规划僵化 | 执行中发现假设错误 | v1 用 `direct` 兜底 + 汇总标缺口；v2 才考虑 `revise_plan` |
| 父上下文被摘要撑爆 | 5 份摘要过长 | 硬截断 + 两级 reduce |
| 并发自锁 | root 与 Worker 抢同一"Agent"闸 | 子闸 key 加 `#task`；闸位超时转 `deferred` |
| 计划成环 / 死锁 | 任务永远不 ready | Kahn 检测前置（启动前） |
| 历史污染 | Worker 写进父历史 | `Recordable=false` + 不调 journal |
| 事件风暴 | 多路正文交错 | 子任务只发状态；`emit` 已串行化 |
| 与既有绑定冲突 | 协作会话被 Worker 绑定覆盖 | Worker 绕过 `Resolve`，绝不动 `t_session.agent_id` |
| 递归失控 | Worker 又派 Worker | `AllowDelegate` 仅 `depth==0 && coordinator`；候选目录排除 coordinator |

---

## 16. 待确认

1. **coordinator 是新建一条 `t_agent` 记录，还是复用现有某个 Agent 并置 `agent_role=1`？**（关系到 UI 上的呈现与会话绑定）
2. **Reporter 默认用谁**：`IsDefault` 的 Agent（推荐）还是新建一个"综合报告"专用 Agent？
3. **子任务是否允许写文件**：v1 建议完全沿用各 Worker 自身的工具配置与权限分级，不额外加限制；若要收紧，需在 `ToolsBundle` 增加过滤步骤。
4. **`direct` 时是否允许 coordinator 用工具**：建议允许（它就是普通 Agent），只是不派发 DAG。
5. **计划是否需要用户确认**：v1 不做（生成即执行，UI 只展示）；`plan_mode: auto|confirm` 留位。
6. **`plannerCorrectionRetries` 默认 2 是否够**：建议先 2，按线上数据再调。

# 多 Agent 协作（Coordinator → Worker）设计方案 v2

> 状态：设计稿（不落地）
> 目标版本：`3.0.0` —— 对应 [initialization.go](../src/agentService/initialization.go) 中的 `feature 1: coordinator-worker 架构支持`
> 适用范围：`cclawservice` 聊天主链路

## 修订说明（v2 推翻了 v1 的三处设计）

v1（已提交于 `28f1cc6`，可从 git 取回）在以下三处判断错误，本版全部纠正：

| # | v1 的做法 | 为什么错 | v2 的做法 |
|---|---|---|---|
| 1 | 把 coordinator 做成**统一入口**，现有路由降级为"提示（Hint）" | 现有"绑定 → 意图 → 兜底"的机制是既定且正确的，不应被削弱 | **路由一行不改**；coordinator 只接管"意图识别"这一步的内部实现 |
| 2 | 把计划做成**内置工具** `submit_plan`，coordinator 作为被路由选中的 Agent 在 `ToolLoop` 里提交 | 规划发生在"还不知道该用哪个 Agent"的阶段，放进 ToolLoop 就要改工具执行器、给 coordinator 配启动文档与技能，还要处理 ToolLoop 与 DAG 的嵌套 —— 全是间接成本 | 规划放在**路由决策阶段**（一次 LLM 调用），不占工具位、不进 ToolLoop |
| 3 | 引入 `t_run` / `t_run_node` 独立运行实体 | 与既有的 session / turn 模型重复，造成"一次对话两套历史" | 计划**从属于 turn**：`t_turn_plan` / `t_turn_task`，历史仍只有 `t_session` / `t_turn` 一套 |

---

## 0. 前提：本方案不动什么

先把你现有机制逐条列出，并标明本方案的处理方式 —— **除第 ⑤ 项外，全部保持原样**：

| 步骤 | 现状实现 | 本方案 |
|---|---|---|
| ① 只有一个可用 Agent | `Resolve` 在 `len(agents)==1` 时直接命中（[agent_router.go:73](../src/cclawservice/harness/ctxmgr/agent_router.go:73)） | **不变** |
| ② UI 绑定 / 显式指定 Agent | `explicitAgentId` 精确匹配，命中即用；未命中报错（[agent_router.go:79](../src/cclawservice/harness/ctxmgr/agent_router.go:79)） | **不变** |
| ③ 会话绑定 | `ChatSessionAgentRegistry`，TTL 1h（[session_agent_mgr.go](../src/cclawservice/harness/ctxmgr/session_agent_mgr.go)） | **不变**（仅单 Agent 轮写入绑定） |
| ④ 关键词规则 | `t_agent_route_rule` + `matchKeyword`（[agent_router.go:96](../src/cclawservice/harness/ctxmgr/agent_router.go:96)） | **不变**（命中即单 Agent，不进入规划） |
| ⑤ **意图识别选 Agent** | `classifyByLLM` + `BuildClassifyAgentPrompt`（[agent_router.go:189](../src/cclawservice/harness/ctxmgr/agent_router.go:189)、[context_assembler.go:187](../src/cclawservice/harness/ctxmgr/context_assembler.go:187)） | **升级为 Coordinator 决策**：输出 `single` 或 `plan` |
| ⑥ 兜底 | `defaultAgent`：`IsDefault` → 第一个可用（[agent_router.go:110](../src/cclawservice/harness/ctxmgr/agent_router.go:110)） | **不变**，并成为"计划全失败"时的最终兜底 |

一句话：**把第 ⑤ 步从"分类出一个 AgentId"改成"理解意图并输出执行计划"，其余全部原封不动。**

---

## 1. 现有调用链与唯一插入点

### 1.1 现状（`ChattingService.execute`）

```
Stream/Send
  └─ execute
      ├─ 107  Preparer.Prepare       → AgentRouter.Resolve（①②③④⑤⑥）+ provider + 技能 + prompt + 工具
      ├─ 121  每Agent并发闸 TryAcquire(agentId, MaxConcurrents)
      ├─ 143  Journal.Begin          → t_session 建档 / 复用
      ├─ 151  Assembler.Assemble     → system + 历史 + 本轮用户消息
      ├─ 159  invoke                 → 无工具一次调用 / 有工具 ToolLoop
      └─      Journal.Finish         → 写 t_turn + t_turn_tool（轮末一次性落库）
```

### 1.2 插入点只有一处

`AgentRouter.Resolve` 的第 ⑤ 步。它当前的语义是"用一次轻量 LLM 调用，从候选 Agent 里挑一个 ID"。本方案把这次调用的**输出协议**从"一个 ID"扩展为"单 Agent 决策 或 执行计划"，调用次数不变、位置不变、上游输入不变。

除此之外不需要动：绑定、关键词、兜底、Preparer、Assembler、ToolLoop、Journal、SSE 全部沿用。

### 1.3 为什么不能做成"统一入口"

- 会让**每一条**消息都多一次 LLM 调用（绑定会话今天是 0 次）；
- 会让"用户明确选了某个 Agent"的语义失效 —— 用户选了就是选了；
- 一旦 coordinator 出错，全站不可用，没有逃生通道。

**结论：coordinator 是"意图识别"这一层的实现替换，不是新的入口。**

---

## 2. 目标架构

### 2.1 决策是二元的：`single | plan`

```
RouteDecision
├─ Mode = single   → 与今天完全一致（一个 Agent 跑一轮）
└─ Mode = plan     → 生成计划 → 分派给多个 Worker → 汇总
```

关键：**`single` 分支的输出与今天 `classifyByLLM` 的结果完全等价**（同样的候选清单、同样的严格校验），所以简单问题的行为、成本、延迟**零变化**。

### 2.2 时序

```
                    ┌──────────────────────────────────────────────┐
用户消息 ───────────▶│ AgentRouter.Resolve                          │
                    │  ① 单 Agent 短路 / ② 显式 / ③ 会话绑定        │──▶ single ──▶ 现有路径（不变）
                    │  ④ 关键词规则                                │
                    │  ⑤ Coordinator.Decide  ← 本方案唯一改动       │
                    │  ⑥ default 兜底                              │
                    └──────────────────────────────────────────────┘
                                     │
                        ┌────────────┴────────────┐
                        ▼ mode=single             ▼ mode=plan
                  现有单 Agent 执行          PlanExecutor
                   （零变化）                    │
                                    ┌───────────┼───────────┐
                                    ▼           ▼           ▼
                                 wave 1：t1  t2（并行，各自一个 Worker turn）
                                    │
                                    ▼
                                 wave 2：t3（依赖 t1、t2）
                                    │
                                    ▼
                              Reporter turn（汇总，流式输出最终答复）
                                    │
                                    ▼
                         Journal.Finish：t_turn + t_turn_plan + t_turn_task
```

### 2.3 与"一次对话 = 一份历史"的一致性

| 角色 | 是否写会话历史 | 是否流式 | 说明 |
|---|---|---|---|
| 规划（Coordinator.Decide） | 否 | 否 | 一次 `SimpleComplete` 调用，不是 turn |
| Worker turn（×N） | **否** | 否 | 结果只进 `t_turn_task`，避免污染父历史与记忆蒸馏 |
| Reporter turn（×1） | **是** | **是** | 唯一写入 `t_turn` 的一轮，最终答复 |

这样 `t_session` / `t_turn` 的语义完全不变：**一次用户提问仍然是一条 turn**。

---

## 3. 决策契约

### 3.1 输出（单次 LLM 调用的严格 JSON）

```json
{ "mode": "single", "agent": "agent_001" }
```

```json
{
  "mode": "plan",
  "intent": "对比 A、B 两个资产包的处置价值，输出对比报告",
  "tasks": [
    { "id": "t1", "agent": "agent_001", "action": "提取 A 包 2024 年资产负债与利润表主要科目，输出 markdown 表", "depends_on": [] },
    { "id": "t2", "agent": "agent_001", "action": "提取 B 包 2024 年资产负债与利润表主要科目，输出 markdown 表", "depends_on": [] },
    { "id": "t3", "agent": "agent_002", "action": "基于 t1、t2 的科目表计算关键比率并列出差异超过 20% 的科目", "depends_on": ["t1", "t2"] }
  ],
  "reporter": "default"
}
```

### 3.2 解析的三级兼容（保证不倒退）

1. 严格 JSON 解析 → 取 `mode/agent/tasks`；
2. JSON 失败 → 退化为"整段文本是否等于某个 AgentId"（**即今天的行为**）；
3. 仍失败 / 超时 / 输出不合法 → 走 `defaultAgent`（**即今天的兜底**）。

> 这条设计的意义：即使 Coordinator 的 prompt 完全失效，系统也只会退化成"今天的样子"，**不会变得更差**。

### 3.3 什么时候必须 `single`

写进 prompt 的硬规则（同时做程序校验）：

- 只涉及一个专业领域，一个 Agent 能一次答完；
- 需要与用户来回澄清；
- 计划校验后有效任务 < 2 个（**单任务计划一律降级为 single**，否则只是多花一次汇总调用）。

---

## 4. 计划（Plan）的表示

### 4.1 schema

```go
type Plan struct {
    Intent   string      // 一句话复述用户目标（用于 UI 展示与日志归因）
    Tasks    []PlanTask
    Reporter string      // "default" | 具体 agentId（白名单校验）
}

type PlanTask struct {
    ID        string   // t1、t2…
    Agent     string   // 必须是候选清单里的 AgentId（精确匹配）
    Action    string   // 自包含任务书
    DependsOn []string // 只能引用已存在的 ID；无环
}
```

### 4.2 粒度准则（写进 prompt，用校验兜底）

- **一个任务 = 一个可独立交付的产物**（一张表 / 一份清单 / 一段结论）；
- 同一专业领域的连续步骤 → **合并**为一个任务（避免"取数→清洗→计算"拆成三次跨 Agent 往返）；
- 不同专业领域 → **并列**（这才是并行的价值来源）；
- 依赖只能用 `depends_on` 表达，**禁止**在 `action` 里写"根据上面的结果"（Worker 看不到父对话）。

### 4.3 校验规则

校验发生在**任何 Worker 启动之前**，纯代码、无 LLM：

| 规则 | 处理 |
|---|---|
| `agent` ∉ 候选清单 | 剔除该任务 |
| `id` 重复 / 格式非法 | 剔除该任务 |
| `depends_on` 指向不存在的 ID | 删除该依赖项 |
| 存在环 | 剔除环上任务 |
| `action` 为空或 < 20 字 | 剔除该任务 |
| `action` 含指代词（`上面/上述/刚才/前述/上一步/该表`） | 剔除该任务 |
| 同一 Agent + 高度相似 `action` | 去重 |
| 有效任务 < 2 | **降级为 single** |
| 有效任务 > `maxTasks`（默认 5） | 截断尾部 |
| `reporter` 不在白名单 | 改为 `default` |

剔除后要**修正依赖闭包**：若 B 依赖被剔除的 A，则 B 也剔除（连带），避免"半个输入"。

---

## 5. 执行引擎

### 5.1 抽出一个可复用的"单 Agent 一轮"

现在 `execute` 把"准备 → 装配 → 调用 → 收尾"写死在一个方法里，Worker 无法复用。需要抽出：

```go
// cclawservice/chat/turn_runner.go
type TurnRequest struct {
    Cmd        Command
    Agent      dataobject.TAgent // 已解析；Worker 由计划指定
    UserMsg    string            // 用户原话，或任务书
    Sink       ISink             // Reporter 传 SSE sink；Worker 传 NopSink
    Recordable bool              // Worker=false，Reporter=true
    DisableTools bool            // Reporter=true 时不再调工具
}

type ITurnRunner interface {
    Run(ctx context.Context, req TurnRequest) (TurnOutcome, error)
}
```

`execute` 改为 `prepare → runner.Run → journal` 三段，**root 路径行为不变**。这是唯一的"结构性改动"，且可以先用现有测试守住。

`Recordable=false` 时**零改动即实现上下文隔离**：`Journal.Begin` 不写绑定态 → `Session.HasHistory()==false` → [assembler.go:24](../src/cclawservice/chat/assembler.go:24) 不读历史，Worker 只看到自己的启动文档 + 任务书。

### 5.2 Worker turn 的形态

| 项 | 取值 | 理由 |
|---|---|---|
| Sink | `NopSink` + 只发状态事件 | 多个 Worker 同时流式会把正文交错写进同一条 SSE |
| ToolLoop | **同步串行**（`NewToolLoop(..., false)`） | 避免"并行里的并行"，工具扇出交给 root 的流式分支 |
| 并发闸 | `agentConcurrent.TryAcquire(ctx, agentId+"#task", agent.SubAgents)` | key 必须与 root 的 `agentId` 分开，否则同 Agent 的根轮与子任务互相堵死 |
| 超时 | `min(perTaskTimeout, run 剩余时间)` | 共用 root 的 `AgentRunTimeout`（600s）预算 |
| 记账 | 只进内存 `t_turn_task` 视图 | 轮末统一落库 |

> 注意 `ConcurrentAdvance` 的 limit 由**该 key 首次调用**决定（[concurrency_advance.go:29](../src/platformutility/concurrency/concurrency_advance.go:29)），所以 `#task` 这个 key 不要在不同 agent 之间复用，也不要在运行中改变 `subagents`。

### 5.3 并行：只能用 task scope

`archtest` 用 AST 静态禁止裸 `go` 语句（[goroutine_test.go:101](../src/cclawservice/archtest/goroutine_test.go:101)），因此并行必须走托管 scope。

```go
// 每一层（wave）一个 scope：因为 Wait() 会把 scope 永久置为 draining
scope := concurrency.GetTaskManager().NewScope(fmt.Sprintf("turn:%s:w%d", turnID, wave), width)
for _, t := range layer {
    _ = scope.Go(runCtx, concurrency.RoutineSpec{
        Name: "task:" + t.ID,
        Fn:   func(ctx context.Context) error { return runWorker(ctx, t) },
    })
}
_ = scope.Wait(waveCtx)
scope.Cancel()
```

两条已核实的约束：

- `taskScope.Wait` 会 `draining=true`，之后 `addIfOpen` 一律拒绝（[task_scope.go:45](../src/platformutility/concurrency/task_scope.go:45)）→ **每层新建 scope，不能复用**；
- `dispatch` 用 `context.AfterFunc(s.ctx, runCancel)` 把调用方 ctx 与 scope ctx 合流（[task_manager.go:320](../src/platformutility/concurrency/task_manager.go:320)）→ `scope.Cancel()` 即该层全停，root ctx 取消即全树停（**中断级联免费获得**）。

层宽 `width = min(cfg.MaxParallel, len(layer), Σ受影响 Agent 的 SubAgents)`。

### 5.4 失败语义

| 情形 | 该任务 | 下游 | 本轮结局 |
|---|---|---|---|
| Worker 返回错误 | `failed` | 依赖它的任务 `skipped` | 继续；汇总时标为缺口 |
| Worker 超时 | `failed(Timeout)` | 同上 | 同上 |
| 全层失败 | 全部 `failed` | — | **退回 default Agent 单跑**（见 §7） |
| 用户中断 / 客户端断开 | 全部 `canceled` | — | 不写历史，已得任务仍落 `t_turn_task` |

**中心原则：Reporter 永远执行**（除整体取消），宁可给"部分结论 + 明确缺口"。

### 5.5 上下文与 token

- Worker 的输入 = 任务书（见 §9.2），其中"上游结果"只放**摘要**，不放全文；
- Worker 的输出 = 完整正文，但**回传给 Reporter 的只有截断摘要**（默认 ≤800 rune）；
- Reporter 的输入 = 用户原话 + N 份摘要，总量受 Reporter 自己的 `ContextPolicy` 约束；
- `maxTasks` 默认 5：超过则先分组汇总（两级 reduce），避免 Reporter 窗口被打爆。

---

## 6. Reporter（汇总）

### 6.1 为什么汇总也要走一个正常 turn

- 需要**流式**输出（用户要看到打字效果）；
- 需要**写入会话历史**（下一轮才有上下文）；
- 需要**复用记忆维护**（蒸馏/压缩链路不变）；
- 需要一个**合法的 Agent 身份**（它的启动文档决定文风与合规口径）。

所以 Reporter = 一次普通的 `TurnRunner.Run`，只是：
- `UserMsg` = 原问题 + 各任务摘要 + "请综合上述结果作答"；
- `DisableTools = true`（避免重复调工具、减少失败面）；
- `Recordable = true`、`Sink = SSE sink`。

### 6.2 Reporter 是谁

| 取值 | 说明 | 建议 |
|---|---|---|
| `default` | 平台默认 Agent | **默认**，与"兜底"语义一致 |
| 具体 agentId | 计划里指定（白名单校验） | 用于"报告排版"固定交给某个 Agent |
| `routed` | 再跑一次分类 | 不推荐（多一次调用且不稳定） |

---

## 7. 降级阶梯（最坏情况不差于今天）

```
① Coordinator 决策失败/超时/输出非法
     → ② 退回现有 classifyByLLM（一次重试）
          → ③ 仍失败 → default Agent 单跑（今天的兜底）
计划已生成但……
④ 全部任务被校验剔除 → single（用 ② 的结果）
⑤ 全部 Worker 失败   → default Agent 单跑，并把失败原因作为附加上下文
⑥ Reporter 失败      → 直接拼接各任务摘要作为答复（非 LLM），不空转
```

第 ⑥ 条是保底：任何情况下用户都能拿到"已得到的部分结果 + 缺口说明"。

---

## 8. 数据模型

沿用 `t_session → t_turn → t_turn_tool` 的既有结构，**新增两张从属表**：

```sql
-- 一轮（turn）的计划；一个 turn 最多一份
CREATE TABLE IF NOT EXISTS t_turn_plan (
    ID            INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id       VARCHAR(40) NOT NULL UNIQUE,
    session_id    VARCHAR(40) NOT NULL,
    mode          INTEGER NOT NULL DEFAULT 0,   -- 0 single / 1 plan
    intent        VARCHAR(512),                 -- 复述的用户目标
    plan_json     TEXT,                         -- 原始决策（审计/回放）
    reporter      VARCHAR(8),
    validate_note VARCHAR(512),                 -- 校验剔除记录
    status        INTEGER NOT NULL DEFAULT 0,   -- 0 规划 / 1 执行 / 2 完成 / 3 降级
    created_at    INTEGER NOT NULL,
    finished_at   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_t_turn_plan_session ON t_turn_plan(session_id, created_at);

-- 计划中的每个任务（Worker 的一次执行）
CREATE TABLE IF NOT EXISTS t_turn_task (
    ID          INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id     VARCHAR(40) NOT NULL,
    seq         INTEGER NOT NULL,               -- 计划内顺序，与 t_turn_tool.seq 同构
    task_key    VARCHAR(8)  NOT NULL,           -- t1 / t2 …
    agent_id    VARCHAR(8)  NOT NULL,
    action      VARCHAR(512) NOT NULL,
    depends     VARCHAR(128),                   -- 逗号分隔的 task_key
    wave        INTEGER,
    status      INTEGER NOT NULL DEFAULT 0,     -- 0 pending/1 running/2 ok/3 failed/4 skipped/5 canceled
    started_at  INTEGER, finished_at INTEGER,
    elapse_ms   INTEGER,
    summary     TEXT,                           -- 回传给 Reporter 的摘要
    tokens_in   INTEGER, tokens_out INTEGER,
    error_code  VARCHAR(64),
    FOREIGN KEY (turn_id) REFERENCES t_turn(turn_id)
);
CREATE INDEX IF NOT EXISTS idx_t_turn_task_turn ON t_turn_task(turn_id, seq);
```

**落库时机**：与现有 `Journal.Finish` 保持同一节奏 —— **轮末一次性写**。原因：

- `t_turn` 行本身是轮末才插入（`MemoryService.appendTurn`），提前写 `t_turn_task` 会触发外键失败；
- 执行期只维护内存视图，避免每个任务一次 DB 写；
- 中断轮复用既有 `FinishPartial` 通道（已有 `interrupt_reason` 语义），把已完成任务的摘要一并落库。

`t_agent` 不需要新增列：`subagents`（可派生上限）与 `max_concurrent`（根轮并发上限）已经够用。

---

## 9. 提示词

### 9.1 决策提示词（替换 `BuildClassifyAgentPrompt` 的位置，新增不删旧）

```markdown
你是任务编排器。先判断这条用户消息需要"一个专家"还是"多个专家协作"。

# 候选专家（编号|名称|简介）
- agent_001|不良资产智能助手|尽调/估值/处置，债权与抵押物科目级数据
- agent_002|财务报表分析|逐科目异常识别与比率计算

# 输出（严格 JSON，不要任何解释或代码块标记）
需要单个专家：
{"mode":"single","agent":"<编号>"}
需要协作：
{"mode":"plan","intent":"<一句话复述目标>","tasks":[
  {"id":"t1","agent":"<编号>","action":"<自包含任务书>","depends_on":[]}
],"reporter":"default"}

# 判断规则
1. 一个专家一次能答完 → single。不要为了用上协作而拆任务。
2. 只有出现下列情况才 plan：
   - 需要两个及以上不同专业领域；
   - 需要并行取证（多份文件/多个对象）；
   - 需要"先取数再计算"这种有依赖的多段处理。
3. 任务粒度：一个任务 = 一个可独立交付的产物。
   同专业的连续步骤必须合并成一个任务。
4. 任务书必须自包含：写清目标、材料、输出格式。
   禁止出现"上面的"、"刚才的"、"上一步的结果"等指代；
   依赖关系只能通过 depends_on 表达。
5. 任务数 2~{maxTasks}。不确定就用 single。
```

### 9.2 任务书模板（Worker 的 user message）

```
【协作任务 {key}（第 {i}/{n} 个）】
目标：{action}

已知材料：
{inputs}

上游结果摘要：
{depSummaries | "（无）"}

输出要求：
- 直接给出结论与关键数据，不要复述任务书；
- 篇幅不超过 {k} 字；
- 若材料不足，明确说明缺什么，不要编造。
```

### 9.3 Reporter 的 user message

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

> Reporter 不改自己的 system prompt —— 它仍是"某个 Agent 在干自己的活"，只是这一轮的输入是结构化材料。这样启动文档、技能、合规口径全部自动生效。

---

## 10. 配置

在 `agentService/config/agentdefault.json` 增加一段（与现有 `agentconfig` 平级）：

```json
"coordinator": {
  "enabled": true,
  "maxTasks": 5,
  "maxParallel": 3,
  "taskTimeoutSec": 180,
  "summaryMaxRunes": 800,
  "plannerMaxTokens": 1024,
  "keywordPreempt": true,
  "bindingPreempt": true
}
```

- `keywordPreempt=true`：关键词命中 → 直接单 Agent（**保持现状**）。设为 false 则关键词只作为规划的提示。
- `bindingPreempt=true`：会话已绑定 → 直接单 Agent（**保持现状**，避免绑定会话每轮多一次 LLM 调用）。
- `enabled=false`：整体回退到今天的分类逻辑（一行开关）。

---

## 11. UI / SSE

前端按 payload 键嗅探（[api.js:127](../src/cclawchat/scripts/modules/api.js:127)），因此新事件只用 `type` 键，旧前端会静默忽略：

```json
{"type":"plan","intent":"对比 A、B 两个资产包的处置价值","tasks":[{"key":"t1","agent":"agent_001","action":"…","depends_on":[]}]}
{"type":"task","key":"t1","agent":"agent_001","status":"running"}
{"type":"task","key":"t1","agent":"agent_001","status":"done","ms":18400,"summary_len":620}
{"type":"task","key":"t3","agent":"agent_002","status":"skipped","reason":"dep_failed"}
{"type":"plan_done","ok":2,"failed":1,"ms":42100}
```

UI 呈现（业务系统可解释性优先）：

1. 一条"协作计划"卡片：`intent` + 分层任务列表（t1、t2 并行 → t3）；
2. 每个任务有状态灯与所用 Agent 头像，点击展开摘要；
3. 失败/跳过用与工具错误一致的色标，且最终答复里必须出现对应"缺口"段落。

事件发送复用 `SSEPresenter.emit`（已加锁串行写，多 Worker 并发发事件是安全的）。

---

## 12. 代码改动清单

| # | 文件 | 改动 | 规模 |
|---|---|---|---|
| 1 | `harness/ctxmgr/agent_router.go` | 新增 `ResolveDecision`（返回 `RouteDecision`）；`Resolve` 保留为薄包装（取 `decision.Agent`） | 小 |
| 2 | `harness/ctxmgr/coordinator.go` **新增** | 决策调用 + JSON 解析（三级兼容）+ 计划校验 | 中 |
| 3 | `harness/ctxmgr/plan_prompt.go` **新增** | 决策提示词、任务书模板、汇总材料模板 | 小 |
| 4 | `harness/ctxmgr/context_common.go` | 新增 `RouteDecision` / `Plan` / `PlanTask` 值对象 | 小 |
| 5 | `chat/turn_runner.go` **新增** | 从 `execute` 抽出单轮执行单元（root 与 Worker 共用） | 中 |
| 6 | `chat/plan_executor.go` **新增** | 分层调度（scope）+ 并行 + 失败处理 + 事件 | 中 |
| 7 | `service/chatting_service.go` | `execute` 改为 `prepare → runner → journal`；plan 分支接入 | 中 |
| 8 | `chat/preparer.go` | `Prepare` 接受已解析的 Agent（供 Worker 用），不再重复路由 | 小 |
| 9 | `model/dataobject/t_turn_plan.go`、`t_turn_task.go` **新增** | 两表实体 | 小 |
| 10 | `model/dataaccessor/`、`model/repo/` | 两表读写（轮末批量） | 小 |
| 11 | `data/ddl_sqlite3.sql` | 两表 DDL | 小 |
| 12 | `controller/sse_presenter.go` | 新增 `OnPlan/OnTask`（复用 emit） | 小 |
| 13 | `chat/chat_common.go` | `TurnTiming` 增加 `TasksMs/TaskCount/Planned` | 小 |

**明确不需要改**：工具执行器（`toolexecutor` 一行不动）、`ToolsBundle`、沙箱与权限、`Assembler`、`MemoryService`、计费与观测。

---

## 13. 分阶段落地

| 阶段 | 内容 | 验收 |
|---|---|---|
| **P0**（1–2 天） | 抽 `TurnRunner`；`execute` 改薄；root 行为零变化 | 现有单测全绿；`archtest` 全绿 |
| **P1**（2–3 天） | `RouteDecision` + `single` 分支（等价替换 `classifyByLLM`）；`coordinator.enabled=false` 可完全回退 | `single` 命中率与今天一致；关闭开关后行为逐字节一致 |
| **P2**（3–5 天） | 计划解析 + 校验 + **串行**执行 + Reporter | 3 任务含依赖可跑通；任一步失败可产出部分结论 |
| **P3**（2–3 天） | 分层并行（scope）+ 每 Agent 子闸 + 超时/中断 | 中断父轮 → Worker 全停；同 Agent 子任务不超 `subagents` |
| **P4**（2–3 天） | `t_turn_plan`/`t_turn_task` 落库 + SSE 计划事件 | 刷新可恢复；中断轮任务仍留痕 |
| **P5** | 调优（`maxTasks`、摘要长度、两级 reduce）、评测 | 简单问题计划率 < 10%；复杂任务完成率对比单 Agent 基线 |

**P2 的必测用例**：全并行 3 任务；钻石依赖（t1,t2→t3）跨 2 层；中间任务失败；全部失败降级到 default；环/未知 Agent 被剔除；用户中途中断。

---

## 14. 风险

| 风险 | 表现 | 对策 |
|---|---|---|
| 模型过度规划 | 简单问题也拆成 3 个任务 | prompt 硬规则 + "有效任务 <2 降级 single" + 监控计划率 |
| 计划僵化 | 执行中发现假设错误无法调整 | v1 用任务书自包含 + Reporter 标注缺口；v2 才考虑中途重规划 |
| 延迟 | 串行 3 层任务 | 分层并行 + 每任务超时 + 层宽限制 |
| 成本 | N 个 Worker | 每 Agent 子闸 + `maxTasks` + 明确"能 single 就 single" |
| 上下文污染 | Worker 写进父历史 | `Recordable=false`（复用 `HasHistory()` 判定，零改动） |
| 并发自锁 | 同 Agent 根轮与子任务抢同一闸 | 子任务用 `agentId#task` 独立 key |
| 并行不受控 | 新增裸 goroutine | `archtest` 已静态拦截；只能用 scope |
| 汇总窗口爆 | 5 个任务摘要过长 | 摘要截断 + 两级 reduce |
| 绑定会话无法协作 | 长会话被锁在单 Agent | `bindingPreempt` 开关；后续可加"协作信号"轻量判据 |

---

## 15. 待确认

1. **Reporter 用谁**：默认 Agent（推荐）还是计划里指定？若计划可指定，需要一个"擅长综合报告"的 Agent 配置。
2. **关键词是否抢跑**：`keywordPreempt=true`（保持现状）会让含关键词的跨领域问题不进入规划。是否接受？
3. **会话绑定是否抢跑**：同上（`bindingPreempt`）。若要支持"长会话中途转为协作"，需要引入一个零成本信号（如用户显式 `@两个Agent`）。
4. **Worker 是否允许写文件**：v1 建议完全按各 Agent 自身配置（沿用现有沙箱与权限分级），不加额外只读限制。
5. **计划是否需要用户确认**：v1 不需要（生成即执行，UI 只展示）；若要走"计划-确认-执行"，需要新增消息状态。
6. **`reporter` 是否禁用工具**：本方案建议禁用；若某业务需要 Reporter 二次取证，再放开。

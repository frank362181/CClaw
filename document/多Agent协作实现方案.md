# 多 Agent 协作实现方案

> 状态：设计定稿（未落地）
> 范围：`cclawservice` 聊天主链路
> 目标版本：`3.0.0`
> 本文是**唯一权威文档**：§1–§12 是设计，§13 是执行计划，§14–§16 是验收、风险与待确认。
> 关联：[Agent Loop设计机制](./Agent%20Loop设计机制.md)、[prompt_cache原理分析](./prompt_cache原理分析.md)、[工具权限分级设计方案](./工具权限分级设计方案.md)

**阅读顺序**：要判断"为什么这样做" → §1–§8；要判断"改哪里" → §9–§12；要判断"怎么排期、怎么验、怎么回滚" → §13；要判断"验收用例" → §14。

---

## 1. 目标与边界

### 1.1 要解决什么

在现有"**显式 > 会话绑定 > 关键词 > 意图分类 > 默认兜底**"的路由之上增加一条能力：

> 当一个问题**一个 Agent 做不完**时，自动拆成多个子任务分派给 Worker 并行执行；全部 Worker 完成后，由**一个独立的评审 Agent 统一评估所有产出**，并据此给出最终答复。

### 1.2 四条不变量

| # | 不变量 | 含义 |
|---|---|---|
| **I1** | 一次提问只产生**一条 `t_turn`** | 历史只有一套；Worker 的过程不进会话历史 |
| **I2** | 简单问题的**路径、成本、延迟与今天完全一致** | 分诊复用今天已有的那次分类调用 |
| **I3** | **统一评估是强制的**，不是计划项 | 所有 Worker 产出必须经过同一次独立评估，且评估者不是任何 Worker |
| **I4** | 任何新能力**关掉后行为逐字节等于今天** | `complexity.enabled=false` 即可整体回退 |

### 1.3 明确不改的部分

路由优先级与实现、单 Agent 执行链、`Assembler`、`MemoryService`、`ToolLoop`、`toolexecutor`（工具执行器一行不动）、沙箱与权限分级、计费与观测。

---

## 2. 名词与组件

### 2.1 名词

| 名词 | 含义 | 归属 |
|---|---|---|
| **Triage（分诊）** | 判定一条消息走 single 还是 delegate | 路由阶段 |
| **Plan（计划）** | 一次协作的任务 DAG（只描述"做什么"） | 执行期数据 |
| **PlanTask** | 计划中的一个子任务（生产者 + 任务书 + 依赖） | 执行期数据 |
| **Worker** | 子任务的执行者（= 一次 produce turn） | 执行期角色 |
| **Wave（波次）** | 依赖已满足、可并发执行的一批任务 | 调度单位 |
| **Evaluation（统一评估）** | 全部 Worker 完成后，由**一个**评审 Agent 一次性评估所有产出并写出最终答复 | 收尾阶段 |
| **Verdict** | 单个任务的裁决：`pass` / `pass_with_notes` / `fail` / `unverifiable` | 评估结果 |
| **Issue** | 一条问题：`severity`(blocker/major/minor) + `claim` + `evidence` + `suggest` | 评估结果 |
| **CrossCheck** | 跨任务一致性检查结论（批量评估独有） | 评估结果 |
| **Independence** | 评审者与生产者的独立性：`strong` / `moderate`（`weak`/`self` 不能作为结论） | 评审者属性 |
| **Evaluator（评审者）** | 承担统一评估的 Agent，同时产出最终答复 | 收尾阶段 |
| **Coordinator（协调者）** | 承担"规划 → 分派 → 监控 → 汇聚 → 委派评估"的角色 | 贯穿 ②–⑤ |

> 命名约定：工具轮次策略已改名为 `IRoundTool` / `TurnState.TurnTool` / `FirstRoundTool`，因此 `Plan` / `PlanTask` / `IPlanExecutor` 这组词本方案独占。

### 2.2 组件

| 组件 | 放置 | 职责 |
|---|---|---|
| `ICoordinator` | `chat` | 协调者门面：串起规划/分派/监控/汇聚/委派评估；实现为引擎，不额外产生 LLM 调用 |
| `ITriager` | `harness/ctxmgr` | 一次 LLM 调用完成分诊；delegate 时同时产出计划；内含提示词/解析/回填 |
| `IPlanExecutor` | `chat` | DAG → wave、派发 Worker、逐任务机检、**监控（watchdog）**、**汇聚（bundle）** |
| `IEvaluator` | `chat` | 评审者指派 + 统一评估（含作答）+ synthesis 机检 |
| `IMachineCheck` | `chat` | L-A/L-B 逐任务机检（纯函数，0 LLM） |
| `ITurnRunner` | `chat` | 单轮执行单元（root / worker / evaluator 共用） |
| `Preparer.PrepareFor` | `chat` | 用**给定 Agent** 构造 `TurnState`（不走路由） |

### 2.3 协调者（Coordinator）的职责与边界

协调者的五个职责，**只有第一个和最后一个消耗 LLM 调用**：

| 职责 | 做什么 | 实现方式 | LLM |
|---|---|---|---|
| **① 规划** | 理解意图 → 产出任务 DAG + 分诊结论 | `ITriager`（复用路由里那次分类调用位） | **1 次**（与今天分类共用） |
| **② 分派** | 拓扑分层 → 按波次派发 Worker（受并发闸/配额/预算约束） | `IPlanExecutor` | 0 |
| **③ 监控** | 观察进度/心跳/耗时，处理超时、停滞、闸位争用、预算耗尽 | 引擎内的看门狗（§6.6） | 0 |
| **④ 汇聚** | 把 N 份产出 + 机检结果 + 缺陷清单组装成评估输入 | 确定性函数 `BuildEvaluationBundle`（§6.7） | 0 |
| **⑤ 委派评估** | 指派一个**独立**评审 Agent，一次评估全部产出并作答 | `IEvaluator` + `ITurnRunner` | **1 次** |

总成本：`1（规划）+ N（Worker）+ 1（评估）= N+2`。

**为什么协调者不是一个"LLM Agent"**：

| 若做成 LLM Agent | 后果 |
|---|---|
| 每轮多一次"协调者思考"调用 | 成本 `N+3` 起，且协作轮延迟 +1~3s |
| 由模型决定"派给谁、要不要重派、是否评估" | 变成不可预测的自主循环，无法保证 V1–V3 的强制约束 |
| 需要给协调者配启动文档、技能、工具权限 | 引入新的配置面与安全面 |
| 监控变成"模型周期性看一眼" | 延迟高、成本高，且漏判无法测试 |

结论：**协调者是角色，不是 Agent turn**。它是确定性的编排器（可测试、可回归），Agent 只在"规划"和"评估"两处被调用。

> 注意：这条结论**不能**推广到评审者。评审者是"**角色（执行形态）+ 独立身份（错误来源）**"两层结构：做成自主 Agent 会丢掉 V1 覆盖与 V3 决定权，只做成角色（自评）则评估失去信息量。两档落地（moderate / strong）见 §7.8。

---

## 3. 全链路

```
                          ┌──────── Coordinator（引擎，0 额外 LLM）────────┐
用户消息                   │ 规划 → 分派 → 监控 → 汇聚 → 委派评估            │
  │                        └───────────────────────────────────────────────┘
  │
  ├─① 规划 = 分诊（1 次调用，复用今日分类调用位）
  │     ├─ A 段（0 成本）：禁用/强制/单Agent/planner/显式/绑定/关键词 → single
  │     ├─ L1 结构强信号 → 进入 B 段（提示必须 delegate）
  │     └─ L2 LLM 分诊 → single | delegate
  │
  ├─② single ────────────────────────────────▶ 今日路径（零改动）
  │
  └─② delegate
        ├─ 计划校验（linter，纯代码）→ 不合法则回填（≤2 次）
        ├─ 可协作性复查：评审者? 预算 N+2? 时间 depth+1? → 不满足即回落 single
        │
        ├─③ 分派 + 监控（wave 0..d）
        │     每个 produce = 一次 Worker turn（不记账、不写绑定、不读父历史）
        │     每个 Worker 绑定 monitorSink：最后事件时刻 / 工具是否在跑 / 耗时
        │     每完成一个 → 立即跑 L-A/L-B 机检（0 LLM），有 blocker 先就地修复
        │     看门狗：单任务超时→取消；长时间无事件→标 slow（§6.6）
        │
        ├─④ 汇聚（0 LLM）：等全部任务终态 → BuildEvaluationBundle（确定性，§6.7）
        │
        ├─⑤ 委派评估（1 次调用，独立评审 Agent）
        │     一次性评估**全部** Worker 产出 → 逐任务 verdict + 跨任务一致性 + 最终答复
        │
        ├─⑥ synthesis 机检（0 LLM）：答复里的数字必须能追溯到已通过校验的 claim
        │
        └─⑦ 落库：t_turn（1 条）+ t_turn_plan（1 条）+ t_turn_task（N+1 条）
```

每一步的负责人与失败处理：

| 步骤 | 协调者职责 | 负责人 | 失败时 |
|---|---|---|---|
| ① 规划 | 规划 | `ITriager` | 解析失败 → 回落今日分类 → 再失败 → `defaultAgent` 单跑 |
| ② 计划 | 规划 | linter | 结构化错误回填（≤2 次）→ 仍不合法 → 降级 single |
| ③ 分派 | 分派 | `IPlanExecutor` | 派发失败/配额不足 → `deferred` 重排；单任务失败 → 下游 `skipped` |
| ③ 监控 | 监控 | `IPlanExecutor` + watchdog | 超时 → 取消该任务；停滞 → 标 `slow`；全失败 → default Agent 单跑 |
| ④ 汇聚 | 汇聚 | `IPlanExecutor` | 输入超预算 → 按比例缩短摘要；仍超 → 截尾并在 `gaps` 声明 |
| ⑤ 委派评估 | 委派评估 | `IEvaluator` | 评审者不可用 → **不协作**（前置已拦）；评估不通过 → 级联修复 / 降级标注；全失败/中断 → 不触发评估 |
| ⑥ synthesis 机检 | 委派评估 | `IMachineCheck` | 出现无据数字 → 要求重写一次（仍失败则加声明） |
| ⑦ 落库 | — | `Journal` | 与今天一致：轮末一次性写；中断走 `FinishPartial` |

### 3.1 端到端示例（同时给出调用计数与波次）

用户："对比 A、B 两个资产包的处置价值，出一份对比报告"

```
① 分诊（1 次）        → delegate：t1 提取 A、t2 提取 B、t3 基于 t1/t2 交叉比对
② linter + 可协作性    → 通过（无环、Agent 合法、评审者可用、预算够）
③ wave0：t1 ∥ t2      → 2 次 Worker；完成后各跑一次 L-A/L-B（0 LLM）
   wave1：t3          → 1 次 Worker；同样机检
④ 统一评估 + 汇总      → 1 次调用：给出 t1/t2/t3 的 verdict + 跨任务一致性 + 最终答复
⑤ synthesis 机检      → 0 LLM
⑥ 落库                → 1 条 t_turn + 1 条 t_turn_plan + 4 条 t_turn_task（3 produce + 1 evaluate）
```

合计 **1 + 3 + 1 = 5 次调用 = N+2**。同样的问答走单 Agent 是 2 次。

**关键点**：评估是**一次**，不是每个任务一次。因此波次不受评估影响（`depth` 层），延迟只多一次调用。

---

## 4. 阶段一：分诊（single vs delegate）

### 4.1 复杂度的可判定定义

**判据不是"问题长短"，而是"一个 Agent 能不能一次做完"。** 四个可观测维度：

| 维度 | 观测方式 |
|---|---|
| **D1 能力跨度** | 各候选 Agent 的技能 `trigger-keywords` 分别命中 → 命中涉及几个**不同 Agent** |
| **D2 取证广度** | 去重的材料/对象计数（`.pdf/.xlsx/.docx` 文件、编号、债务人、资产包、抵押物）+ "分别/各自/逐个/每一个" |
| **D3 依赖深度** | "先…再…/第一步…第二步/基于…计算/得出…然后/据此" 的阶段数 |
| **D4 交付形态** | 显式要求几种产物（报告+表格 / 清单+结论 / 分三部分） |

```
复杂 ⇔ D1 ≥ 2  或  D2 ≥ 3  或  D3 ≥ 2  或  (D2 ≥ 2 且 D4 ≥ 2)
```

反例（必须写进 prompt 与测试）：

| 输入 | 判定 | 理由 |
|---|---|---|
| 贴 3000 字合同要求"总结 5 条要点" | simple | D1=1, D2=1, D3=0；**长 ≠ 复杂** |
| "先查 A、B 两个债务人的诉讼状态，再按回收率排序出表" | delegate | D3=2 + D4=2；**短 ≠ 简单** |
| "2026014 处置报告的诉讼时效怎么算" | simple | 咨询类，D1=1 |
| "对比 A 和 B" | simple | 仅并列词，无材料/阶段证据 |

> 复杂度是"任务结构 × 当前可用能力集合"的属性：同一个问题在只有 1 个 Agent 的单位只能是 simple。

### 4.2 三层判定

#### A 段（0 成本，first match wins）

| # | 条件 | 结果 | source |
|---|---|---|---|
| 1 | `complexity.enabled=false` | single | `disabled` |
| 2 | `forceMode=single`（UI 开关） | single | `forced` |
| 3 | 只有 1 个可用 Agent | single | `single_agent` |
| 4 | `forceMode=delegate`（UI 开关） | → B 段 | `forced` |
| 5 | 会话 `routing_mode=planner` | → B 段（跳过 6/7/8） | `session_planner` |
| 6 | 显式 `agentId` | single | `explicit` |
| 7 | 会话绑定 | single | `binding` |
| 8 | 关键词命中且 `keywordRuleMode=direct` | single | `keyword` |
| 9 | L1 强信号 | → B 段（提示必须 delegate） | `struct_strong` |
| 10 | 否则 | → B 段（L2 分诊） | `llm` |

`keywordRuleMode=hint`（默认）时关键词结果**不短路**，作为证据注入 L2。

> 与今天机制相比只有一处顺序调整：第 5 条排在 6/7/8 之前。因为 UI 会把 `t_session.agent_id` 回传成显式 `agentId`；不让 planner 先短路，协作会话从第二轮起就会被"显式绑定"钉死成单 Agent。

#### L1 结构信号（0 成本，只做加法）

**强信号（命中即进入 B 段，不再"选一个 Agent"）**：

| 强信号 | 判据 | 为什么可靠 |
|---|---|---|
| S1 能力越界 | 技能命中覆盖 **≥2 个不同 Agent** | 一个 Agent 只有自己的技能集，跨两家物理上做不到 |
| S2 材料洪泛 | 去重材料计数 **≥3** | 单次上下文无法兼顾，必然需要分段取证 |

命中后用**规划提示词**（显式要求 delegate）；若模型仍返回 single，以模型为准并记录 `deviation=strong_signal_single`——不强行拼凑计划。

**弱信号（只作证据，不单独提升）**：W1 多诉求、W2 对比并列、W3 材料=2、W4 串行阶段、W5 多交付物。`weakSignalHits ≥ 2` → prompt 标注"倾向复杂"，否则"倾向简单"。W6 篇幅（>1500 字）**永远只作提示**。

#### L2 LLM 分诊（1 次调用，复用今日分类调用位）

```json
{"mode":"single","agent":"agent_001","intent":"咨询诉讼时效口径",
 "signals":{"capability_span":1,"evidence_breadth":0,"dependency_depth":0,"deliverable":1},
 "confidence":0.92}
```

```json
{"mode":"delegate","intent":"对比 A、B 两个资产包的处置价值",
 "signals":{"capability_span":1,"evidence_breadth":2,"dependency_depth":2,"deliverable":2},
 "confidence":0.81,
 "tasks":[
   {"id":"t1","agent":"agent_001","action":"提取 A 包 2024 年资产负债与利润表主要科目，输出 markdown 表（单位万元）","depends_on":[],"priority":10,"expected_output":"科目 | 2024 | 2023 | 同比"},
   {"id":"t2","agent":"agent_001","action":"提取 B 包 2024 年资产负债与利润表主要科目，输出 markdown 表（单位万元）","depends_on":[],"priority":10,"expected_output":"科目 | 2024 | 2023 | 同比"},
   {"id":"t3","agent":"agent_001","action":"基于 t1、t2 的科目表计算资产负债率/有息负债率/净利率，列出差异超 20% 的科目","depends_on":["t1","t2"],"priority":10,"expected_output":"比率对比表 + 差异科目清单"}
 ]}
```

设计要点：

1. **先证据后结论**：先填四维计数，再给 mode；程序校验自洽性（`delegate` 时至少一维 ≥2，否则按 signals 重算 mode）。
2. **严禁询问"要不要校验"**：提示词不出现 verify 相关字样；计划里也不允许出现（§5.2）。
3. **`confidence` 只卡 delegate**：`< delegateConfidence`（默认 0.6）→ 降为 single。
4. **不指定评审者**：评审者由引擎指派（§7.3），模型无权决定由谁监督自己。
5. **`intent` 必填**：用于 UI 展示与日志归因。

> 注意示例里三个任务都派给 `agent_001`：因为评审者不能是任何 Worker，而当前单位只有 2 个 Agent（见 §7.3 的硬约束）。

#### B 段（计划产出后，按顺序）

| # | 条件 | 结果 | source |
|---|---|---|---|
| 11 | L2 解析失败 | 回落今日分类 → 再失败用 `defaultAgent` | `fallback` |
| 12 | 计划 linter 不通过（回填 2 次后） | single | `plan_invalid` |
| 13 | 有效任务 <2 | single | `too_few_tasks` |
| 14 | `confidence < delegateConfidence` | single | `low_confidence` |
| 15 | 可协作性复查不通过（评审者/预算/时间） | single | `no_verifier`/`no_budget`/`no_time` |
| 16 | 全部通过 | delegate | `struct_strong`/`llm`/`forced` |

#### 可协作性前置（B 段第 15 条）

```go
func canDelegate(comCode string, plan Plan) error {
    if _, err := pickEvaluator(comCode, producerIDs(plan)); err != nil { return ErrNoVerifier }
    if !budget.CanAfford(len(plan.Tasks) + 2) { return ErrBudget }          // 分诊 + N + 评估
    if remainingTime < latencyEstimate(plan) { return ErrLatency }          // 按 depth+1 波次估
    return nil
}
```

| 条件 | 不满足时 | 说明 |
|---|---|---|
| 存在合格的独立评审者 | single（`no_verifier`） | 评审者不得是任何 Worker（§7.3） |
| 预算 ≥ `N+2` | single（`no_budget`） | 省成本的正确位置是**不协作**，不是砍评估 |
| 时间够（`depth+1` 波次） | single（`no_time`） | 评估只在最后加一轮 |

> 该检查需要 N 与 depth，因此实际顺序是：L2 产出计划 → linter → 可协作性复查 → 通过才真派发。若失败则丢弃计划并回落 single。

### 4.3 三级降级（不倒退保证）

```
① 严格 JSON 解析成功 + 自洽校验通过 → 使用
② JSON 失败 → 整段文本精确等于某个 AgentId → single（= 今天的行为）
③ 仍失败 / 超时 / 空 → defaultAgent 单跑（= 今天的兜底）
```

再加一条保险：`mode=delegate` 但**有效任务 < 2** → 自动降为 single。

### 4.4 多轮会话：`routing_mode`

今天的会话绑定会让第 2..n 轮不再路由，这对单 Agent 正确，但会杀死协作：

```
首轮：拉 A、B 两个包做对比 → delegate ✓
次轮：再把 C 包加进来一起比 → 已绑定 agent_002 → 单 Agent ✗
```

解法：`t_session.routing_mode`

| 首轮判定 | 落库 | 后续轮次 |
|---|---|---|
| single | `routing_mode=0`，`agent_id=<选中>` | 沿用今天的绑定语义（**完全不变**） |
| delegate | `routing_mode=1`，`agent_id=<评审者>` | 每轮重新分诊（跳过 A 段 5/6/7/8），规划时带上上一轮摘要 |

### 4.5 成本模型

| 分支 | 今天 | 之后 |
|---|---|---|
| 显式/绑定/关键词/唯一 Agent | 1 次回答 | 同 |
| 简单问题 | 1 分类 + 1 回答 = **2** | 1 分诊 + 1 回答 = **2** |
| 协作（N 个任务） | — | 1 分诊 + N 个 Worker + **1 次统一评估** = **N+2** |
| 机检触发就地修复（每个） | — | +1 Worker（0 额外 LLM 用于机检） |
| 评估后修复级联 | — | ≤ `maxRepairTasks` 个 Worker + 1 次重评估（默认最多 +3） |

一句话：**评估是"一次"，所以协作成本是 N+2，而不是 2N+2。**

---

## 5. 阶段二：计划

### 5.1 结构

```go
// chat/plan.go
type PlanMode int
const (
    PlanModeSingle   PlanMode = iota // 不派发，直接用 Decision.Agent
    PlanModeDelegate
)

type Plan struct {
    Mode     PlanMode
    Intent   string     // 一句话复述用户目标
    Tasks    []PlanTask // Mode==Delegate 时非空
}

type PlanTask struct {
    ID             string   // ^t\d+$，唯一
    Agent          string   // 必须在候选目录内
    Action         string   // 自包含任务书
    DependsOn      []string // 只引用已存在的 ID；无环
    Priority       int      // 预算截断时保留高分
    ExpectedOutput string   // 输出结构约定（L-B 机检用）
}

// 计划**没有** verify / reviewer 字段：
//   - 评估是强制的（§7.1 V1/V3）；
//   - 评审者由引擎指派（§7.3），模型无权选择监督自己的人。
```

### 5.2 linter（纯代码，任何 Worker 启动前执行）

| 规则 | 处理 |
|---|---|
| `agent` ∉ 候选目录 | 剔除该任务 |
| `id` 重复 / 格式非法 | 剔除 |
| `depends_on` 指向不存在的 ID | 删除该依赖项 |
| 存在环（Kahn 不能全部出队） | 剔除环上任务 |
| `action` 为空或 < 20 字 | 剔除 |
| `action` 含指代（`上面/上述/刚才/前述/上一步/该表/前面的`） | 剔除 |
| 同一 Agent + 高度相似 `action` | 去重 |
| 被剔除任务的下游 | 连带剔除（依赖闭包修正） |
| 有效任务 < 2 | 降级 single |
| 有效任务 > `maxTasks` | 按 `Priority` 截尾 |
| **出现 `verify`/`review`/`check` 等自定监督字段** | **整份计划拒绝** |
| **生产者的并集覆盖了所有可用 Agent**（无评审者） | 降级 single（`no_verifier`） |

### 5.3 计划回填（≤2 次）

linter 失败不抛错终止，而是把结构化错误回填给模型自纠（同一轮多轮消息，不引入内置工具）：

```json
{"ok":false,"attempt":1,"error":{"code":"NOT_SELF_CONTAINED","task_id":"t3",
 "message":"t3.action 出现指代『上面的表』，且未在 depends_on 声明 t1/t2",
 "hint":"依赖写进 depends_on，action 改为『基于 t1/t2 的科目表』"}}
```

2 次仍失败 → 降级 single（附 `note` 说明未能拆分的原因）。

### 5.4 提示词要点

1. 能一次答完就不要拆；
2. 只有"多专业领域 / 并行取证 / 取数后计算"才 delegate；
3. 一个任务 = 一个可独立交付的产物；同专业连续步骤合并，不同专业并列；
4. 任务书必须自包含：目标、材料、输出格式、约束；禁止指代；
5. 依赖只能用 `depends_on` 表达；
6. 判不准时选 single；**不要提评估/校验**。

---

## 6. 阶段三：分派与监控

### 6.1 DAG → wave

```
for wave := 0; ; wave++ {
    ready := tasks whose deps all produced (status==ok)
    if len(ready) == 0 { break }

    // Wait() 会把 scope 永久置为 draining → 每层必须新建 scope
    scope := concurrency.GetTaskManager().NewScope(fmt.Sprintf("turn:%s:w%d", turnID, wave), width)
    for _, t := range ready {
        _ = scope.Go(runCtx, concurrency.RoutineSpec{Name: "task:" + t.ID, Fn: runWorker(t)})
    }
    _ = scope.Wait(waveCtx)
    scope.Cancel()
}
// 全部波次结束后：统一评估（§7）
```

- `width = min(maxParallel, len(ready), Σ 相关 Agent 的 SubAgents)`；
- 裸 `go` 被 `archtest` 静态禁止，**必须**用 scope；
- `dispatch` 会把调用方 ctx 与 scope ctx 合流 → `scope.Cancel()` 即该层全停，root ctx 取消即全树停（**中断级联免费获得**）。

#### 依赖语义是"已产出"，不是"已校验"

因为评估只在最后做一次，下游必然消费**尚未评估**的上游产出。这是"统一评估 + N+2 成本"的直接代价，必须显式处理：

| 处理 | 做法 |
|---|---|
| 下游把上游当**待校验输入** | 任务书模板里明确：上游结果标记为 `[provisional]`，只能作为输入，不得直接断言为事实 |
| 上游发现问题 → **修复级联** | 评估阶段判定 t1 有 blocker 时，t1 与**所有传递依赖它的任务**（如 t3）一起重跑，然后重评估 |
| 级联上限 | `maxRepairTasks`（默认 2）；超出则只修 blocker 所在任务，下游标 `tainted` 并在答复里声明 |

> 若某业务场景必须"下游只用已校验数据"，正确做法是把计划拆小（或用串行多轮对话），而不是在单轮里串两级评估。

### 6.2 Worker turn（三件事必须做对）

从 `execute` 剥出 `ITurnRunner`，root / worker / evaluator 共用：

```go
type TurnRequest struct {
    Cmd          Command
    Agent        dataobject.TAgent // 已解析；不再路由
    UserMsg      string            // 任务书 / 评估材料
    Kind         TurnKind          // Root | Produce | Evaluate
    Sink         ISink
    Recordable   bool              // Produce: false；Root/Evaluate: true
    DisableTools bool              // Evaluate: true
}
```

| 要点 | 做法 | 原因 |
|---|---|---|
| 上下文隔离 | `Recordable=false` → `Session.HasHistory()==false` → 不读父历史 | 现有判定，**零改动** |
| 不污染历史 | Worker 不调 `journal`、不触发 `memoryMaintainer` | 否则父会话回放会看到中间过程 |
| 不写会话绑定 | 绕过 `Resolve`，用 `Preparer.PrepareFor(ctx, cmd, agent, kind)` | 否则子任务会把会话绑定改成 Worker |
| 工具循环 | Worker 用**同步串行** ToolLoop | 避免"并行里的并行"；扇出交给 root |

### 6.3 并发闸：必须分命名空间

```go
// root（已有）:  key = "agent:" + agentId              limit = MaxConcurrents
// worker:        key = "agent:" + agentId + "#task"    limit = SubAgents（0 视作 1）
// evaluator:     key = "agent:" + agentId + "#eval"    limit = 1
```

- 闸的 limit 由**该 key 首次调用**决定 → 必须独立 key，否则同 Agent 的根轮与子任务共用计数器，互相堵死；
- 闸位等待上限 10s → 拿不到返回 `deferred`，**重排到下一波**，不能算失败；
- 评估走独立 key，避免与 Worker 抢配额（尤其评审者同时也是 Worker 单位的场景）。

### 6.4 超时与预算

- 整轮受 `AgentRunTimeout`（600s）约束，Worker 全部继承；
- 单任务超时 = `min(taskTimeoutSec, 剩余时间)`；
- 每波开始前算剩余预算，按 `Priority` 截尾 → `skipped(BUDGET)`；
- **必须为评估预留预算**：开工前扣掉 1 次评估的估算用量；不足则整个计划降级（而不是评估时才发现没预算）。

### 6.5 失败语义

| 情形 | 任务状态 | 下游 | 本轮结局 |
|---|---|---|---|
| Worker 报错 / 超时 | `failed` | `skipped(DEP_FAILED)` | 继续；评估时标缺口 |
| 机检 blocker（L-A/L-B） | 就地修复 1 次 | 不传播 | 修复后仍 blocker → `failed` |
| 闸位 10s 未获得 | `deferred` | 不传播 | 重排下一波（最多 1 次） |
| 全部任务失败 | 全部 `failed` | — | **降级：default Agent 单跑一次** |
| 用户中断 / 客户端断开 | 全部 `canceled` | — | 不写历史；已完成任务仍留痕 |
| 评估调用失败 | — | — | 拼接各任务摘要（非 LLM）作为答复，并声明"未经评估" |

### 6.6 监控与汇合（协调者的第 ③④ 项职责）

#### ③ 监控：看门狗，不是"再调一次 LLM"

协调者监控 N 个 Worker，靠的是**它们自己发出的事件**，不需要额外调用：

| 信号 | 来源 | 用途 |
|---|---|---|
| 最后事件时刻 | `monitorSink` 记录每次 `OnDelta / OnReasoning / OnTool` 的时间 | 判断是否停滞 |
| 工具是否在跑 | `OnTool(phase=intent/running)` → 在跑；`done/error` → 结束 | 停滞阈值分级 |
| 已耗时 | 任务开始时间 | 单任务超时 |

判定与动作（**分级，避免误杀**）：

| 情形 | 阈值 | 动作 |
|---|---|---|
| 工具运行中 | 复用工具层心跳（`ToolCallingHeartbeatInterval=3s`），工具自身 120s 超时 | 不干预——有事件即活性 |
| LLM 调用中长时间无事件 | `stallWarnSec=60` 记 warn；`stallKillSec=min(taskTimeoutSec,180)` 取消 | 取消该 Worker → `failed(STALL)` |
| 超过单任务时限 | `taskTimeoutSec` | 取消 → `failed(TIMEOUT)` |
| 配额/预算耗尽 | 每波开始前算 | 剩余任务 `skipped(BUDGET)`，**但评估预算必须保留** |

监控**不做**的事（v1）：不重新规划、不让 LLM 介入判断、不因"慢"就换 Agent（换人只发生在修复阶段）。理由：这些都是把确定性编排变成不可测的自主循环。

监控产物：`t_turn_task.elapse_ms` / `error_code=STALL|TIMEOUT`、SSE `task` 事件里的 `slow` 标记、指标 `stall_count` / `timeout_count`。

#### ④ 汇合：等全部终态，不靠"消息通知"

**Worker 不通知评审者，也不需要通知。** 两者之间没有直连通道，也不应该建：一次 turn 的生命周期里只有执行器在跑，Agent 之间是"被调用"的关系，不是"互相收发消息"的实体。

正确的模型是**执行器做汇合（barrier），再由它调用评审者**：

```
Worker t1 ─┐
Worker t2 ─┼─▶ 执行器收集 TaskResult（内存 map[taskID]）
Worker t3 ─┘            │
                        │ 每波结束 / 每次修复后检查"是否全部进入终态"
                        ▼
                 readyToEvaluate() == true
                        │
                        ▼
          runner.Run(Kind=Evaluate, Agent=evaluator)   ← 一次同步调用，"通知"就是这次调用
```

三点要澄清：

| 疑问 | 答案 |
|---|---|
| 谁负责触发？ | **执行器**（`IPlanExecutor`），不是 Worker，也不是评审者 |
| 触发条件是什么？ | **所有任务进入终态**（`ok / failed / skipped / canceled`），且 `deferred` 已重排用尽 |
| "通知"长什么样？ | 没有消息、没有队列、没有 DB 轮询。就是执行器在进程内同步调用一次 `TurnRunner.Run`，把 N 份产出作为 `UserMsg` 传进去 |

#### 任务状态机（触发判定只依赖它）

```
pending ──▶ running ──┬──▶ ok
                      ├──▶ failed
                      └──▶ canceled
pending ──▶ deferred ──┬──▶ pending   （重排，≤2 次）
                       └──▶ failed(CAPACITY)
pending ──▶ skipped(DEP_FAILED / BUDGET / UPSTREAM_UNVERIFIED)
```

```go
func readyToEvaluate(tasks []*TaskState) bool {
    for _, t := range tasks {
        switch t.Status {
        case ok, failed, skipped, canceled:
            continue                       // 终态
        case deferred:
            if t.DeferCount >= maxDefer {  // 重排用尽 → 归为终态
                t.Status = failed
                t.ErrorCode = "CAPACITY"
                continue
            }
            return false
        default:                            // pending / running
            return false
        }
    }
    return len(tasks) > 0
}
```

#### 为什么不用别的方式

| 备选 | 为什么不用 |
|---|---|
| Worker 完成即回调评审者 | 会变成"N 个 Worker 各触发一次评估"，等于回到逐任务评估（成本 `2N`），且每次只看得到局部，无法做 `cross_checks` |
| Worker 完成后写 DB，评审者轮询 | 引入延迟与竞争；评审者是 turn，不是常驻进程，轮询无处安放 |
| 用消息队列/事件总线解耦 | 本项目 Worker 是**同进程内的一次 turn**，不是独立服务；加总线只会把一次函数调用变成不可观测的异步链路 |
| 每个 Wave 结束就评一次 | 后续 Wave 的结果评不到；且评估次数 = 波次数，成本失控 |

#### 必须处理的四个边界

| 边界 | 处理 |
|---|---|
| 有任务 `deferred` | **不触发**；重排到下一波。重排上限 2 次后转 `failed(CAPACITY)`，再判定 |
| 有任务 `skipped`（上游失败） | 视为终态：评估需要知道它被跳过，并写进 `gaps` |
| 全部失败 | 不进评估：直接降级 `defaultAgent` 单跑（省一次调用） |
| 用户中断 / 客户端断开 | 不进评估：`runCtx` 已取消，直接收尾；已完成任务与机检结果留痕 |

#### 评估只发生一次，但"重评估"是允许的

```
首次评估（1 次）
   └─ 有 blocker → 级联修复 → 重评估（≤maxRepairRounds，默认 1 次）
        └─ 仍有 blocker → 不再重评估，写进 gaps
```

即：`Trigger()` 的调用点是 **2 个**——"全部终态"之后，以及"每轮级联修复"之后；不会因为某单个 Worker 完成就触发。

#### 若将来 Worker 变成异步/远程服务

当前 Worker 是**同进程内的一次 turn**，所以汇合是一次函数返回。若以后引入远程执行（例如把工具执行或长任务外置），只需把"内存 map"换成"结果存储 + 汇合查询"：

| 时机 | 变化 |
|---|---|
| Worker 落结果 | 写 `t_turn_task`（`status=ok` + summary/claims），并置 `finished_at` |
| 汇合判定 | 评估前查一次 `SELECT count(*) FROM t_turn_task WHERE turn_id=? AND status IN ('pending','running')`，为 0 才触发 |
| 幂等 | 触发前先 `UPDATE t_turn_task SET status=running WHERE kind=1 AND status=pending` 抢占，防止重复评估 |

**仍然不需要 Agent 之间直接通信**——只是把"内存汇合"换成"DB 汇合"，调用方依旧是执行器。

#### 落库与事件

- 评估节点在**被触发时**才创建 `t_turn_task` 行（`kind=1`，`status=running`），不是提前建一条 pending；
- SSE 在触发时发 `{"type":"evaluate","status":"running"}`，完成后发 `done` + 逐任务 verdict；
- 若从未触发（全失败/中断），则**没有** `kind=1` 行——"没有评估"这件事本身就是可查的（而不是靠一个空的 pass 掩盖）。

### 6.7 汇聚：把 N 份产出组装成"评估输入"（0 LLM）

汇聚**不是写答案**，而是把散落的执行结果组装成一份**确定性的、可复现的**评估输入。写答案仍然发生在评估那一次调用里（这是 `N+2` 的前提）。

```go
type EvaluationBundle struct {
    Question   string        // 用户原问题（原文）
    Intent     string        // 规划时的意图复述
    Tasks      []BundleTask  // 按计划 seq 稳定排序
    Gaps       []Gap         // 由执行期事实预填充（不是模型写的）
    Budget     BundleBudget  // 已用/剩余，供评审者判断"是否还值得深挖"
}

type BundleTask struct {
    ID       string       // t1
    Agent    string       // 生产者
    Action   string       // 任务书（原文，不截断）
    Expected string       // expected_output
    Status   TaskStatus   // ok / failed / skipped / canceled
    Summary  string       // ≤summaryMaxRunes（超限按比例缩）
    Claims   []Claim      // 结构化，(quote 截断到 200 字符)
    Machine  MachineGate  // L-A/L-B 结果（pass / blocker 列表）
}

type Gap struct {          // 由执行期事实生成
    TaskID string
    Kind   string          // failed / skipped / budget / capacity / stall / timeout
    Detail string
}
```

规则：

| 规则 | 说明 |
|---|---|
| **顺序稳定** | 按计划 `seq` 排序，不按完成时间——保证同输入同输出（可复现、可断言、利于缓存） |
| **gaps 预填充** | `failed / skipped / budget / capacity / stall / timeout` 在汇聚阶段就写进 `Gaps`，评审者**必须**逐条处理（采纳或解释），不能视而不见 |
| **体积控制** | 总长受 `evaluate.inputMaxRunes`（默认 12000）约束；超限时按比例缩短各 `summary`（下限 300），`Action` 与 `Claims.quote` 优先保留 |
| **不喂推理过程** | 只给产出与证据，不给 Worker 的 tool 调用链与思考过程（反锚定，§7.3） |
| **无 LLM** | 纯函数，可用表驱动单测；同一 bundle 序列化后应字节稳定 |

为什么把"汇聚"单独抽出来：

1. **可测试**：不用起 LLM 就能断言"上游失败时 gaps 里有没有它"；
2. **可解释**：评审者看到的是结构化事实，而不是一段拼接文本；
3. **可替换**：将来要做"两级 reduce"（每 5 个任务先合并一次）时，只改这一层。

---

## 7. 阶段四：委派评估

### 7.1 三条强制（结构不变量）

| # | 强制内容 | 明确禁止 |
|---|---|---|
| **V1 覆盖** | 一次评估必须覆盖**全部** Worker 产出；返回的 `tasks[]` 必须逐一对应所有任务 ID，缺一个即视为**无效评估** | 只抽几个看、只看汇总、按风险跳着看 |
| **V2 独立性** | 评审者**不得是任何 Worker**，且达 `strong`/`moderate`；无法满足则**不得进入协作** | 让某个 Worker 评估自己或同伴；`weak`/`self` 作为结论 |
| **V3 决定权** | "要不要评估""由谁评估"都由**引擎**决定 | 模型在计划里声明 `verify:none` 或指定评审者（linter 直接拒绝整份计划） |

推论：**单 Agent 的单位永远不协作**（可协作性前置拦），系统里不存在"协作但没评估"的状态。

> 命名：这三条不变量记作 **V1–V3**（V = 不变量），**不要**与执行计划里的里程碑 `M1–M9`（§13.3）混淆。

### 7.2 三层校验（两层逐任务，一层统一）

| 层 | 谁做 | 粒度 | 抓什么 | 成本 |
|---|---|---|---|---|
| **L-A 机检** | Go 代码 | **逐任务** | 引用是否存在、原文片段是否真在该位置、数字/编号/日期是否在源材料中出现、派生值能否重算一致 | 0 LLM |
| **L-B 结构检** | Go 代码 | **逐任务** | `expected_output` 字段/表头/条目是否齐全、条数与长度下限、必填项为空、单位量纲缺失、"待补"却给确定结论 | 0 LLM |
| **L-C 语义评估** | 评审 Agent | **一次覆盖全部** | 逐任务：口径一致性、是否回答任务书每一项、推测是否被写成事实；**跨任务：口径/单位/时间范围是否自洽** | 1 LLM |

**顺序硬性 L-A/L-B → L-C**：

- 每个 Worker 一完成就跑机检（0 成本），有 blocker **就地修复**，不等到最后；
- 机检无法判定的语义问题，才交给最后一次统一评估。

这样既保住了"机检先行省钱"的性质，又把 L-C 收敛成唯一一次调用。

#### 产出契约（L-A 的前提）

Worker 必须把结论拆成带出处的 claim：

```json
{
  "summary": "A 包有息负债 12.4 亿元，资产负债率 68.2%，经营性现金流为负。",
  "claims": [
    {"id":"c1","text":"有息负债 12.4 亿元","kind":"fact",
     "source":{"file":"reports/A_2024.pdf","locator":"第 37 页 短期借款+长期借款","quote":"短期借款 740,000,000.00"}},
    {"id":"c2","text":"资产负债率 68.2%","kind":"derived",
     "formula":"总负债/总资产","operands":{"负债":1284000000,"资产":1883000000}},
    {"id":"c3","text":"经营现金流为负","kind":"inference"}
  ]
}
```

| kind | 机检方式 |
|---|---|
| `fact` | 文件存在 + `quote` 能在该位置文本中找到 |
| `derived` | 用 `operands` 重算，与 `text` 中数字比对（相对容差 0.5%） |
| `inference` | 只校验"已标注为推理"，交 L-C 判是否有依据 |

#### 统一评估的输入（必须控制体积）

每个任务只喂三样东西，总量受 `evaluateInputMaxRunes`（默认 12000）约束：

| 内容 | 上限 |
|---|---|
| 任务书 `action` + `expected_output` | 原文 |
| 该任务的 `summary` | 800 rune（超了按比例缩，最低 300） |
| 该任务的 `claims`（含 source 与 quote） | 结构化、紧凑；`quote` 截断到 200 字符 |

**不给**：Worker 的推理过程、tool 调用链、父会话历史、其他单位数据。

#### 统一评估的输出契约

一次调用同时产出"逐任务裁决 + 跨任务一致性 + 最终答复"：

```json
{
  "tasks": [
    {"task_id":"t1","verdict":"pass","checked":["c1","c2","c3"],"issues":[],"unverifiable":[]},
    {"task_id":"t2","verdict":"fail","checked":["c1","c2"],"issues":[
       {"id":"i1","severity":"blocker","claim_id":"c2","kind":"口径",
        "message":"资产负债率用了母公司口径，任务书要求合并报表",
        "evidence":{"file":"reports/B_2024.pdf","locator":"第 41 页 合并资产负债表"},
        "suggest":"改用合并报表重算"}
    ],"unverifiable":[]},
    {"task_id":"t3","verdict":"pass_with_notes","checked":["c1"],"issues":[
       {"id":"i2","severity":"minor","claim_id":"c1","message":"未给出比率计算过程","suggest":"补公式与中间值"}
    ],"unverifiable":[]}
  ],
  "cross_checks": [
    {"task_ids":["t1","t2"],"kind":"口径不一致",
     "message":"t1 用合并报表、t2 用母公司报表，两者不可直接比较"}
  ],
  "gaps": ["t2 口径不一致，其结论未纳入最终对比"],
  "answer": "……最终答复（结论先行，缺口单列）……"
}
```

有效性约束（缺一即视为**无效评估** → 按 `unverifiable` 重试一次）：

- `tasks[]` 必须覆盖全部任务 ID，**一个不缺**（M1 的程序化落地）；
- 每条 issue 必须有 `evidence`（文件+位置，或可复算算式）；
- 某任务 `verdict=fail` 时至少一条 `blocker`；
- `verdict=pass` 时必须给出非空 `checked`；
- 允许 `unverifiable`：**"我不知道"是合法结论，"通过"不是默认值**。

> **批量评估的额外收益**：`cross_checks` 是逐任务评估做不到的——它能发现"t1 用合并报表、t2 用母公司报表"这种**跨任务**不一致。

### 7.3 评审者指派

| 等级 | 判定（代码可判） | 允许作为结论 |
|---|---|---|
| `strong` | `evaluator ∉ producers` 且 `vendor(evaluator) != vendor(任一 producer)` | ✅ |
| `moderate` | `evaluator ∉ producers` | ✅ |
| `weak` | 评审者是某个 Worker（换会话/清上下文） | ❌ 记为"未评估" |
| `self` | 同轮自检 | ❌ 只补格式，不进准确性结论 |

```go
func pickEvaluator(comCode string, producerIDs []string) (string, Independence, error) {
    cands := agentsOf(comCode)
    cands = exclude(cands, producerIDs...)      // 不得是任何 Worker
    best  := rank(cands, bySkillOverlap(0.6), byVendorDiff(0.3), byLoad(0.1))
    if best == nil { return "", 0, ErrNoIndependentVerifier }  // → 不协作
    return best.ID, independenceOf(best, producerIDs), nil
}
```

**域匹配优先于独立性等级**：一个不懂业务的 strong 评审者，比懂业务的 moderate 更没用。

#### 一条必须提前认知的硬约束

评审者不能是任何 Worker，因此：

| 单位内 Agent 数 | 能做什么 |
|---|---|
| 1 | **不能协作**（无可指派评审者） |
| 2 | 协作 = **1 个生产者 + 1 个评审者**；所有任务必须派给同一个生产者 |
| 3+ | 才可能"两个 Agent 都生产 + 第三个评估" |

当前 `t_account` 下 `100001` 只有 `agent_001`、`agent_002` 两个 Agent，所以：

- 协作轮的 N 个任务都会落在同一个 Agent 上，评审由另一个承担 —— **这仍然是真正的协作**（分工体现在"生产 vs 评估"，以及任务并行），只是生产侧不跨 Agent；
- 若希望生产侧也跨 Agent，**必须新增第三个 Agent 作为专职评审者**（建议名为"复核助手"，`primary_model` 用 `qwen3.8-flash` 以拿到 `strong` 独立性）。

> 这一条同时解释了为什么 `verify_agent_id` 值得作为独立字段：它让"专职评审者"成为一个显式角色，而不是靠排除法碰运气。

### 7.4 Rubric（固定表，不经模型）

| 检查项 | 层 | 粒度 |
|---|---|---|
| 输出结构符合 `expected_output` | L-B | 逐任务 |
| 每个数字/日期/编号都有出处 | L-A | 逐任务 |
| 出处可回读（文件/位置/引文一致） | L-A | 逐任务 |
| 派生值可重算一致 | L-A | 逐任务 |
| 口径与任务书一致（合并/母公司、含税/不含税、基准日） | L-C | 逐任务 |
| 任务书要求的每一项都有应答 | L-C | 逐任务 |
| 推测被标注为推测 | L-B + L-C | 逐任务 |
| 单位、量纲、时间范围 | L-B | 逐任务 |
| **跨任务口径一致** | L-C | 整批 |
| **结论强度与证据强度匹配**（"可能"不得写成"必然"） | L-C | 整批 |

### 7.5 回路与终止

```
N 个 Worker ──▶ 逐任务机检 ──┬─ blocker ──▶ 就地修复（≤1 次/任务）
                              └─ 通过 ──┐
                                        ▼
                              统一评估（1 次）──┬─ 全部 pass ─────────────▶ 采纳 answer
                                                ├─ 有 blocker ──▶ 修复级联（≤maxRepairTasks + 依赖闭包）
                                                │                  ──▶ 重评估（1 次）
                                                └─ unverifiable ──▶ 标"未核实"，写进 gaps
```

| 出路 | 触发 | 结果 |
|---|---|---|
| `repair`（就地） | 机检 blocker | 该 Worker 带着具体缺口重做，再跑机检（无额外 LLM 用于机检） |
| `repair`（级联） | 评估给出 blocker | 该任务 + 传递依赖它的任务一起重做，然后重评估 |
| `escalate` | 级联修复后仍 fail，且存在另一个合格评审者 | 换评审者重评估一次（仅换取视角，不换生产者） |
| `degrade` | 修复次数/预算用尽，或 `unverifiable` | 该任务结论不采纳，写进 `gaps`；答复仍产出 |

终止条件（缺一不可）：

1. 就地修复：每任务 ≤1 次；
2. 评估后级联修复：≤ `maxRepairRounds`（默认 1）轮，且每轮涉及任务 ≤ `maxRepairTasks`（默认 2）；
3. 同一 issue 重复出现即停（复用 loop_guard 的"完全相同即停"思想）；
4. 评估预算与整轮超时。

**绝不允许**：fail 当 pass、把 unverifiable 当 pass、因预算不足跳过评估。

### 7.6 评审者不可用的四种情形

| 情形 | 处理 |
|---|---|
| 单位内只有 1 个 Agent | **不进入协作**（`ErrNoVerifier`），走单 Agent |
| 生产者的并集覆盖了所有 Agent | 同上（计划被 linter 拒绝并降级 single） |
| 评审者并发额已满 | 评估 `deferred` → 重排；连续 2 次 → `degrade(CAPACITY)`，答复声明"未经评估" |
| 评审者失败 / 超时 | 换下一个合格评审者重试 1 次；仍失败 → 降级为"拼接摘要"，并声明"未经评估" |

关键差别：**不可用 → 不协作**；**已协作但评估失败 → 明确声明未评估**。任何情况下都不允许"标着已评估、实际没评估"。

### 7.7 synthesis 机检（答复也要过一道）

评估者同时写答复，会引入新失真（错引、夸大、把"未核实"写成"已确认"）。因此答复产出后再跑一次**纯机检**（0 LLM，保持 N+2）：

| 检查 | 方式 | 失败后果 |
|---|---|---|
| 答复里的每个数字/编号，都能在**已通过评估**的 claims 里找到 | 机检（复用 `KeyTokenPattern`） | 标"无据结论"，要求评估者重写一次 |
| `gaps` 中列出的缺口在答复里有对应说明 | 机检（关键词） | 缺失即重写一次 |
| `unverifiable` 的任务没有出现在结论段落 | 机检（任务 ID/关键词） | 命中即重写一次 |

重写仍失败 → 照常输出，但开头加声明：

> ⚠️ 本答复中有部分结论未能追溯到已核验的证据，请谨慎使用。

---

### 7.8 评审者：角色与身份的两层分离（moderate / strong 落地）

**结论先行**：评审者**不是**一个自主 LLM Agent；也**不能**只是"同一个 Agent 换一段 prompt"。
它是 **一个单次执行的 `ITurnRunner` 角色（代码形态）+ 一个独立于所有 Worker 的 Agent 身份（错误来源）**。

§2.3 的结论"协调者是角色不是 Agent"**不能**直接搬到评审者身上——两者缺的是不同的东西。

#### 7.8.1 两个轴必须先拆开

| 轴 | 协调者 | 评审者 |
|---|---|---|
| **执行形态**：是否自主决策 | 确定性编排器（0 LLM，规划复用已有分类调用位） | **单次 turn 角色**：一次调用、默认不给工具、结构化输出 |
| **执行身份**：谁来说这段话 | 无所谓，是引擎 | **必须独立于所有 Worker 的 Agent** |

方案原本就是这个结构，只是分散在 §2.1（`Evaluator` 是"承担统一评估的 Agent"）与 §2.2（`IEvaluator` 是 `chat` 组件、经 `ITurnRunner` 执行）两处。
**D1 争的从来不是"要不要做成 Agent"，而是"要不要有第三个身份"。**

#### 7.8.2 为什么"同 Agent 换 prompt"不成立

评审者要解决的是**错误相关性**，不是代码分层：

| 做法 | 后果 | §7.3 等级 |
|---|---|---|
| 同一 Agent、同一模型、换 evaluator prompt | 同一组权重看自己刚写的东西 → 自我确认偏差；prompt 换说法不改权重 | `self` ❌ 只能补格式 |
| 不同 Agent、同 vendor / 同模型 | 上下文与提示词不同，但**同源幻觉、同口径误读**会原样复现 | `moderate` ✅ |
| 不同 Agent、不同 vendor / 不同模型 | 错误来源去相关，能抓到"同源模型一起搞错" | `strong` ✅ 推荐 |

> **"独立"指的是身份与错误来源独立，不是"自主决策独立"。**

#### 7.8.3 为什么也不能是"自主 Agent"

| 若做成自主 Agent | 破坏哪条不变量 |
|---|---|
| 自己决定抽样看几个产出 | **V1 覆盖**：`tasks[]` 必须一个不缺，只有"单次调用 + 引擎校验返回结构"能强制 |
| 在计划里自己声明"不用评估 / 指定评审者" | **V3 决定权**：评审者必须由引擎指派 |
| 带工具回读原文、反复查证 | 成本与延迟不可控（`N+2` 保不住）；且 §7.2 的 L-A 已用 Go 代码把"引文是否真在原文"查掉，**评审者不需要工具** |
| 自主循环 | 无法回归测试（§13.7 依赖可重放） |

这与 §7.2 的分工一致：L-A / L-B 是 0 LLM 的纯函数，只把"口径一致性、是否答全、推测是否被写成事实"这类**语义判断**留给唯一一次 L-C。

#### 7.8.4 两档落地

两档**执行形态完全相同**（同一个 evaluator mode、同一次调用、同一套输出契约），差别只在**评审者从哪来**。

| | **moderate 档**（2 Agent 单位） | **strong 档**（≥3 Agent 单位） |
|---|---|---|
| 前提 | 单位内可用 Agent ≥ 2 | 单位内可用 Agent ≥ 3 |
| 评审者来源 | 从**非生产者**的现有 Agent 中选（`pickEvaluator` 排除 `producerIDs`） | 专职评审 Agent（`t_agent.agent_role=1`，`verify_agent_id` 指向它） |
| 生产侧 | 所有任务收敛到**同一个** Worker（因为剩下的那个要当评审者） | 可跨 Agent 生产（≥2 个 Worker + 1 个评审者） |
| 模型要求 | 无额外要求（与生产者同 vendor 也接受） | `primary_model` 用**不同 vendor**（如 `qwen3.8-flash`），以拿到 `strong` |
| 落库 | `t_turn_task.independence=1` | `t_turn_task.independence=2` |
| 达不到时 | `minIndependence=strong` ⇒ `ErrNoVerifier`，**不进入协作**（绝不静默降档） | — |

配置口径（§11）：`[evaluate] minIndependence`
- `moderate`（默认）：2 Agent 即可协作；
- `strong`：必须有不同 vendor 的独立评审者，否则退化为单 Agent；
- **任何情况下都不允许**"配置要求 strong、实际按 moderate 执行却不声明"——`independence` 必须与实跑等级一致地落库并在事件中带出。

#### 7.8.5 代码落地的四条硬约束

**① `ITurnRunner` 增加 evaluator mode**

```
root | worker | evaluator
```

evaluator mode 的约束全部由代码保证，不靠提示词祈祷：

| 约束 | 实现 |
|---|---|
| 单次调用、无自主循环 | 与 worker 共用 `TurnRunner`，但不进入 tool looper |
| 默认不给工具 | `TurnState` 的 toolset 为空（L-A/L-B 已覆盖回读类校验） |
| 输入受控 | 只喂 §7.2 的三样内容，总量 ≤ `evaluate.inputMaxRunes` |
| 输出受控 | 走 §7.2 的结构化契约校验，缺任务 ID / 缺 `evidence` 即判无效并重试一次 |
| 不参与派发 | `IPlanExecutor.PickTask` 的候选集**排除** evaluator |

`Preparer.PrepareFor` 的签名相应扩展为 `PrepareFor(agent, mode)`，仍不走路由。

**② `pickEvaluator` 加角色过滤（补 §7.3 的实现）**

```go
func pickEvaluator(comCode string, producerIDs []string, min Independence) (string, Independence, error) {
    cands := agentsOf(comCode)
    cands = onlyRole(cands, AgentRoleEvaluator, AgentRoleBoth) // ① 只有评审者角色才能被选中
    cands = exclude(cands, producerIDs...)                    // ② 不得是任何 Worker
    best  := rank(cands, bySkillOverlap(0.6), byVendorDiff(0.3), byLoad(0.1))
    if best == nil { return "", 0, ErrNoIndependentVerifier }
    lvl := independenceOf(best, producerIDs)
    if lvl < min { return "", 0, ErrIndependenceTooLow }      // ③ 不静默降档
    return best.ID, lvl, nil
}
```

**③ `t_agent.agent_role` + 路由池双向排除（当前代码必踩的坑）**

现状 `AgentRouter.availableAgents()` 只过滤 `Deleted` / `Disabled`：

```go
for _, a := range all {
    if !a.Deleted && !a.Disabled {   // 没有角色过滤
        out = append(out, a)
    }
}
```

后果：新增的专职评审者会**自动进入协作/路由候选池**，一旦被选成某个 Worker（技能匹配时尤其容易），V2 就地失效，且**不会报错**——只会悄悄退化成自评。

必须补（§9）：
- `t_agent.agent_role`：`0 worker`（默认）/ `1 evaluator` / `2 both`；
- `availableAgents()` 只返回 `worker|both`（路由与 Worker 派发）；
- `pickEvaluator` 只返回 `evaluator|both`；
- 评审者**禁止** `is_default=1`（否则所有对话默认落到评审者身上）。

**④ 提示词：rubric 放代码，领域知识放 Agent**

| 内容 | 放哪 | 理由 |
|---|---|---|
| rubric 固定表（§7.4）、输出 JSON 契约、V1–V3 约束 | **代码注入的 evaluator mode prompt** | 它们是契约；放进可编辑的 `AGENTS.md` 会被改坏，导致 `tasks[]` 校验大面积失败 |
| 领域知识（口径、单位、业务约定） | 评审 Agent 自己的启动文档 / skills | §7.3：**域匹配优先于独立性**——不懂业务的 strong 评审者比懂业务的 moderate 更没用 |

组合原则：**与生产者共享同一份领域知识，但用不同的模型**——"能判断"与"不会犯同样的错"两者才能同时成立。

#### 7.8.6 与 V1–V3 的对应（自检表）

| 不变量 | 由什么保证 |
|---|---|
| **V1 覆盖** | evaluator 单次调用 + 引擎校验 `tasks[]` 全 ID + 缺即无效重试 |
| **V2 独立性** | `pickEvaluator` 排除 producers + 角色过滤 + `minIndependence` 不降档 |
| **V3 决定权** | 评审者只由 `pickEvaluator` 产生；计划里的 `verify` / 评审者字段由 linter 拒绝 |

#### 7.8.7 选择建议（对应当前单位）

| 当前 `100001` 的 Agent 数 | 建议 | 等级 |
|---|---|---|
| 2（`agent_001`、`agent_002`，同 vendor） | 生产侧收敛为单一 Worker，另一个扮演 evaluator；`minIndependence=moderate` | `moderate` |
| 新增第 3 个"复核助手"（`agent_role=1`，`qwen3.8-flash`） | 生产侧可跨 Agent；`minIndependence=strong` | `strong` |
| 1 | 不协作（§7.6 已强制） | — |

代价说明：新增评审 Agent **不增加 LLM 调用数**（评估那一次本就在 `N+2` 里），用的是它自己的（更便宜的）模型；真正的成本是**多一份配置面**（启动文档、模型、并发）。
这也是把 rubric 放代码、只让 Agent 携带领域知识的原因——尽量把新增 Agent 需要维护的内容压到最小。

---

## 8. 阶段五：落库与历史

### 8.1 历史一致性

- 会话历史文件只写**一条** assistant 记录（评估者产出的最终答复）；
- Worker 的过程只进 `t_turn_task`；
- `t_session.agent_id` = 评审者（UI 徽标有确定值），`routing_mode=1` 表示"本会话入口是规划器"。

### 8.2 落库

- 与今天一致：轮末随 `Journal.Finish` 一次性写（`t_turn` 行本身是轮末插入）；
- 中断轮复用 `FinishPartial`：已完成任务与机检结果仍留痕；
- 评估结果（逐任务 verdict + issues + cross_checks + gaps）与答复一并落库，供审计与回放。

---

## 9. 数据模型

```sql
-- 会话：记录入口模式
ALTER TABLE t_session ADD COLUMN routing_mode INTEGER NOT NULL DEFAULT 0; -- 0 绑定Agent / 1 规划器

-- Agent：协作规模 + 专职评审者
ALTER TABLE t_agent ADD COLUMN max_children     INTEGER NOT NULL DEFAULT 3; -- 单波最大并行 Worker
ALTER TABLE t_agent ADD COLUMN max_tasks        INTEGER NOT NULL DEFAULT 5; -- 单轮最多任务数
ALTER TABLE t_agent ADD COLUMN verify_agent_id  VARCHAR(8);                 -- 专职评审者（可空）
-- 角色：0 worker(默认) / 1 evaluator / 2 both。路由与 Worker 派发只取 worker|both，
-- 评审者指派只取 evaluator|both；详见 §7.8.5（否则专职评审者会被选成 Worker，导致 V2 静默失效）
ALTER TABLE t_agent ADD COLUMN agent_role       INTEGER NOT NULL DEFAULT 0;

-- 分诊留痕
CREATE TABLE IF NOT EXISTS t_route_decision (
    ID          INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id     VARCHAR(40) NOT NULL UNIQUE,
    session_id  VARCHAR(40) NOT NULL,
    mode        INTEGER NOT NULL,          -- 0 single / 1 delegate
    source      INTEGER NOT NULL,          -- disabled/forced/no_verifier/explicit/binding/keyword/struct/llm/fallback
    agent_id    VARCHAR(8),
    intent      VARCHAR(512),
    signals     VARCHAR(64),               -- "2,2,2,2"
    confidence  REAL,
    elapse_ms   INTEGER,
    note        VARCHAR(256),
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_route_decision_session ON t_route_decision(session_id, created_at);

-- 本轮计划（一个 turn 最多一份）
CREATE TABLE IF NOT EXISTS t_turn_plan (
    ID            INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id       VARCHAR(40) NOT NULL UNIQUE,
    session_id    VARCHAR(40) NOT NULL,
    mode          INTEGER NOT NULL DEFAULT 0,   -- 0 single / 1 delegate
    intent        VARCHAR(512),
    evaluator     VARCHAR(8),                   -- 评审者（同时是答复作者）
    source        INTEGER NOT NULL DEFAULT 0,
    signals       VARCHAR(64),
    confidence    REAL,
    plan_json     TEXT,
    validate_note VARCHAR(512),
    attempts      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    finished_at   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_t_turn_plan_session ON t_turn_plan(session_id, created_at);

-- 任务（N 条 produce + 1 条 evaluate）
CREATE TABLE IF NOT EXISTS t_turn_task (
    ID           INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id      VARCHAR(40) NOT NULL,
    seq          INTEGER NOT NULL,
    task_key     VARCHAR(8)  NOT NULL,          -- t1/t2…（evaluate 固定用 "ev"）
    kind         INTEGER NOT NULL DEFAULT 0,    -- 0 produce / 1 evaluate
    agent_id     VARCHAR(8)  NOT NULL,
    action       VARCHAR(512),
    depends      VARCHAR(128),
    wave         INTEGER,
    status       INTEGER NOT NULL DEFAULT 0,    -- 0 pending/1 running/2 ok/3 failed/4 skipped/5 canceled/6 deferred
    verdict      INTEGER,                       -- 0 pass/1 pass_with_notes/2 fail/3 unverifiable
    independence INTEGER,                       -- 仅 kind=1: 1 moderate / 2 strong
    checked      VARCHAR(512),                  -- 已核对的 claim id（逗号分隔）
    issues_json  TEXT,                          -- 未解决 issue
    cross_json   TEXT,                          -- 仅 kind=1: 跨任务一致性结论
    gaps_json    TEXT,                          -- 仅 kind=1: 未采纳的结论
    started_at   INTEGER, finished_at INTEGER, elapse_ms INTEGER,
    summary      TEXT,
    tokens_in    INTEGER, tokens_out INTEGER,
    error_code   VARCHAR(64),
    FOREIGN KEY (turn_id) REFERENCES t_turn(turn_id)
);
CREATE INDEX IF NOT EXISTS idx_t_turn_task_turn ON t_turn_task(turn_id, seq);
```

> 逐任务 verdict 落在对应的 produce 行上（由评估结果回填），不另起表；`kind=1` 只有一行，因为评估只有一次。

---

## 10. 事件与 UI

前端按 payload 键嗅探，因此新事件**只用 `type` 键**（旧前端静默忽略）：

```json
{"type":"plan","intent":"对比 A、B 两个资产包的处置价值","mode":"delegate","tasks":[{"key":"t1","agent":"agent_001","depends_on":[]}]}
{"type":"task","key":"t1","agent":"agent_001","status":"running"}
{"type":"task","key":"t1","agent":"agent_001","status":"ok","ms":18400,"machine":"pass"}
{"type":"task","key":"t1","agent":"agent_001","status":"ok","ms":21000,"machine":"repaired"}
{"type":"evaluate","evaluator":"agent_002","independence":"strong","status":"running"}
{"type":"evaluate","status":"done","verdicts":{"t1":"pass","t2":"fail","t3":"pass_with_notes"},"gaps":1,"ms":9200}
{"type":"plan_done","ok":2,"failed":1,"unverifiable":0,"ms":42100}
```

- Worker **不转发正文**（多路交错会写乱 SSE）；答复正文只有评估者那一份，走常规 `message` 增量；
- 事件按**状态变化**发送，不按 token；
- 复用 `SSEPresenter.emit` 的锁。

UI 呈现：一张"协作计划"卡片（intent + 分层的任务状态灯 + 最后一次评估的逐任务裁决），失败/未核实用与工具错误一致的色标，答复里必须有对应"缺口"段落。

---

## 11. 配置

沿用本项目 ini 约定（`[section]` + 平铺 key，与 `[toolRule] functionCalling`、`[chat] sessionRecord` 同风格），写入 `agentService.ini`：

```ini
[routing]
; 总开关。false = 完全不判定，退化为今天的路由（L0）
enabled=false
; 只判定并落 t_route_decision，执行仍走单 Agent（L1 推荐首个上线形态）
dryRun=true
; 灰度期：false 时分诊照跑但一律走单 Agent（L2）
allowDelegate=false
; 进入 delegate 的置信度门槛
delegateConfidence=0.6
; 命中 L1 结构强信号时，即使 LLM 给 single 也强制进入 B 段
strongSignalOverride=true
; 能力跨度阈值：涉及几个不同 Agent 才算复杂
minCapabilitySpan=2
; 材料数量强信号阈值
minMaterialsStrong=3
; 关键词命中的语义：hint(只加分) / hard(直接 single)
keywordRuleMode=hint
; speed / balanced / quality，只映射 delegateConfidence 与 minMaterialsStrong
qualityPreset=balanced

[plan]
maxTasks=5
maxParallel=3
taskTimeoutSec=180
; 监控阈值：无事件多久记 warn / 多久取消（工具运行中不适用）
stallWarnSec=60
stallKillSec=180
; Worker 产出摘要上限（汇聚时按比例缩短不低于 300）
summaryMaxRunes=800
triageMaxTokens=1024
lintRetries=2
; 专职评审者；优先级：本配置 > t_agent.verify_agent_id > 自动指派
evaluatorAgentId=

[evaluate]
; true = 评估照跑照落库，但评估失败不改动答复（L3）
shadowOnly=true
; strong 或 moderate；weak/self 不可能被接受
minIndependence=moderate
; 评估输入总长上限（汇聚阶段按比例缩短 summary）
inputMaxRunes=12000
maxRepairRounds=1
maxRepairTasks=2
; 评审调用可占本轮预算的比例上限
budgetRatio=0.3
; blocker 未解决时：true = 降级标注后仍输出；false = 整轮失败
blockOnBlocker=true
```

**开关语义（逐项）**

| 开关 | 作用 | 何时改 | 回滚效果 |
|---|---|---|---|
| `complexity.enabled=false` | 完全不判定，退化为今天的路由 | L1 起置 `true` | **唯一正当的整体回滚开关** |
| `complexity.dryRun=true` | 照常判定并落 `t_route_decision`，执行仍走单 Agent | L2 起置 `false` | 恢复"只记录不执行" |
| `complexity.allowDelegate` | 灰度期开关：`false` 时分诊照跑但一律走单 Agent | L3 起置 `true` | 协作能力关闭，执行器代码保留 |
| `evaluate.shadowOnly=true` | 评估照跑照落库，但不因评估失败而改动答复 | L4 置 `false` | L4→L3 只需改这一行 |
| `plan.maxParallel=1` | 退化为串行，能力不变 | 并发异常时 | 波次仍正确，只是慢 |
| `qualityPreset` | `speed`/`balanced`/`quality`，只映射两个阈值 | 调优期 | 无副作用 |


**没有 `evaluate.enabled=false`**：要关闭评估，唯一正当方式是 `complexity.enabled=false`（即不进入协作）。

`evaluate.budgetRatio` 指评审调用可占本轮预算的比例上限；因为只有 1 次，通常远低于产出的消耗。

---

## 12. 代码改动清单

按依赖顺序（前面的必须先做）：

| # | 文件 | 改动 | 规模 |
|---|---|---|---|
| 1 | `chat/turn_runner.go` **新增** | 从 `execute` 剥出 `TurnRequest`/`ITurnRunner`（root/worker/evaluate 共用）；行为零变化 | 中 |
| 2 | `chat/preparer.go` | 新增 `PrepareFor(ctx, cmd, agent, kind)`；`Prepare` 保留为薄包装 | 小 |
| 3 | `harness/ctxmgr/complexity.go` **新增** | `ComplexitySignals` + `DetectSignals`（纯函数） | 中 |
| 4 | `harness/ctxmgr/route_decision.go` **新增** | `RouteMode`/`RouteSource`/`Decision`/`Plan`/`PlanTask` | 小 |
| 5 | `harness/ctxmgr/triager.go` **新增** | `ITriager`：一次调用完成分诊 + 计划；三级退化 + linter 回填 + `canDelegate` | 中 |
| 6 | `harness/ctxmgr/plan_prompt.go` **新增** | 分诊提示词、候选目录渲染、任务书模板（Worker 侧另见 `chat/worker_prompt.go`） | 小 |
| 7 | `harness/ctxmgr/agent_router.go` | 新增 `ResolveDecision`；`Resolve` 改薄包装；A 段顺序按 §4.2 重排 | 中 |
| 8 | `harness/ctxmgr/session_agent_mgr.go` | 缓存条目加 `routingMode` | 小 |
| 9 | `chat/plan_executor.go` **新增** | DAG→wave、scope、子闸、超时、机检触发、调用评估 | 中 |
| 9a | `chat/monitor.go` **新增** | `monitorSink`（最后事件/工具在跑）、watchdog（warn/kill）、`readyToEvaluate` 状态机 | 中 |
| 9b | `chat/aggregate.go` **新增** | `BuildEvaluationBundle`：稳定排序、gaps 预填充、体积控制（纯函数） | 小 |
| 10 | `chat/evaluator_assign.go` **新增** | `pickEvaluator`（排除全部生产者 / 域匹配 / 负载 / 独立性判定） | 小 |
| 11 | `chat/machine_check.go` **新增** | L-A/L-B 逐任务机检：引用回读、数字定位、派生值重算、结构完整性；synthesis 机检 | 中 |
| 12 | `chat/evaluate_prompt.go`、`chat/worker_prompt.go` **新增** | 统一评估 prompt（逐任务裁决 + cross_checks + gaps + answer）+ 输出有效性与覆盖率校验；Worker 任务书与 claims 契约 | 中 |
| 13 | `service/chatting_service.go` | `execute` 改为 `prepare → [分诊?] → executor 或 runner → journal`；委托路径不再直接 invoke | 中 |
| 14 | `model/dataobject/` | `TSession.RoutingMode`；`t_route_decision.go`/`t_turn_plan.go`/`t_turn_task.go` | 小 |
| 15 | `model/dataaccessor/` | 三表批量写（轮末）+ `SetSessionRoutingMode` | 小 |
| 16 | `data/ddl_sqlite3.sql` | §9 全部 DDL | 小 |
| 17 | `controller/sse_presenter.go` | `OnPlan/OnTask/OnEvaluate`（复用 `emit`） | 小 |
| 18 | `chat/chat_common.go` | `TurnTiming` 加 `TriageMs/TasksMs/EvaluateMs/TaskCount` | 小 |
| 19 | `harness/security/` | `IssueRepetition`（沿用 loop_guard 结构） | 小 |

**复用而非新造**：引用回读用 `pCommon.SafeJoin`+`pCommon.ReadFile`（同 `LoadHistoryTurnRecords`）；关键标记用 `ctxmgr.KeyTokenPattern`；重算用 `float64` + 相对容差；循环终止沿用 `loop_guard`。

---

## 13. 执行计划

> 本节把 §1–§12 的设计拆成**可执行、可验收、可回滚**的工程步骤。
> 旧编号对照：P0→M1、P1→M2、P2→M3、P3→M4、P4→M5、P5→M6、P6→M7、P7→M8；M0/M9 为新增（决策基线与灰度）。

### 13.1 怎么用这份计划

1. **先看 §13.2.1 的三项决策**——不拍板就动手，返工量约为 3 人日。
2. **一个里程碑一个门禁（DoD）**：门禁不过不进下一阶段，也不并行开下一个 M。
3. **一个任务一个 PR**：提交说明带任务 ID + 验收证据 + 回滚方式（§13.6）。
4. **贯穿原则**：先做零行为变化的重构（M1），再加新能力；任何阶段都能用**一行配置**退回今天的行为（§11）。
5. **测试用例编号统一引用 §14**，本节只说"什么时候跑哪一层"。

### 13.2 开工前置

#### 13.2.1 必须拍板的 3 件事（🔒 阻塞）

| # | 决策 | 为什么阻塞 | 建议默认 | 影响面 |
|---|---|---|---|---|
| **D1** | 是否新增专职评审 Agent | 评审者不得是任何 Worker（V2）。当前单位只有 2 个 Agent（`agent_001/002`，同 vendor），不新增就只能"单生产者 + 单评审者"，**生产侧无法跨 Agent** | **新增**一条 `t_agent`（"复核助手"，`primary_model=qwen3.8-flash`），并用 `verify_agent_id` 指定 | M5（指派）、§14 用例 23/25 |
| **D2** | 评估与答复是否合一 | 合一 = `N+2`；分开 = `N+3` | **合一**（评审者同时写答复） | M5、§4.5 成本模型 |
| **D3** | `blockOnBlocker` 默认值 | 决定"有 blocker 时是否仍出答复" | `true`（降级标注后仍输出，而不是整轮失败） | M5（降级路径）、§14 用例 34 |

> D1 若选"不新增"，请一并接受：本方案在 2-Agent 单位内的实际形态是"1 个 Worker + 1 个评审者"，`maxTasks` 必须降到 2，且 §14 用例 15/19 的部分断言需改为 `no_verifier` 回落。
>
> **D1 → 独立性等级**：新增专职评审者 ⇒ `strong`；不新增 ⇒ `moderate`（生产侧收敛为单一 Agent）。两档的代码落地、`agent_role` 字段、不降档规则与失败路径见 **§7.8**。

#### 13.2.2 环境与基线（0.5 人日，属 M0）

| 步骤 | 命令 / 动作 | 通过标准 |
|---|---|---|
| 建分支 | `git checkout -b feature/coordinator-worker` | — |
| 基线测试 | `cd src && go test ./...` | 全绿；先记录**当前失败项**，登记为"基线已知问题" |
| 基线 lint | `golangci-lint run` | 与基线一致，不引入新告警 |
| 基线冒烟 | `src/scripts/chat_100_e2e.py` 跑一轮，记录：命中 Agent、首字节时间、总耗时、token | 存为 `M0-baseline.json`（放 session 工作区，不入库） |
| 记录架构约束 | 确认 `archtest`（禁止裸 `go`）、shutdown 白名单、`leak_test` 思路 | 新增代码不得触发红 |
| 配置骨架 | `agentService.ini` 预留 `[routing]`/`[plan]`/`[evaluate]` 三段（全部关闭） | 能解析且默认关闭，行为不变 |
| 备份数据库 | 复制 `data/cclaw.db` | 迁移失败时可直接回退 |

> **基线的用处**：M1 是纯重构，"行为零变化"必须靠数据对比，不能靠感觉。

### 13.3 里程碑总览

| 里程碑 | 对应设计 | 目标 | 主要交付物 | 门禁（DoD） | 人日 |
|---|---|---|---|---|---|
| **M0** | — | 决策 + 基线 | 决策记录、`M0-baseline.json`、分支、配置骨架 | D1–D3 定稿；基线数据入库；`enabled=false` 时行为与 HEAD 一致 | 0.5 |
| **M1** | §12-1/2 | 抽出单轮执行单元（**零行为变化**） | `turn_runner.go`、`PrepareFor` | 现有测试全绿；冒烟对比基线一致（Agent/输出/token 相同，延迟 ±10%） | 1.5 |
| **M2** | §4.1 | 复杂度纯函数 + 用例集 | `complexity.go` + 单测 | 用例全绿（含 2 条"长度陷阱"）；纯函数、< 5ms | 2 |
| **M3** | §4.2–4.3 | 分诊接入 + **dryRun 只落库** | `triager.go`、`route_decision.go`、`plan_prompt.go` | dryRun 下 100% 走单 Agent，输出与基线一致；决策可查 | 3 |
| **M4** | §4.4 | 打开 single-only + 会话模式 | `routing_mode`、缓存扩展 | 协作率 0；简单路径 p95 延迟 ±5% | 1.5 |
| **M5** | §5、§6.7、§7 | delegate 执行（串行）+ 汇聚 + 统一评估 | `plan.go`、`plan_executor.go`、`aggregate.go`、`evaluator_*` | 3 任务含依赖跑通；调用数 = `N+2`；评估覆盖全部任务 | 5.0 |
| **M6** | §6.1–6.6 | 并行 + 并发治理 + **监控** + 中断 | scope 调度、子闸、`monitor.go` | 中断 → 全树停；同 Agent 不超 `subagents`；监控不误杀不漏判；无裸 goroutine | 3.5 |
| **M7** | §7.2、§7.5 | 机检 + 修复回路 + synthesis 机检 | `machine_check.go`、回路 | 机检覆盖率 100%；无评审者时不协作；级联上限生效 | 3.5 |
| **M8** | §8–§10 | 落库 + 事件 + UI | 三表、SSE、计划卡片 | 刷新可恢复；中断轮留痕 | 2.5 |
| **M9** | §13.8–13.9 | 灰度与调优 | 灰度报告、阈值定稿 | 指标达标；无 P1 故障 | 持续 |

合计约 **23 人日**（1 人全职约 5 周；2 人并行约 3 周，见 §13.11）。

### 13.4 任务分解

每个任务给出 **ID / 交付物 / 改动点 / 关键步骤 / 验收**；里程碑末尾统一给门禁与回滚。

#### M0 前置（0.5 人日）

| ID | 任务 | 改动点 | 步骤 | 验收 |
|---|---|---|---|---|
| T0.1 | 决策记录 | 本文 §16（勾选并补理由） | 把 D1–D3 结论写回 §16，标注日期与决策人 | 三项都有结论与理由 |
| T0.2 | 基线采集 | — | 跑基线测试 + 冒烟，存数据 | `M0-baseline.json` 可复现 |
| T0.3 | 配置骨架 | `agentService.ini` + 配置结构体 | 三段预留，默认全关；解析失败要有明确日志 | `enabled=false` 行为与 HEAD 逐字节一致 |

#### M1 抽出单轮执行单元（1.5 人日）— **零行为变化**

整条链的地基，**此阶段不引入任何新能力**。

| ID | 任务 | 改动点 | 步骤 |
|---|---|---|---|
| T1.1 | 定义执行单元契约 | `chat/turn_runner.go` **新增** | 定义 `TurnKind{Root,Produce,Evaluate}`、`TurnRequest`（Agent/UserMsg/Recordable/DisableTools/ParentTrace）、`TurnOutcome`（Answer/Usage/ToolCalls/Elapsed）、`ITurnRunner` |
| T1.2 | 实现 `TurnRunner` | 同上 | 把 `execute` 的第 5–8 段（并发闸 → 中断登记 → 超时 → 建档 → 装配 → invoke → 收尾记账）搬入，把 `Recordable/DisableTools` 参数化 |
| T1.3 | 增加 `PrepareFor` | `chat/preparer.go` | 抽出"不含路由"的构造函数；`Prepare` = `Resolve` + `PrepareFor` |
| T1.4 | `execute` 改薄 | `service/chatting_service.go` | `execute` = `Prepare` + `runner.Run` + 返回；删除搬走的逻辑，不留双份实现 |
| T1.5 | 等价性验证 | — | 冒烟 3 轮对比 `M0-baseline.json`：Agent 相同、输出相同、延迟 ±10%、token 相同；**额外覆盖一次用户中断** |

**门禁**：`go test ./...`、`golangci-lint run`、`archtest` 全绿；冒烟对比通过；`git diff` 审查**不得**出现行为变更（除函数搬家）。

**回滚**：纯内部重构，无开关；异常直接 revert 单个 PR。

**风险**：`execute` 里有一段"中断轮也做记忆维护"的分支，搬动时容易丢 → 对比测试**必须**覆盖中断场景（对应 §14 用例 18 的前置）。

#### M2 复杂度纯函数（2 人日）

| ID | 任务 | 改动点 | 步骤 |
|---|---|---|---|
| T2.1 | 信号结构 | `harness/ctxmgr/complexity.go` **新增** | `ComplexitySignals{CapabilitySpan, EvidenceBreadth, DependencyDepth, Deliverable, Materials[], HitAgents[], WeakSignalHits}` |
| T2.2 | 材料与实体抽取 | 同上 | 正则抽文件名/编号/日期；按类型去重；识别"分别/各自/逐个/再/然后" |
| T2.3 | 阶段与交付物计数 | 同上 | 阶段连接词表；交付物词表（报告/表格/清单/结论/方案…） |
| T2.4 | 能力跨度 | 同上 + `agentRepo.GetSkillsByAgent` | 每 Agent 命中技能数 → 涉及几个不同 Agent |
| T2.5 | 用例集 | `complexity_test.go` | ≥12 条，含：3000 字总结 = simple、双债务人串行 = delegate、3 份材料 = delegate、跨 2 Agent = delegate |

**门禁**：用例全绿；`DetectSignals` 是纯函数（无 IO、无 LLM，签名可断言）；基准测试单次 < 5ms。

**回滚**：尚未被生产代码引用，零风险。

**注意**："弱信号"字段命名用 `WeakSignalHits`，**不要**与评审独立性的 `weak` 等级混用。

#### M3 分诊接入 + dryRun（3 人日）

| ID | 任务 | 改动点 | 步骤 |
|---|---|---|---|
| T3.1 | 值对象 | `harness/ctxmgr/route_decision.go` **新增** | `RouteMode`、`RouteSource`、`Decision{Mode,Agent,Plan,Source,Intent,Signals,Confidence,ElapseMs,Note}`、`Plan`、`PlanTask` |
| T3.2 | 分诊提示词 | `harness/ctxmgr/plan_prompt.go` **新增** | 四维判据 + 反例 + 输出 schema + 候选目录渲染（只给 ID/名称/领域标签，**不给**启动文档正文） |
| T3.3 | 分诊器 | `harness/ctxmgr/triager.go` **新增** | 一次 `SimpleComplete`；严格 JSON → 文本 AgentId 兜底 → 失败回落；`signals` 自洽校验；`delegateConfidence` 门槛；`canDelegate` 前置（评审者/预算/时间） |
| T3.4 | 计划 linter | 同上 | §5.2 全规则；**拒绝 `verify/review` 字段**（V3）；依赖闭包修正；有效任务 < 2 → 降级 |
| T3.5 | 回填修正 | 同上 | 结构化错误回填，≤`lintRetries`（2） |
| T3.6 | 路由接入 | `agent_router.go` | 新增 `ResolveDecision`；`Resolve` = 取 `Decision.Agent`；A 段顺序按 §4.2 first-match-wins |
| T3.7 | 决策留痕 | `t_route_decision` + accessor + `SetSessionRoutingMode` 预留 | 轮末批量写；`dryRun` 时**也写** |
| T3.8 | 配置接入 | `service_main.go` + ini | `[routing]` 段 → 结构体；默认 `enabled=false` |

**门禁**：`dryRun=true` 灰度环境跑 20 条问题，**全部**走单 Agent 且输出与基线一致；`t_route_decision` 20 条记录齐全（`source`/`signals`/`confidence`）；`enabled=false` 时与 M1 完全一致。

**回滚**：`[routing] enabled=false`（一行）。

**风险**：分诊调用超时拖慢主链路 → 沿用现有 10s 超时 + 失败静默回落；**必须有超时注入测试**。

#### M4 打开 single-only（1.5 人日）

| ID | 任务 | 改动点 | 步骤 |
|---|---|---|---|
| T4.1 | 会话模式落库 | `t_session.routing_mode` + `TSession.RoutingMode` + `SetSessionRoutingMode` | single 时写 0 |
| T4.2 | 缓存扩展 | `session_agent_mgr.go` | 条目加 `routingMode`；命中优先，未命中读 DB 一次 |
| T4.3 | 灰度打开 | 配置 | `enabled=true, dryRun=false, allowDelegate=false`（delegate 一律回落 single） |

**门禁**：协作率 0；简单路径 p95 延迟与 token 相对基线 ±5%；`routing_mode` 落库正确。

**回滚**：`enabled=false`。

#### M5 执行器 + 汇聚 + 统一评估（5.0 人日）

先做**串行**版本（波次内也串行）把正确性跑通，M6 再开并行。

| ID | 任务 | 改动点 | 步骤 |
|---|---|---|---|
| T5.1 | 计划值对象 | `chat/plan.go` **新增** | `PlanMode`、`Plan`、`PlanTask`（含 `ExpectedOutput`、`Seq`、`DependsOn`） |
| T5.2 | 执行器骨架 | `chat/plan_executor.go` **新增** | 拓扑排序 → wave；串行 `runProduce`；状态机 `pending/running/ok/failed/skipped/deferred/canceled` |
| T5.3 | Worker 提示词 | `chat/worker_prompt.go` **新增** | 任务书模板 + **claims 产出契约**（§7.2）+ 上游结果标 `[provisional]` + 摘要长度上限。**claims schema 在此定稿**，M7 才能并行开工 |
| T5.4 | 评审者指派 | `chat/evaluator_assign.go` **新增** | 排除全部生产者 → 域匹配/独立性/负载排序；`ErrNoIndependentVerifier` |
| T5.5 | 统一评估提示词 | `chat/evaluate_prompt.go` **新增** | 逐任务裁决 + `cross_checks` + `gaps` + `answer`；输入体积控制（`inputMaxRunes`） |
| T5.6 | 覆盖率校验 | 同上 | `tasks[]` 必须覆盖**全部**任务 ID（强制不变量 V1，§7.1）；缺 → 无效评估 → 重试 1 次 |
| T5.7 | 评估后级联修复 | `plan_executor.go` | blocker 任务 + 传递依赖者重跑 → 重评估；`maxRepairRounds`/`maxRepairTasks` |
| T5.8 | 降级路径 | 同上 | 全部失败 → default Agent 单跑；评估失败 → 拼接摘要 + 声明"未经评估" |
| T5.9 | **汇聚（协调者第 ④ 职责）** | `chat/aggregate.go` **新增** | `BuildEvaluationBundle`：按 `seq` 稳定排序、`Gaps` 由执行期事实预填充、体积按比例缩短（下限 300）、不喂推理过程。**纯函数**，对 `MachineGate` 只依赖接口（M7 前用 stub） |
| T5.10 | **评估触发（汇合点）** | `chat/monitor.go` **新增**（本阶段只含状态机部分） | `readyToEvaluate()`：全部终态才 true；`deferred` 重排 ≤ `maxDefer`(2) → `failed(CAPACITY)`；全失败/中断不触发；触发 = 执行器同步 `runner.Run(Kind=Evaluate)`；`kind=1` 行在**触发时**创建 |

> 机检触发的"就地修复"放在 M7（T7.4），因为它依赖机检层；M5 只做"评估给出 blocker 之后"的回路。
> T5.9 与 T5.10 是 §6.7 / §6.6 的落地，**不可省**：没有它们，`N+2` 就退化成一段不可断言的黑盒。

**门禁**
- 3 任务（t1、t2 无依赖，t3 依赖二者）串行跑通，调用数 **恰为 `N+2`**；
- 评估 `tasks[]` 覆盖 3/3；
- 上游失败 → 下游 `skipped`，且 `Gaps` 在**调用评估前**就含该条（§14 用例 40）；
- 同一批结果汇聚两次 → 序列化结果**字节一致**（§14 用例 42）；
- 3 任务先后完成 → 只在全部终态后触发 **1 次**评估（§14 用例 43）；
- 无评审者（人为把单位缩成 1 个 Agent）→ 走单 Agent，行为如今天。

**回滚**：`allowDelegate=false`（执行器代码保留，只是不进入）。

**风险**：评审者与 Worker 抢同一 Agent 的并发额 → 已用独立闸 key（`#eval`），门禁里要验证。

#### M6 并行 + 并发治理 + 监控（3.5 人日）

| ID | 任务 | 改动点 | 步骤 |
|---|---|---|---|
| T6.1 | wave 并行 | `plan_executor.go` | 每波 `NewScope`（**不可复用**：`Wait()` 会永久 draining）；`width = min(maxParallel, len(wave), Σ SubAgents)` |
| T6.2 | 子闸 | 同上 | key = `agentId + "#task"`，limit = `SubAgents`（0→1）；**不能**复用 root 的 key（`ConcurrentAdvance` 的 limit 由该 key 首次调用决定） |
| T6.3 | 闸位超时 | 同上 | 10s 未获得 → `deferred` → 重排下一波（≤2 次）→ 用尽转 `failed(CAPACITY)` |
| T6.4 | 预算截尾 | 同上 | 每波前算剩余，按 `Priority` 截尾；**必须预留评估预算**（`evaluate.budgetRatio`） |
| T6.5 | 中断级联 | 验证为主 | `runCtx` 派生；`scope.Cancel()`；中断后任务 `canceled` 落库 |
| T6.6 | 并发压测 | — | 10 并发会话 × 3 任务，观察 `AllRoutineStats` 无泄漏 |
| T6.7 | **监控（协调者第 ③ 职责）** | `chat/monitor.go` 扩展 | `monitorSink` 记录 `OnDelta/OnReasoning/OnTool` 的最后事件时刻与"工具是否在跑"；watchdog：`stallWarnSec` 记 `slow`+warn，`stallKillSec` 取消→`failed(STALL)`；工具运行中**豁免**（复用 `ToolCallingHeartbeatInterval=3s`）；`taskTimeoutSec` 硬兜底→`failed(TIMEOUT)`（**不依赖事件**，防"一直有心跳但逻辑挂死"） |

**门禁**
- 中断父轮 → 全部子任务 5s 内停止；
- 同 Agent 子任务数从不超过 `subagents`；
- `archtest` 全绿（无裸 `go`）；
- 压测后 inflight 归零，无 goroutine 泄漏；
- 监控四条：60s 静默只 warn 不杀（用例 35）、180s 无事件被杀（36）、工具心跳不误杀（37）、超时硬兜底（38）。

**回滚**：`maxParallel=1`（退化为串行，能力不变）；监控阈值调大可临时缓解误杀。

#### M7 机检 + 修复回路 + synthesis 机检（3.5 人日）

机检是纯函数，**可与 M5/M6 并行开发**（前提：T5.3 的 claims schema 已定稿）。

| ID | 任务 | 改动点 | 步骤 |
|---|---|---|---|
| T7.1 | L-A 引用回读 | `chat/machine_check.go` **新增** | 文件存在 + `quote` 定位（复用 `pCommon.SafeJoin` + `pCommon.ReadFile`，同 `LoadHistoryTurnRecords`） |
| T7.2 | L-A 派生重算 | 同上 | `operands` 重算，相对容差 0.5% |
| T7.3 | L-B 结构检 | 同上 | `expected_output` 的字段/表头/条目数；单位与口径必填 |
| T7.4 | 机检接入 | `plan_executor.go` | Worker 完成即跑；blocker → 就地修复（不进入评估） |
| T7.5 | synthesis 机检 | `machine_check.go` | 答复里的数字必须能追溯到已通过评估的 `claims`；缺口说明存在性 |
| T7.6 | issue 重复检测 | `harness/security/` | 复用 `loop_guard` 结构；同一 blocker 修复后仍在 → 立即停 |
| T7.7 | shadow 模式收尾 | 配置 | `shadowOnly=true` 下评估照跑照落库，但不改写答复 |

**门禁**（对应 §14.4 用例 26–34）
编造引文 → L-A blocker；派生值算错 → L-A blocker；口径不符 → L-C blocker 且定位到 claim；t1/t2 口径不一致 → `cross_checks` 命中；材料缺项 → `unverifiable`（不是 fail）；`shadowOnly=true` 时答复与 M5 一致。

**回滚**：`evaluate.shadowOnly=true`（恢复"只记录不改写"）。

#### M8 落库 + 事件 + UI（2.5 人日）

| ID | 任务 | 改动点 | 步骤 |
|---|---|---|---|
| T8.1 | 三表 DDL + 迁移 | `data/ddl_sqlite3.sql` + 迁移脚本 | `ALTER TABLE t_session ADD routing_mode`；`ALTER TABLE t_agent ADD max_children/max_tasks/verify_agent_id`；`CREATE t_route_decision / t_turn_plan / t_turn_task`。**无 auto-migrate**，必须脚本 + 更新 DDL 双写 |
| T8.2 | 实体与写入 | `model/dataobject/`、`model/dataaccessor/` | 轮末批量写；中断轮走 `FinishPartial` 仍写任务与机检结果 |
| T8.3 | SSE | `controller/sse_presenter.go` | `OnPlan/OnTask/OnEvaluate`（复用 `emit` 锁）；新事件**只用 `type` 键**，旧前端静默忽略 |
| T8.4 | 前端 | `scripts/modules/api.js`、`chat.js` | 新 `type` 分支；计划卡片 + 逐任务状态灯 + 评估裁决 |
| T8.5 | 恢复 | `GetSessionTurns` 扩展或新增 `/api/GetTurnPlan` | 刷新可看到计划与裁决 |

**门禁**：刷新可恢复；中断轮的任务与裁决可查（`kind=1` 行缺失本身可查）；旧前端不报错。

**回滚**：前端按 `type` 忽略新事件；后端只写不读。

#### M9 灰度与调优（持续）

| ID | 任务 | 步骤 |
|---|---|---|
| T9.1 | 逐级灰度 | 按 §13.8 L0→L4 推进，每级记录出口指标 |
| T9.2 | 阈值校准 | 依据 §13.9 的协作率/误报率，调 `delegateConfidence`、`minMaterialsStrong`、`maxRepairTasks` |
| T9.3 | 报告归档 | 灰度报告 + 最终配置定稿 + §16 遗留项关闭 |

### 13.5 依赖图

```
M0 ──▶ M1 ──┬──▶ M2 ──▶ M3 ──▶ M4 ──┬──▶ M5 ──▶ M6
            │                        │
            └────────────────────────┴──▶ T7.1~T7.3（机检纯函数，可并行）
                                             │
                                             ▼
                                       M7 ──▶ M8 ──▶ M9
```

| 约束 | 原因 |
|---|---|
| 所有后续阶段必须在 M1 之后 | 全部依赖 `TurnRunner` 与 `PrepareFor` |
| M2 可与 M3 的前半部分并行 | 信号函数与提示词互不依赖 |
| T7.1–T7.3 可与 M5/M6 并行 | 纯函数 + 单测；接入口在 T7.4 |
| T5.9 依赖 T5.3 的 claims schema | 汇聚要读 `Claims`；M7 前用 stub 满足接口 |
| M8 可最后做 | 不落库也能灰度（只是不可回放） |

### 13.6 分支与提交约定

| 项 | 约定 |
|---|---|
| 分支 | `feature/coordinator-worker`；每里程碑一个子分支 `feat/m1-turn-runner` … |
| PR 粒度 | 一个任务一个 PR；M1 允许拆 2 个（契约 / 搬迁） |
| 提交粒度 | 一个可编译、可测试的单元；禁止"半成品提交" |
| 每个 PR 必须包含 | 改动说明（任务 ID）+ 验收证据（命令输出/对比数据）+ 回滚方式 |
| **禁止** | 在 M1–M4 阶段引入任何可见行为变化 |

### 13.7 测试执行方案

| 层次 | 内容 | 何时跑 |
|---|---|---|
| 单元 | `complexity`（≥12）、linter（≥10）、`machine_check`（≥8）、`evaluator_assign`（≥6）、`aggregate`（≥5）、`monitor`/`readyToEvaluate`（≥5）、`plan_executor`（≥10） | 每次提交 |
| 契约 | `chat_controller_test.go` 扩展：单 Agent 路径输出不变（对照基线） | 每个 PR |
| 架构 | `archtest`（裸 go / shutdown / leak） | 每个 PR |
| 集成 | 起真实服务 + mock LLM：dryRun、single、delegate(N=2/3/5)、中断、超时、无评审者 | M3/M5/M6/M7 门禁 |
| 回归 | `src/scripts/chat_100_e2e.py` 对比基线（Agent、首字节、总时长、token） | 每个里程碑 |
| 压测 | 10 并发会话 × 3 任务；inflight 归零、无 goroutine 泄漏 | M6 门禁 |
| 故障注入 | LLM 超时、评估返回非法 JSON、评审者被禁用、预算耗尽、闸位耗尽、工具长跑占心跳 | M5/M6/M7 门禁 |

**关键等价性测试（M1 命门）**：同一问题在 M1 前后各跑 3 次，比较 `agent_id / 输出文本 / provider / token / 是否触发工具`，**必须完全一致**（延迟允许 ±10%）。

用例清单见 §14；本节只规定"哪一层、什么时候跑"。

### 13.8 灰度发布（四级开关）

| 级 | 配置 | 观察 | 出口条件 |
|---|---|---|---|
| **L0 关闭** | `enabled=false` | 与今天一致 | 部署后 24h 无异常 |
| **L1 影子** | `enabled=true, dryRun=true` | 只落 `t_route_decision`，不执行 | 跑满 1 周；协作率 5%~20%；抽检 30 条决策无误判 |
| **L2 单 Agent** | `dryRun=false, allowDelegate=false` | 分诊生效但只走单 Agent | 1 周；p95 延迟 ±5%；无新增错误码 |
| **L3 协作小流量** | `allowDelegate=true, shadowOnly=true`，按单位/用户灰 | 协作真跑，评估只记录 | 2 周；`N+2` 调用计数正确；无 P1 故障 |
| **L4 全量** | `shadowOnly=false` | 评估真正约束答复 | 2 周；缺口说明正确；无"假已校验"投诉 |

**每级回滚**：改一个配置项 + 重启（或热加载）。L4 → L3 只需 `shadowOnly=true`。

### 13.9 上线后观察（2 周）

| 指标 | 目标 | 异常处理 |
|---|---|---|
| 协作率（delegate 占比） | 5%~20% | 过低 → 放宽 `delegateConfidence`；过高 → 收紧 |
| 简单路径 p95 延迟 | 与基线 ±5% | 超标 → 查是否没命中 A 段短路、被强制走 LLM |
| 协作轮 p95 延迟 | < 基线 × 4 | 超标 → 减 `maxTasks`/`maxParallel` |
| 评估覆盖率 | 100% | < 100% → 视为缺陷，立即修 |
| 独立性分布 | `strong` ≥ 50% | 低 → 检查 `qwen3.8-flash` 是否配到评审者 |
| 机检拦截率 | > 60% | 低 → 检查 claims 契约是否被 Worker 遵守 |
| 评估抓错率（人工抽检 30 条） | > 70% | 低 → 修评审 prompt 或换模型 |
| 误报率 | < 20% | 高 → 提高 `evidence` 要求，或退回 `shadowOnly` |
| 无据结论数 | 0 | > 0 → synthesis 机检未生效 |
| `stall_count` / `timeout_count` | 前者可有、后者应低 | 前者高 → 检查心跳豁免；后者高 → 调 `taskTimeoutSec` |
| 单轮成本 | 协作轮 ≈ 单 Agent × 3~4 | 超出 → 核对是否有多余调用（应为 `N+2`） |

**每日必看**：`t_route_decision.source` 分布（哪条短路最多）、`no_verifier`/`no_budget` 次数、`degrade` 次数。

### 13.10 交付风险与应急

> 本节是**工程交付**风险；设计层面的风险见 §15。

| 风险 | 触发信号 | 应急 |
|---|---|---|
| 分诊拖慢主链路 | 简单路径 p95 +20% | 关 `enabled`；检查是否未命中短路分支 |
| 评审者成瓶颈 | 评估 `deferred` 增多 | 增加合格评审者；调 `maxParallel` |
| 协作返工过多 | 级联修复次数高 | 降 `maxRepairTasks`；收紧 delegate 门槛 |
| 评估误报 | 人工抽检误报 > 30% | `shadowOnly=true`；加强 evidence 要求 |
| 线程/goroutine 泄漏 | `AllRoutineStats.Inflight` 不归零 | 回滚到上一里程碑；查 scope 是否被复用 |
| 前缀缓存失效致成本上升 | 上游 token 花费上涨 | 检查候选目录是否稳定排序 |
| 数据迁移失败 | 启动报 `no such column` | 用备份 `data/cclaw.db` 回退；DDL 与迁移脚本一并回退 |
| 监控误杀 | `error_code=STALL` 占比异常 | 调大 `stallKillSec`；确认工具心跳豁免生效 |
| 决策反复 | 同一会话轮次间分诊结果抖动 | 固定 `routing_mode`；检查提示词中候选目录顺序 |

### 13.11 排期建议

#### 13.11.1 单人（约 5 周）

| 周 | 内容 | 周末应达到 |
|---|---|---|
| W1 | M0 + M1 + M2 | 重构完成且行为零变化；复杂度函数有单测 |
| W2 | M3 | dryRun 上线，决策可查 |
| W3 | M4 + M5（前半：执行器 + Worker） | 串行协作能跑出 3 个任务 |
| W4 | M5（后半：汇聚 + 触发 + 统一评估 + 回路） | 完整 `N+2` 链路跑通 |
| W5 | M6 + M7 | 并行、监控、中断、机检、修复全通 |
| W6 | M8 + 灰度准备 | 落库、事件、UI；L1 影子开始 |
| W7+ | M9 | 逐级灰度到全量 |

#### 13.11.2 两人并行（约 3 周）

| 人 | 负责 |
|---|---|
| A | M1 → M3 → M5 → M7（主链路 + 汇聚 + 评估） |
| B | M2 → 机检纯函数（T7.1–T7.3）→ M6（并发治理 + 监控）→ M8（落库/UI） |

交汇点：M1 完成后同时开工；**`claims` schema 在 T5.3 定稿后 B 才能写机检实现**。

### 13.12 第一周逐日清单（可直接照做）

| 日 | 上午 | 下午 | 产出 |
|---|---|---|---|
| D1 | D1–D3 决策；建分支；跑基线测试与冒烟 | 记录基线数据；预留三段配置 | 决策记录 + `M0-baseline.json` |
| D2 | T1.1/T1.2：`turn_runner.go` 契约与实现 | T1.3：`PrepareFor` | 可编译，未接入 |
| D3 | T1.4：`execute` 改薄 | T1.5：等价性对比（含中断场景） | M1 门禁通过，提 PR |
| D4 | T2.1–T2.3：信号结构、材料/阶段抽取 | T2.4：能力跨度 | `complexity.go` 初版 |
| D5 | T2.5：≥12 用例 + 基准测试 | 修 M2 门禁问题；预读 M3 提示词设计 | M2 门禁通过 |

### 13.13 灰度核对清单（逐项翻开关）

按顺序翻，每翻一项先确认上一项出口条件已满足：

| 序号 | 动作 | 核对点 |
|---|---|---|
| 1 | `[routing] enabled` 保持 `false`，先部署 | 日志无配置解析错误；行为与基线一致 |
| 2 | `enabled=true` + `dryRun=true` | `t_route_decision` 开始有数据；**执行仍是单 Agent** |
| 3 | `dryRun=false` + `allowDelegate=false` | 协作率 = 0；p95 延迟无回归 |
| 4 | `allowDelegate=true` + `[evaluate] shadowOnly=true` | `N+2` 调用计数正确；答复仍以 Worker 汇总为准 |
| 5 | `shadowOnly=false` | 评估的 blocker 真正影响答复；缺口说明出现 |

配置项全貌见 §11；**`allowDelegate` 必须是配置项而非编译期常量**——L2→L3 只改配置重启，回滚也只改一行。

---

## 14. 测试矩阵

### 14.1 分诊

| # | 输入 | 期望 |
|---|---|---|
| 1 | `13 * 17 = ?` | single |
| 2 | 3000 字合同"总结 5 条" | single（长≠复杂） |
| 3 | "先查 A、B 两个债务人诉讼状态，再按回收率排序出表" | delegate（短≠简单） |
| 4 | "把 A.pdf、B.pdf、C.pdf 的科目提出来对比" | delegate（S2） |
| 5 | "尽调 + 估值 + 法律风险，出完整处置方案" | delegate（S1 跨 2 Agent） |
| 6 | 单位内只有 1 个 Agent | single（`no_verifier`） |
| 7 | 预算不足以覆盖 `N+2` | single（`no_budget`） |
| 8 | 分诊返回非法 JSON | 回落到今日分类 |
| 9 | 计划的生产者覆盖了全部 Agent | single（`no_verifier`） |

### 14.2 计划

| # | 场景 | 期望 |
|---|---|---|
| 10 | 环 / 未知 Agent / 指代任务 | 剔除；下游连带剔除 |
| 11 | 有效任务 <2 | 降级 single |
| 12 | 计划里出现 `verify` 字段或指定 reviewer | **整份拒绝** → 回填或降级 |
| 13 | linter 失败 2 次 | 降级 single 并附 note |

### 14.3 执行

| # | 场景 | 期望 |
|---|---|---|
| 14 | 全并行 3 任务 | wave0 三个 produce 并发；随后 1 次评估 |
| 15 | 钻石依赖（t1,t2→t3） | 2 个 produce 波次 + 1 次评估（共 3 轮 LLM 波次） |
| 16 | 中间任务失败 | 下游 `skipped`；评估把缺口写进 `gaps` |
| 17 | 全部失败 | default Agent 单跑 |
| 18 | 用户中途中断 | 全树 `canceled`；已得结果留痕 |
| 19 | `enabled=false` | 与今天逐字节一致 |
| 20 | 调用计数 | 恰为 `N+2`（无修复时） |

### 14.4 评估

| # | 场景 | 期望 |
|---|---|---|
| 21 | 任意协作轮 | **恰好 1 次**评估调用；`tasks[]` 覆盖全部任务 |
| 22 | 评估遗漏某个任务 | 视为无效评估 → 重试 1 次；仍缺 → 该任务 `unverifiable` |
| 23 | 指派到某个 Worker | 断言失败 → 降级 single（绝不静默放行） |
| 24 | 评审者与生产者同 vendor | `moderate` 并落库 |
| 25 | 评审者用 `qwen3.8-flash` | `strong` |
| 26 | 引文编造 | L-A blocker → 就地修复 |
| 27 | 派生值重算不一致 | L-A blocker |
| 28 | 口径不符（母公司 vs 合并） | L-C blocker → 级联修复 |
| 29 | t1/t2 口径不一致（各自都对） | `cross_checks` 命中 |
| 30 | 材料缺该科目 | `unverifiable`（非 fail）→ 写入 `gaps` |
| 31 | 同一 blocker 修复后仍在 | 立即停（issue 重复）→ degrade |
| 32 | 评审者并发满/超时 | 重试 1 次 → 降级"拼接摘要"并声明"未经评估" |
| 33 | 答复出现已核验 claims 之外的数字 | synthesis 机检 → 重写一次 |
| 34 | 级联修复超过 `maxRepairTasks` | 只修 blocker 任务，下游标 `tainted` 并在答复声明 |

### 14.5 监控（协调者第 ③ 项职责）

| # | 场景 | 期望 |
|---|---|---|
| 35 | Worker 60s 无任何事件（LLM 静默） | 记 `slow` + warn 日志；**不取消** |
| 36 | Worker 180s 无事件 | 取消该任务 → `failed(STALL)` |
| 37 | 工具正在执行、每 3s 有心跳 | **不误杀**（有心跳即活性） |
| 38 | 超过 `taskTimeoutSec` | 取消 → `failed(TIMEOUT)`；下游 `skipped` |
| 39 | 预算耗尽 | 剩余任务 `skipped(BUDGET)`；**评估预算仍保留** |

### 14.6 汇聚（协调者第 ④ 项职责）

| # | 场景 | 期望 |
|---|---|---|
| 40 | 上游失败 / 跳过 | `Gaps` 在调用评估前就包含对应条目（不依赖模型发现） |
| 41 | 输入超 `inputMaxRunes` | 按比例缩短 summary（不低于 300），Action/quote 优先保留 |
| 42 | 同一批结果重复汇聚两次 | 序列化结果**字节一致**（顺序稳定、可复现） |

### 14.7 评估触发（汇合点）

| # | 场景 | 期望 |
|---|---|---|
| 43 | 3 任务先后完成 | **只在全部终态后**触发 1 次评估；中途不触发 |
| 44 | 某任务 `deferred`（闸位满） | 不触发；重排该任务后仍不触发，直到它终态 |
| 45 | `deferred` 连续 2 次 | 转 `failed(CAPACITY)`，随后触发评估，缺口写进 `gaps` |
| 46 | 全部任务失败 | **不触发**评估；直接降级 default Agent 单跑 |
| 47 | 用户中断 | **不触发**评估；`t_turn_task` 无 `kind=1` 行 |
| 48 | 评估后级联修复 | 恰好再触发 1 次重评估；不再级联则停止 |

---

## 15. 风险与对策

| 风险 | 表现 | 对策 |
|---|---|---|
| 模型过度规划 | 简单问题也拆 3 个任务 | 提示词硬规则 + 有效任务 <2 ⇒ single + `confidence` 门槛 + 监控协作率 |
| 模型欠规划 | 复杂问题判成 simple | L1 强信号硬提升；`keywordRuleMode=hint`；UI"用协作重答" |
| 协作变贵 | `N+2` 次调用 | 收紧 `delegate` + 预算可行性前置 |
| 评估形同虚设 | 评审者顺从/共谋 | 评审者不得是任何 Worker；机检先行；强制逐任务裁决；`unverifiable` 合法 |
| 评估无评审者可派 | 2 个 Agent 时生产侧不跨 Agent | 明示约束；建议新增专职评审者（`verify_agent_id`） |
| 评估晚发现 | 下游已消费未校验数据 | 任务书标注 `[provisional]`；评估后级联修复 + 上限；答复必须声明缺口 |
| 单次评估上下文过长 | N 大时输入爆窗 | `inputMaxRunes` + 按比例缩短 summary；N>maxTasks 时截尾或两级 reduce |
| 评估者成瓶颈 | 只有 1 个评审者 | 评估并发 key 独立（`#eval`，limit=1）；至少 2 个合格评审者 |
| 假"已评估" | weak/self 混入 | 等级写进产物；单 Agent 单位不协作 |
| 修复死循环 | 反复失败 | 轮次 + issue 重复即停 + `maxRepairTasks` + 预算 |
| 监控误杀 | 模型长思考被当成卡死 | 分级阈值（warn 60s / kill 180s）；工具运行中不适用该阈值（有心跳即活性）；阈值可配 |
| 监控漏判 | Worker 真挂死但一直有心跳 | 硬兜底：`taskTimeoutSec` 强制取消，不依赖事件 |
| 汇聚不可复现 | 同一结果两次拼出的输入不同 | 按计划 `seq` 稳定排序；纯函数 + 字节一致单测 |
| 并发自锁 | root 与 Worker 抢同一闸 | 子闸 key 加 `#task`；闸位超时转 `deferred` |
| 计划成环 | 任务永远不 ready | Kahn 检测前置 |
| 历史污染 | Worker 写进父历史 | `Recordable=false` + 不调 journal |
| 多轮退化 | 绑定短路让协作只在首轮 | `routing_mode=1` 时每轮重新分诊 |
| 前缀缓存失效 | 候选目录顺序不稳 | 按 (priority, agentId) 稳定排序 |

---

## 16. 决策与待确认

### 16.1 阻塞项（M0/T0.1 必须填写结论）

| # | 问题 | 建议默认 | 结论 | 决策人 / 日期 |
|---|---|---|---|---|
| **D1** | **是否新增专职评审 Agent**：当前单位只有 2 个 Agent（`agent_001/002`，同 vendor），不新增则生产侧无法跨 Agent | 新增一条 `t_agent`（"复核助手"，`primary_model=qwen3.8-flash`）并用 `verify_agent_id` 指定，同时拿到 `strong` 独立性与跨 Agent 生产 | 待定 | — |
| **D2** | **评估与答复是否合一**：本文按 `N+2` 把"评估 + 汇总"做成同一次调用 | 合一；若改为"评审者只评审、另由某 Agent 撰稿"，成本变 `N+3` | 待定 | — |
| **D3** | **`blockOnBlocker` 默认值**：blocker 未解决时 | `true`（降级标注后仍输出） | 待定 | — |

> 三项不拍板即开工，返工量约 3 人日（§13.2.1）。

### 16.2 非阻塞项（可在灰度期定）

| # | 问题 | 当前取值 | 说明 |
|---|---|---|---|
| 4 | `maxRepairTasks` 默认 2 是否合适 | 2 | 决定级别返工上限；灰度期按"协作返工过多"信号调整 |
| 5 | 写操作类任务的时机 | 直接写 | 是否需要"暂存目录 + 评估通过后提交"；会改动现有工具写路径，工作量较大，建议后续版本 |
| 6 | `routing_mode` 是否接受 | 接受 | 唯一"改会话语义"之处，且只影响被判为复杂的会话 |
| 7 | `maxTasks` 默认 5 | 5 | 与 `inputMaxRunes` 联动；N 更大时需两级 reduce（§16.3） |
| 8 | 评审者是否允许与某 Worker 同 Agent 但不同 vendor | 允许（`moderate`） | 已落库标注等级，不阻断 |

### 16.3 已知边界（不作为待确认，仅备忘）

- `N > maxTasks` 时需"两级 reduce"（每 5 个任务先合并一次），本文未展开，属后续版本；
- Worker 若变成异步/远程执行，汇合方式从"内存 map"改为"DB 查询"（§6.6 末），调用方不变；
- `weak`/`self` 独立性等级在本方案中**不可作为结论**，因此单 Agent 单位永不协作。

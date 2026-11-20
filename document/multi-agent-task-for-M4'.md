# M4′ 任务清单 —— 撰写 + 落盘 + 机检 + 评估 + 修复

> 配套文档：`document/multi-agent协作实现方案.md`（§8.5–§8.10、§9、§13、§14、§15）
> 编写时点：M3′ / M3′.5 已落地，M4′ 未开始
> 代码基线：`src/cclawservice`（`chat/sop`、`biz/npa`、`service`）

---

## 1. M4′ 定位

文档 §14 原文：

> **M4′** | 撰写 + 落盘 + 机检 + 评估 + 修复 | #8–#10、#11（llm/check 步） | 尽调报告端到端；8 项机检 + rubric 生效；修复 ≤2 轮

**一句话**：把执行器从"只能跑确定性工具步"升级为"能生成内容 + 能自己判卷 + 能按判卷结果定向返工"。

M4′ 的核心不是"让 LLM 能被调用"，而是**建立交付前的质量闭环**：

| # | 能力 | 回答的问题 |
|---|---|---|
| 1 | 章节化内容生产 | 报告正文由谁来写？ |
| 2 | 双判卷（机检 + 评估） | 这份报告能不能交付？ |
| 3 | 定向返工 + 收敛判定 | 不合格怎么办、什么时候停？ |
| 4 | 返工后回边（重新落盘） | 磁盘上的报告是最终版吗？ |

---

## 2. 起点盘点：M4′ 的缺口

### 2.1 计划份量：M4′ 占 9/14

`npa-report` 展开后 14 个任务 / 12 波次（`TestNPAReportSkillSOPIsBuildable` 实测 `tasks=14 waves=12 est_tokens=48768`）：

| kind | 任务 | 数量 | 状态 |
|---|---|---|---|
| tool | `ingest` / `ingest_ocr` / `extract` / `save` | 4 | ✅ M3′ 已实现（真跑） |
| merge | `compose`（章节拼接，builder 生成） | 1 | ✅ M3′ 已实现 |
| **llm** | `compose#basic_info` … `compose#conclusion`（7）+ `evaluate` | **8** | ❌ **T5 `StatusSkipped`** |
| **check** | `verify`（8 项机检） | **1** | ❌ **T5 `StatusSkipped`** |

> 结论：工具步做得再真，产出的也只是 `parsed_context.json` / `extracted_metrics.json`。用户要的**报告**与判断报告能否交付的**判卷**，全部落在 M4′。

### 2.2 代码缺口清单

| # | 现状（代码事实） | 缺口 | 严重度 |
|---|---|---|---|
| 1 | `chat/sop/sop_chatbot.go` 的 `runTask`：`case PlanTaskCheck, PlanTaskLLM` → 直接 `StatusSkipped` | 两类节点无执行体 | **P0** |
| 2 | `bin/cclaw/skills/npa-report/SKILL.md` 的 `ingest.args` 只有 `{recursive, emit_stdout}`，缺 `path`；而 `npa_ingest` schema `required: [path]`，`harness/toolexecutor/tool_spec.go:101` 会拦 | 第一步即 `ToolArgInvalidKind: missing required field "path"` → 全链失败 | **P0** |
| 3 | `biz/npa/npa_artifact.go:31` `ArtifactExtractMetrics = "extract_metrics"`，而 SKILL.md / 计划为 `extracted_metrics` | `missingNeeds` 按产物名查 `ArtifactMgr` → `compose` / `verify` 恒 `blocked` | **P0** |
| 4 | `harness_common.go` 的 `SkillSOPStep` 已解析 `on_fail` / `max_rounds` / `policy`，但 `sop_common.go` 的 `PlanTask` 只有 `MaxTries` | 修复回路所需契约在展开期丢失 | P1 |
| 5 | `biz/npa/npa_checker.go` 只有 3 个门禁器（`materials_manifest`/`metrics_reconciled`/`save_ok`） | §9.1 的 8 项机检缺 6+2 项 | P1 |
| 6 | `sop.TaskBarrierInput` = `{Task, Output(单产物), Lookup, Spec}` | 机检需"全文 + 多上游产物"，现有形状承载不了 | P1 |
| 7 | `sop` 只有 `IToolInvoker`，无 `ILLMInvoker`；且 `sop` 不能依赖 `chat` | LLM 步无调用通道 | P1 |
| 8 | 全仓 `pickEvaluator` 仅存在于注释（`chat/turn_common.go:28`） | 评估者无法选出 | P1 |
| 9 | `TurnRequest` 有 `AllowRecord`/`DisableTool`/`ParentTurnID`，计划执行器尚未使用 | 7 个章节子轮会污染主会话历史 | P1 |
| 10 | `PlanState.Round` 字段存在，无任何递增者 | 修复回路无轮次计数，`t_plan_gap.round` 恒 0 | P2 |
| 11 | `sop.GetSOPChatResultSummary` 只统计成功/失败任务数 | §8.10 需要 `evaluate.answer` 作为正文 | P2 |
| 12 | 台账证据：`t_plan`/`t_plan_task`/`t_artifact` 均 0 行，日志无 `npa_ingest`，无 `IToolInvoker` 测试 | 工具步"已实现但从未真跑通" | — |

### 2.3 与 M3′ 门禁的本质差异

M3′ 已有 per-task 门禁，M4′ 的判卷**不是同一个物种**：

| 维度 | M3′ 门禁（已有） | M4′ 判卷（核心） |
|---|---|---|
| 粒度 | 单任务、单产物 | 全篇报告 + 多个上游产物 |
| 输入形状 | `Artifact` + `Lookup` | `{ReportText, Draft, Metrics, Parsed, Summary}` |
| 判据 | 数值 / 布尔 | 语义与引用（身份证号是否存在、`(文件,页)` 是否真实） |
| 判卷人 | Go 纯函数 | 机检 = Go；评估 = **独立 Agent** |
| 失败后果 | 本步 failed、下游 blocked | **回到生产步重写** |
| 成本 | 0 | 每轮一次 LLM + 可能的 N 次重写 |

**最关键的一格是"判卷人"**：`pickEvaluator` 要求"评审 Agent ∉ 生产者集合"。这是整个方案里唯一真正体现 multi-agent 的地方——工具步、merge 步、机检本质都是单进程确定性流程。只有当"写报告"和"判报告"必须是两个 Agent 时，"协作"才有实质含义（§15 用例 6：宁可降级单 Agent 直答，也不允许自评）。

---

## 3. 任务清单总览

```mermaid
graph LR
  T1[T1 契约闭合 P0] --> T2[T2 PlanTask 契约补全]
  T1 --> T5
  T2 --> T3[T3 ILLMInvoker 通道]
  T3 --> T4[T4 章节任务书 / 评估提示词]
  T4 --> T5[T5 check 步执行]
  T5 --> T6[T6 机检 8 项]
  T6 --> T7[T7 pickEvaluator + 评估步]
  T7 --> T8[T8 修复回路 + 回边]
  T8 --> T9[T9 汇总答复 + 答复机检]
  T8 --> T10[T10 落库 / resume 语义]
  T9 --> T10
```

| 任务 | 目标 | 层级 | 规模 |
|---|---|---|---|
| **T1** | 契约闭合（产物名 + 必填参数）→ 新增构建期校验 | 地基 | 小 |
| **T2** | `PlanTask` 承接 `on_fail`/`max_rounds`/`policy` | 地基 | 小 |
| **T3** | `ILLMInvoker` 通道 + service 适配器 | 地基 | 中 |
| **T4** | 章节任务书 / 评估提示词模板 | 核心 | 中 |
| **T5** | `check` 步执行，产出 `verify_report` | 核心 | 中 |
| **T6** | §9.1 八项机检纯函数 | 核心 | 中 |
| **T7** | `pickEvaluator` + 评估步 | 核心 | 中 |
| **T8** | 修复回路 + 返工回边 | 核心 | **大** |
| **T9** | 汇总答复 + 答复机检 | 收口 | 小 |
| **T10** | llm 步落库/resume 语义 + 端到端验收 | 收口 | 中 |

> 判断标准：**T4–T8 任一缺失，M4′ 验收门禁（"8 项机检 + rubric 生效；修复 ≤2 轮"）不可能通过**；T3 缺失只是"跑不起来"。
> 先做 T1–T7 可得到"完整报告 + 机检结论"（不含返工）的**最小可用切片**；T8 是把"能出报告"变成"出合格报告"的增量，可独立验收。

---

## 4. 逐任务说明

### T1 契约闭合（P0，必须最先做）

**目标**：把"运行期 blocked / 参数缺失"提前成"构建期报错"。

**改动**
- `biz/npa/npa_artifact.go`：`ArtifactExtractMetrics = "extract_metrics"` → `"extracted_metrics"`；
- `bin/cclaw/skills/npa-report/SKILL.md`：`ingest.args` 增 `path: "${spec.project_dir}"`（对照 `ingest_ocr` 已有 `path: "${parsed_context.ocr_dirs}"`，说明写法正确，只是漏了）；
- `chat/sop/sop_outline_validator.go`：新增两条校验规则
  - **L9 产物名闭合**：`Needs` / `${name.field}` 中的每个 name 必须能追溯到某个 `Produces`；且**注册的 extractor 产物名必须等于 `step.Produces`**；
  - **L10 必填参数**：`kind=tool` 的 `Args` 必须覆盖该工具 schema 的 `required` 字段（需把 `map[toolName][]required` 传进校验器）。

**要点**
- 产物名是计划里唯一的跨步契约，来源有两个：SKILL.md（声明）与 extractor（实现）。不一致时 `ArtifactMgr` 只能"查不到 → blocked"，无法说明原因；
- 校验器需要 `KnownTools` 升级为 `KnownToolSpec{Required []string}`，由 `Preparer` 从工具 bundle 提取后传入；
- `spec.Fields()` 已提供 `project_dir`/`archive_no`/`report_kind`/`today`/`ocr`，`${spec.project_dir}` 可直接渲染。

**设计原理**：构建期是 0 成本的，运行期 blocked 要等用户真实跑一轮才发现。凡是"声明与实现可能不一致"的地方，都应该在构建期判定——这与 M3′.5 把"LLM 提案"交给确定性校验器否决是同一个思路。

**验收**
- 故意把 extractor 产物名改错 → 构建报 L9，日志给规则号；
- 故意删掉 `ingest.args.path` → 构建报 L10；
- `TestNPAReportSkillSOPIsBuildable` 仍为 `tasks=14 waves=12`；
- 冒烟后 `t_plan_task` 出现 `ingest` 为 `succeeded`。

---

### T2 `PlanTask` 契约补全

**目标**：让"修复策略"跟着计划快照走，而不是只活在 SKILL.md 里。

**改动**：`chat/sop/sop_common.go`、`chat/sop/sop_plan.go`

**要点**
- `PlanTask` 增加 `OnFail string`（`repair`/`abort`）、`MaxRounds int`（默认 2）、`Role string`（LLM 步角色，与 `Action` 分离）；
- 展开期从 `SkillSOPStep` 透传；`merge` 任务继承父步的 `OnFail`/`MaxRounds`；
- `Policy` 增加语义映射：`no_tools → DisableTool=true`、`read_only → 允许工具但受 ReadRoots 约束`、空 → 默认只读；
- 新增字段全部 `omitempty`，`planSnapshotVersion` **不必**升版（旧快照读出零值再套默认）。

**设计原理**：`Plan` 快照必须能脱离 SKILL.md 独立重放（§8.1.5 resume 靠 digest 比对）。若 `on_fail`/`max_rounds` 只存在于 YAML，SKILL.md 一改就会导致"同一 turn 前后轮次上限不同"的不可解释状态。

**验收**
- `sop_plan_test.go` 断言 `compose#risk` 的 `MaxRounds==2`、`evaluate` 的 `OnFail=="repair"`；
- 快照 round-trip 后字段不丢；旧快照（无新字段）仍可反序列化。

---

### T3 `ILLMInvoker` 通道（M4′ 骨架）

**目标**：让执行器能跑 `kind=llm` 节点，且执行器不认识 `TurnState`。

**改动**
- `chat/sop/sop_common.go`：新增接口与 DTO，`PlanRequest` 增 `LLM ILLMInvoker`；
- `chat/sop/sop_chatbot.go`：`PlanTaskLLM` 分支调 `LLM.Invoke`；`LLM == nil` 时保持 `StatusSkipped`（护住 `mode=off` 与测试）；
- `service/sop_llm_invoker.go`（新）：适配器实现。

```go
type ILLMInvoker interface {
    Invoke(ctx context.Context, req LLMTaskRequest) (LLMTaskResult, error)
}

type LLMTaskRequest struct {
    TaskID, Section, Role, Policy string
    Prompt                        string
    Needs                         []string
    MaxRounds, Round              int
}

type LLMTaskResult struct {
    Text      string
    Rounds    int
    ToolCalls int
    Tokens    int64
}
```

**要点**
- 适配器内部：`Preparer.PrepareFor(ctx, subCmd, agent, TurnRoleGenerator)` → `TurnRunner.Run(TurnRequest{...})` → 取 `TurnOutcome.TurnReply.Content`；
- 子轮三条硬约束：
  - `subCmd.TurnID = turnID + "#" + task.ID`（日志/落库唯一）；
  - **`AllowRecord = false`**（禁止把章节草稿写进主会话历史）；
  - `DisableTool = (policy=="no_tools")`；子轮 sink 用 `NopSink`（只取文本，不混流）。
- 依赖方向保持 `service → chat/sop`，`sop` 不 import `chat`。

**设计原理**：与工具步同一套"能力注入"模式，执行器只认识 `Invoke`。差异收敛到"参数 + prompt"两个纯函数里，`mark/gap/timeout` 逻辑只写一份。`AllowRecord=false` 不是优化而是正确性要求：草稿写进历史后，下一轮上下文会同时出现 7 份半成品，压缩策略会把它当真实对话，污染后续所有轮次。

**验收**
- `fakeLLMInvoker` 驱动下 `compose#basic_info` → `draft#basic_info` 产物存在、任务 `succeeded`；
- 主会话历史行数不因 7 个章节子轮而变化；
- `LLM == nil` 时行为与 M3′ 完全一致（llm/check 仍 `skipped`）。

---

### T4 章节任务书 + 评估提示词（#9）

**目标**：把"一次生成什么"固化成可测的纯函数。

**改动**：`chat/sop/compose_prompt.go`、`chat/sop/evaluate_prompt.go`（新增）

**要点**
- `BuildSectionPrompt(task, evidence)` 输入只有：章节名、该节契约（§8.5 表格）、**证据摘要**、`needs` 产物字段摘要、（可选）上一节结尾；
- **证据摘要必须压缩**：`parsed_context` 可能数十 MB，只能取 `extracted_metrics` 的 `personal_info_roster` / `documents[].persons` / `documents[].metrics` 摘要 + "可按需回读的文件清单"，严禁整篇塞入；
- 提示词强约束：只输出本节 Markdown、不写总结/最终答复、数字必须带 `(文件, 页)`、材料没有的写"未见/待补充"；
- `BuildEvaluatePrompt(rubric, draft, verifyFields)`：rubric 固定表 + 严格 JSON schema（`verdict/scores/gaps/answer`）。

**设计原理**：章节边界既是生成边界，也是**修复边界**——"只重写受影响章节"能成立，前提是每节独立调用、产物名 `draft#<section>` 独立。

**验收**：prompt 含章节契约与 `forbid_invent`；不含超长正文；evaluate schema 字段与解析器字段一一对应。

---

### T5 `check` 步执行（#11 的 check 分支）

**目标**：机检结果变成可被后续 LLM 步引用的**产物**。

**改动**：`chat/sop/sop_chatbot.go`（`runCheckTask`）、`biz/npa_binding.go`

**要点**
- 按 `PlanTask.Checks` 逐个调用注册的机检函数；**`Checks` 为空 → 直接 fail**（配置错误不可静默跳过）；
- 结果封装成产物 `Artifact{Name: task.Produces("verify_report"), ArtifactKind:"json", Fields:{...}}`，`Fields` 放扁平常量：`verify.pass` / `verify.fail_count` / `verify.fail_codes` / `verify.detail`；
- 每个 fail/warn 同时转 `Gap`（带 `Code/TaskID/Section/Severity`）并 `SaveGaps(round)`，`Fixed=false`；
- 不同机检项的实现通过新的注册表 `MachineCheckRepository`（按 check 名索引），与现有 `BarrierRepository`（按 gate 名）并列。

**设计原理**：`evaluate` 的 `needs: [draft, verify_report]`，必须能用 `${verify_report.fail_codes}` 渲染——把检查结果做成产物而非只写 gap，才能与工具步完全同构。"`Checks` 空即 fail"是因为"声明了校验却没实现"和"校验通过"语义天差地别。

**验收**：缺章节的 draft → `verify` 任务 `failed`、`verify_report.fail_codes` 含 `section_complete`、`t_plan_gap` 有对应行。

---

### T6 机检八项纯函数（#8）

**目标**：0 LLM 的确定性质量闸门。

**改动**：`biz/npa/npa_machine_check.go`（新增）、`biz/npa_binding.go`（注册）

**要点**
- 新输入结构（不复用 `TaskBarrierInput`）：

```go
type MachineCheckInput struct {
    ReportText string
    Draft      Artifact
    Metrics    Artifact
    Parsed     Artifact
    Spec       ReportSpec
    Summary    map[string]string // 取自同一轮 summary
}
type MachineCheck func(in MachineCheckInput) []Finding
```

- 逐项判据（§9.1）：
  - `no_invention`：`KeyTokenPattern`（身份证/合同号/不动产证号/金额）扫全文 → 逐 token 到 `parsed_context`/`extracted_metrics` 已抽取文本做归一化子串匹配；**先设扫描上限**（token 数、单文件字符数），超限降级 `warn` 并注明"未全量核验"；级别 **fail**；
  - `numbers_traceable`：每个数字带可定位引用 `(文件, 页)`；fail；
  - `derived_recompute`：分子分母任一缺失 → `warn`（防误杀）；齐全则 `float64` 重算，相对误差 ≤1%；fail；
  - `section_complete`：7 节齐全、非空、每节 ≥50 字；fail；
  - `citations_exist`：`(文件, 页)` 的文件在本轮 manifest 内、页码 ≤ 页数；fail；
  - `materials_consistent`：附录数字等于**同一轮** `summary`；fail；
  - `person_coverage`：债务人 + 每名保证人逐人出现；**warn**；
  - `no_fake_execution`：`files_need_ocr` 非空却称"已 OCR 全部"；fail。

**设计原理**：机检是质量闸门的唯一可信源，必须 0 LLM——若用 LLM 判 LLM，判卷人与被卷人同源，`accuracy` 必然虚高。`fail`/`warn` 分级对应两种处理：`fail` 参与返工，`warn` 只进报告标注。刻意让"缺证据"走 `warn`，因为误判成本不对称：误 `fail` 会触发整节重写（花钱 + 可能越改越差）。

**验收**：§15 用例 4（编造身份证号 → `no_invention` fail）、用例 5（派生值偏 5% → `derived_recompute` fail）各一条单测；每项至少一个正/反用例。

---

### T7 `pickEvaluator` + 评估步（#10）

**目标**：保证"判卷人不是生产者"。

**改动**：`chat/sop/evaluator_assign.go`（新增）、`chat/sop/sop_chatbot.go`

**要点**
- `pickEvaluator(candidates []dataobject.TAgent, producers []string, skillID string) (TAgent, error)`：
  1. **硬排除**所有生产者（`agentID ∈ producers`）——不可协商；
  2. 域匹配：`SkillIds` 覆盖该 skill 者优先；
  3. 独立性：与生产者不同 `AgentId`；
  4. 全部被排除 → `ErrNoIndepEvaluator` → 按 §15 用例 6 **回落单 Agent 直答**（不降档、不自评）。
- evaluate 步：`DisableTool = true`（评审者不许自己动手改）、`Role = TurnRoleEvaluator`；
- 解析严格：维度缺失 / 越界（不在 0..2）/ `verdict` 非法 → 视为解析失败，回灌 parse error 重试 **1 次**，再失败 → `gap{evaluate_unparsable, fail}` + 降级；
- **`verdict` 由 Go 复算**（加权 ≥1.6 且无 0 分项且机检无 fail），LLM 只提供 0/1/2 分数。

**设计原理**：评审独立性是正确性约束，放代码不放 prompt。`verdict` 由 Go 算：LLM 对加权求和极不稳定，而 rubric 是固定表——让 LLM 只做"打分"这件可校验的事，算术留给代码。`DisableTool` 既省钱也防越权（防止绕过修复回路直接覆盖正式报告）。

**验收**：`pickEvaluator` 三分支单测（正常 / 全被排除 / 无域匹配）；非法 JSON 只重试 1 次并落 `evaluate_unparsable`。

---

### T8 修复回路 + 返工回边（最重）

**目标**：不合格时定向重写、可控收敛、并保证磁盘上是最终版。

**改动**：`chat/sop/sop_chatbot.go`、`chat/sop/sop_plan.go`（重写组）

**要点**
1. 合并"机检 fail + 评审 fail"，**按 `Section` 分组**；无 `Section` 的全局 gap（如 `materials_consistent`）→ 直接终止并降级标注（返工也修不出来）；
2. 对每个受影响章节重新执行 `compose#<section>`，prompt 只给"该节 + 相关 gap + 可用证据"，**不重给全篇**；
3. **回边必须显式重排**（当前 outline 缺这条边）：

```
compose#<受影响节> → compose(merge) → save → verify → evaluate
```

  原因：`save` 与 `verify` 同为 Depth 4（`save` 依赖 `draft`；`verify` 依赖 `draft`+`extracted_metrics`+`parsed_context`），首轮是**并行**执行的；返工后若不重跑 `save`，磁盘上的报告仍是旧版；
4. 终止三条件任一满足即停：`pass` / `Round >= MaxRounds` / **连续两轮 gap 集合无变化**；
5. 用尽仍 fail → `blockOnBlocker` 默认 true → **降级输出**：正文照发 + 顶部 `⚠️ 未通过校验：<n> 项`（不整轮失败）；
6. 每轮 `PlanState.Round++`；`SaveGaps(round=Round)`；已修复 gap 追加标记 `Fixed=true`（追加不覆盖）。

**设计原理**
- **按节返工**：返工成本 ∝ 章节长度，全篇重写会让"改一个数字"付出"重写 7 节"的代价，且更易引入新错；
- **全量重跑机检**：`no_invention`/一致性是跨节性质，只跑受影响节会漏掉"改 A 导致与 B 矛盾"；
- **"无进展即停"是最重要的成本保护**：LLM 返工常在同一个错误上震荡，两轮 gap 集合不变说明再给几轮也一样；
- **降级而非失败**：用户要的是"一份标了风险说明的报告"，不是"一个完美的失败请求"。

**验收**
- §15 用例 4/5：只重写 1 节；
- 人为构造震荡（两轮 gap 相同）→ 第 2 轮后停止，总轮次 ≤2；
- **返工后 `report_path` 的 mtime 晚于最后一轮 `draft` 产物时间**；
- `t_plan_gap.round` 递增且 `fixed` 标记正确。

---

### T9 汇总答复 + 答复机检（#11 尾步 / §8.10）

**目标**：用户最终看到的内容与真实执行严格一致。

**改动**：`chat/sop/sop_utility.go`、`biz/npa`

**要点**
- 正文取 `evaluate.answer`；无 answer → `GetSOPChatResultSummary` 兜底（保持 M3′ 不回归）；
- 附三段固定信息：报告路径（`report_path.path`）、材料对账（文件数/总页数/解析目录，**全部取自同一轮 `summary`**）、校验结论（fail 列表）；
- 对最终答复再跑一次 `no_fake_execution`（声称"已完成 OCR / 已保存"必须与实际产物一致）；
- `sink.OnDelta(answer)` **只发一次**，避免与章节子轮混流。

**设计原理**："同轮 summary"是防"抄历史报告数字"（真实事故：材料 12 份、报告写 9 份且无人察觉）。答复机检放最后，因为这是唯一"用户直接看到"的文本，也是唯一可能"声称未执行步骤"的地方。

**验收**：`files_need_ocr` 非空却出现"已 OCR 全部" → `no_fake_execution` fail 并改写措辞。

---

### T10 落库 / resume 语义 + 端到端验收

**目标**：llm 步可续跑、可留痕、可对账。

**改动**：`service/core/orchestrator_storage.go`、测试

**要点**
- `SaveTask` 对 llm 步记 `output_name = draft#<section>`、`input_digest`（章节 prompt 的 hash）、`tries` = 实际轮次；
- resume：`succeeded` 的 llm 步**直接跳过**（重跑 = 重新花钱）；`failed/blocked` 重跑；`PlanState.Round` 与 `t_plan_gap.round` 必须一起恢复，否则修复回路从 0 重来；
- `mode=off` 行为与现状逐字节一致（延续 M2′/M3′.5 门禁）；
- 端到端跑 §15 用例 1–6、9。

**设计原理**：工具步可靠 `input_digest` 幂等；**llm 步没有幂等性**——同样的 prompt 会得到不同文本、花不同的钱。所以续跑语义只能建立在落库状态上，不能"算出来"。这也解释了 §8.1.5 把 `plan_json` digest 与 resume 绑定的原因：状态与计划必须同一时点。

**验收**：跑到 `compose#contracts` 后杀进程 → 重启续跑 → 已成功章节 0 次 LLM 调用；`t_plan_gap` 轮次连续。

---

## 5. 横切约束（每个任务都适用）

| 约束 | 具体要求 | 原因 |
|---|---|---|
| 禁裸 `go` | 一律走 `concurrency.GetTaskManager().NewScope(...).Go(ctx, RoutineSpec{...})` | `archtest` 的 `TestNoBareGoStatement` |
| 并发闸门 | 章节并发受 `maxRoutines`(现 8) 与 agent `MaxConcurrents` 双重限制；**LLM 步建议下调至 3–4** | 长事务 8 路并发易撞上游限流，重试成本高 |
| 超时 | 波次 `tasksWait`(现 180s) 对含 LLM 步的波次明显不足，需按波内任务类型计算；单子轮另有 `ChatRunTimeout` | 写 7 节报告远超 180s，否则必然"波次未收敛 → 后续波次全停" |
| 成本可观测 | 每个 llm 步记录 `Rounds/ToolCalls/Tokens`，汇总进 `PlanResult` 与日志 | §8.1.6 成本表需在 M4′ 后可对账 |
| 失败不静默 | `Save*` 失败仍只写日志（M3′.5 现状），但 `CheckSchema` 启动期必须拦一次 | 已踩过"落库静默失败" |
| `mode=off` 不变 | 所有新分支必须能被 `PlanRequest.LLM == nil` 短路回 `skipped` | M2′/M3′.5 门禁仍在生效 |
| 单一依赖方向 | `sop` 不 import `chat`；适配器只放 `service`/`chat` 侧 | 避免 package 循环 |

---

## 6. 落地顺序

| 阶段 | 任务 | 产出 | 可验收 |
|---|---|---|---|
| P0（半天） | T1 | 计划可真正跑起来（不再首步失败、不再全 blocked） | `t_plan_task` 出现 succeeded 的 tool 行 |
| P1 | T2 + T3 | 7 章节 + merge 可跑（fakeLLM） | 单测：`draft#*` 产物齐全 |
| P2 | T5 + T6 | 机检可跑，`verify_report` 产物可用 | 机检 8 项单测全绿 |
| P3 | T4 + T7 | 真实生成 + 独立评审 | 一份可读报告 + `verdict` |
| P4 | T8 | 定向返工 + 回边 + 收敛 | 用例 4/5，修复 ≤2 轮 |
| P5 | T9 + T10 | 答复一致 + 可续跑 | 用例 1–6、9 + 杀进程续跑 |

> **最小可用切片 = T1–T7**（完整报告 + 机检结论，不含返工）。

---

## 7. 验收口径（映射 §15）

| 用例 | 期望 | 覆盖任务 |
|---|---|---|
| 1 材料齐全 | 摄+提 1 波完成；7 章节齐全；机检 0 fail | T1,T3,T4,T6 |
| 2 含扫描件 | 自动插 `ingest_ocr`；补读后 `materials_check.match=true` | M3′ + T1 |
| 3 份数对不上 | 不进入撰写；gap 落库 | M3′ + T6 |
| 4 编造身份证号 | `no_invention` fail → 只重写该节 → 仍 fail 则降级标注 | T6,T8 |
| 5 派生值偏差 5% | `derived_recompute` fail → 单节重写 | T6,T8 |
| 6 评审=生产者 | `canDelegate` 拒 → 回落单 Agent 直答（不降档） | T7 |
| 7 用户中断 | 全树 `canceled`；`t_plan_task` 留痕 | M6′（部分 T10） |
| 8 进程重启续跑 | `succeeded` 跳过；`npa_ingest` 不重复 | T10 |
| 9 `mode=off` | 与现状完全一致（零协商、零额外 LLM） | T3（nil 短路） |
| 10 SOP 写坏 | linter 拒；0 次工具调用；日志给规则号 | T1 |

---

## 8. 明确不做（M4′ 边界）

- ❌ 不做 `IPlanSink` 事件与计划卡片 UI（#12/#13/#17 → **M5′**）
- ❌ 不做刷新恢复的前端联动（→ **M5′**）
- ❌ 不做并行波次收敛优化 / watchdog / 全树中断（→ **M6′**）
- ❌ 不做灰度与指标调优（→ **M7′**）
- ❌ 不改 `ISink` 既有方法、不改 `TurnRunner` 契约、不让协商轮进入 `ToolLoop`、不让启发式参与任何决策
- ❌ 不新增 `complexity.go` / `DetectSignals` / `IsComplex`

---

## 9. 附：M4′ 核心难点排序（按"最容易做错"）

| 排序 | 难点 | 为什么容易错 | 任务 |
|---|---|---|---|
| 1 | 返工后重新落盘 | `save` 与 `verify` 同深度并行；outline 无回边，必须代码写死 | T8 |
| 2 | 收敛判定 | 只写 `max_rounds` 会震荡烧钱；需"gap 集合不变即停" | T8 |
| 3 | 机检误杀 | `derived_recompute` 缺证据时若判 fail，会触发无意义重写 | T6 |
| 4 | `no_invention` 成本 | 全文 token 逐一对上游回读，不设上限会变性能事故 | T6 |
| 5 | 子轮历史污染 | 7 个章节子轮默认 `AllowRecord=true` 会写 7 份半成品 | T3 |
| 6 | 评估者自评 | 不排除生产者 → `accuracy` 虚高，质量闸门静默失效 | T7 |

> 其中 1/2/5/6 不是"写代码难"，而是**设计约束容易被顺手省略**——T8 标为"大"的原因不在代码量，在状态机与收敛条件。

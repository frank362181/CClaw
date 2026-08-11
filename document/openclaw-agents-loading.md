# OpenClaw 何时读入所有 agent —— 源码分析

> 结论先行：**OpenClaw 不是在启动时一次性把所有 agent 实例化加载，而是"配置一次解析 + 按需惰性读取"**。
> 即："读入所有 agent" 发生在解析配置时（数据层，zod 校验 + 物化默认条目）；"真正加载/运行某个 agent" 发生在按需惰性解析时（运行时层，首次被路由命中）。

---

## 一、触发时机总览

| 时机 | 发生什么 | 代码位置 |
|---|---|---|
| **启动 / 每次配置读取** | 解析 `openclaw.json`，**校验** `agents.entries`（恰好一个 default），并**物化默认 main** | `src/config/zod-schema.agents.ts`、`src/agents/agent-scope-config.ts` |
| **按需（各处调用）** | `listAgentEntries()` 把 `agents.entries` **全部条目**读成 `{id, ...}`；`resolveAgentConfig(id)` 合并 `defaults` | `src/agents/agent-scope-config.ts` |
| **首次被路由命中** | 该 agent 的工作区/技能/模型/会话才真正加载（agent "本体"启动） | bindings 路由 → session 创建 |
| **配置热更新** | `agents.entries` 标记为 `hot`，文件变化后热加载，无需重启 | `src/gateway/config-reload-plan.ts` |

---

## 二、代码证据 + 逐行解读

### ① 配置解析时校验全部 agent 条目 —— `src/config/zod-schema.agents.ts`

```ts
export const AgentsSchema = z.object({
  defaults: z.lazy(() => AgentDefaultsSchema).optional(),
  entries: z
    .record(z.string().regex(/^[a-z0-9_][a-z0-9_-]{0,63}$/i, "Invalid agent id"), AgentEntryConfigSchema)
    .optional(),
})
  .strict()
  .superRefine((value, ctx) => {
    const agents = Object.values(value.entries ?? {});
    const defaultCount = agents.filter((agent) => agent.default === true).length;
    if (defaultCount !== 1) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["entries"],
        message: `agents.entries must contain exactly one default=true entry (found ${defaultCount})`,
      });
    }
  });
```

**解读**：
- 这是**每次配置被读取/重载时都会执行**的校验（zod schema 挂在配置解析链路上）。
- `entries` 是 `record<agentId, entry>`（键值对，键=agent id，校验 id 格式）。
- `superRefine` 强制 `agents.entries` 中**恰好一个 `default:true`**——"读入全部 agent"的第一道关卡，在**配置层**保证 roster 合法。
- 原因：多 agent 必须有唯一默认路由，否则消息不知道该交给谁。

### ② 读取"所有 agent 条目"的核心函数 —— `src/agents/agent-scope-config.ts`

```ts
/** 读取 agents.entries（或旧版 agents.list），补上 id 键 */
export function listAgentEntriesWithSource(cfg: OpenClawConfig): ListedAgentEntry[] {
  const roster = readAgentRosterProperty(cfg);
  if (roster?.kind === "entries" && roster.value && typeof roster.value === "object") {
    return Object.entries(roster.value).map(([id, entry]) => ({
      entry: { ...(entry as Omit<AgentEntry, "id">), id },   // key 补成 id
      source: { kind: "entries", key: id },
    }));
  }
  if (roster?.kind !== "list" || !Array.isArray(roster.value)) return [];
  return roster.value.flatMap((entry, index) =>
    entry !== null && typeof entry === "object"
      ? [{ entry: entry as AgentEntry, source: { kind: "list" as const, index } }]
      : [],
  );
}

/** 规范入口：列出全部 agent 条目 */
export function listAgentEntries(cfg: OpenClawConfig): AgentEntry[] {
  return listAgentEntriesWithSource(cfg).map(({ entry }) => entry);
}

/** 唯一 agent id 列表（去重） */
export function listAgentIds(cfg: OpenClawConfig): string[] {
  const agents = listAgentEntries(cfg);
  if (agents.length === 0 && !hasAgentRosterProperty(cfg)) {
    return [LEGACY_IMPLICIT_AGENT_ID];   // 无 roster 时回退到隐式 "main"
  }
  // ... 去重后返回 id 数组
}
```

**解读**：
- `listAgentEntries` 就是"把 `agents.entries` 里**所有** agent 读出来"的函数。注意它**只在内存配置对象上做纯函数转换**（`Object.entries(...).map`），把每个 key 补成 `id`——**没有 new 任何 Agent 对象、没加载工作区/技能/模型**。
- 它**不是只在启动时被调用一次**，而是被系统各处**按需调用**：系统 agent 概览、`openclaw agents` 命令、doctor、模型注册、UI 等。
- 原因：把"读配置条目"做成**纯函数、可复用**，CLI/UI/网关/doctor 都能一致地枚举 agent，且易测试。

### ③ 每个 agent 的"生效配置 = 自己条目 覆盖 defaults" —— 同文件 `resolveAgentConfig`

```ts
export function resolveAgentConfig(cfg: OpenClawConfig, agentId: string): ResolvedAgentConfig | undefined {
  const id = normalizeAgentId(agentId);
  const entry = resolveAgentEntry(cfg, id) ??
    (!hasAgentRosterProperty(cfg) && id === LEGACY_IMPLICIT_AGENT_ID ? { id } : undefined);
  if (!entry) return undefined;
  const agentDefaults = cfg.agents?.defaults;
  return {
    name: readStringValue(entry.name),
    workspace: readStringValue(entry.workspace),
    agentDir: readStringValue(entry.agentDir),
    model: typeof entry.model === "string" || (entry.model && typeof entry.model === "object") ? entry.model : undefined,
    // ...
    reasoningDefault: entry.reasoningDefault,
    fastModeDefault: entry.fastModeDefault ?? agentDefaults?.fastModeDefault,   // 缺省回落到 defaults
    skills: Array.isArray(entry.skills) ? entry.skills : undefined,              // skills 显式替换，不合并
    contextLimits: entry.contextLimits && typeof entry.contextLimits === "object"
      ? { ...agentDefaults?.contextLimits, ...entry.contextLimits }              // 嵌套对象浅合并
      : agentDefaults?.contextLimits,
    // ...
  };
}
```

**解读**：
- 这是"**何时真正解析某个 agent**"的关键：`resolveAgentConfig(cfg, agentId)` 只解析**一个** agent，且是**惰性按需**调用。
- 它把 `agents.defaults` 作为基线，用**条目覆盖**：标量 `entry.x ?? defaults.x`；嵌套对象（如 `contextLimits`）浅合并；`skills` 则是**显式替换不合并**——与 OpenClaw 文档 "skills REPLACE not merge" 一致。
- 原因：defaults 是共享基线、entries 是差异化覆盖；解析是纯函数，可在任何时刻对任何 agent 计算。

### ④ 默认 agent 的"物化"（启动加载时）—— 同文件 `resolveDefaultAgentId`

```ts
export function resolveDefaultAgentId(cfg: OpenClawConfig): string {
  const agents = listAgentEntries(cfg);
  if (agents.length === 0) {
    // Runtime config loading materializes this entry. Keep the roster-property-absent
    // case for shipped Plugin SDK callers that still pass a pre-roster config object.
    if (!hasAgentRosterProperty(cfg)) return LEGACY_IMPLICIT_AGENT_ID;   // "main"
    throw new Error("No agents configured. Run `openclaw onboard` or `openclaw agents add` first.");
  }
  return normalizeAgentId((agents.find((agent) => agent?.default === true) ?? agents[0])!.id);
}
```

**解读**：
- 注释明确：**"Runtime config loading materializes this entry"**——在**运行时配置加载阶段**，OpenClaw 保证 roster 里有合法默认 agent：若 `agents.entries` 缺失/为空，自动物化隐式 `main`（`LEGACY_IMPLICIT_AGENT_ID`）。
- 这是"何时把 agent 读进来"中**最接近启动时读入**的一步：启动加载配置时 roster 被解析并确保有默认 agent。但**不会**把所有 agent 的工作区/技能/模型都加载。

### ⑤ 启动/命令时读取所有 agent 概要 —— `src/commands/agents.config.ts` `buildAgentSummaries`

```ts
export function buildAgentSummaries(cfg: OpenClawConfig): AgentSummary[] {
  const defaultAgentId = normalizeAgentId(resolveDefaultAgentId(cfg));
  const configuredAgents = listAgentEntries(cfg);          // ← 读全部 agent
  const orderedIds = configuredAgents.length > 0
    ? configuredAgents.map((agent) => normalizeAgentId(agent.id))
    : [defaultAgentId];
  const bindingCounts = new Map<string, number>();
  for (const binding of listRouteBindings(cfg)) { /* ... */ }
  return ordered.map((id) => {
    const workspace = resolveAgentWorkspaceDir(cfg, id);   // ← 解析工作区
    const identity = loadAgentIdentity(workspace);          // ← 读 IDENTITY.md
    // ...
  });
}
```

**解读**：`openclaw agents` / 系统 agent 概览在**需要时**（命令执行、启动概览）调 `listAgentEntries(cfg)` 枚举全部 agent，并逐个解析工作区、读 `IDENTITY.md`。这仍是"读配置+文件元数据"，不是启动 agent 运行时。

### ⑥ 热更新（不重启就重新读入）—— `src/gateway/config-reload-plan.ts`

```ts
{ prefix: "agents.entries", kind: "hot", ... }
```

**解读**：`agents.entries` 被标记为 **`hot`**（可热更新）。网关的文件 watcher 检测到 `openclaw.json` 变化后，**无需重启**即可重新读取/应用 agent 配置。这是"何时再次把所有 agent 读进来"——每次配置文件被保存时。

---

## 三、为什么这样设计（原因总结）

1. **启动快、资源省**：agent 只是**声明式配置**，不是常驻进程。若启动时把全部 agent 实例化（加载每个的工作区、技能、模型、会话），多 agent 场景会浪费大量资源。
2. **按需路由**：渠道消息经 `bindings` 路由到某个 agent，**只有被路由到的那个 agent** 才加载工作区/技能/模型/会话（session 创建时 `resolveAgentConfig` → workspace → bootstrap）。其余 agent 零成本。
3. **配置即数据、纯函数可复用**：`listAgentEntries` / `resolveAgentConfig` 是纯函数，CLI、UI、网关、doctor、system-agent 共用同一套读取逻辑，且极易测试。
4. **校验前置 + 默认兜底**：在配置解析层强制"恰好一个 default"、物化隐式 `main`，避免运行时路由悬空。
5. **声明式覆盖模型**：`defaults`（共享基线）+ `entries.*`（差异化覆盖），skills 显式替换——配置精简、语义清晰。
6. **热更新**：agent 配置标记为 hot，改配置不用重启，运维体验好。

---

## 四、一句话总结

> **"读入所有 agent"** = 解析配置时（zod 校验 + 物化默认条目），发生在启动 / 每次配置重载；
> **"真正加载某个 agent"** = 惰性按需（首次被路由命中、或任何需要其配置的时刻）。

---

## 附：关键源码文件清单（openclaw/openclaw，main 分支）

| 文件 | 作用 |
|---|---|
| `src/config/zod-schema.agents.ts` | agents 配置 schema 与"恰好一个 default"校验 |
| `src/agents/agent-scope-config.ts` | 读所有 agent 条目 / 合并 defaults / 解析工作区目录 |
| `src/agents/agent-scope.ts` | 上述能力的对外再导出 |
| `src/config/config.ts` / `src/config/io.ts` | 配置加载门面（loadConfig / readConfigFileSnapshot） |
| `src/gateway/config-reload-plan.ts` | 配置热更新计划（agents.entries = hot） |
| `src/commands/agents.config.ts` | `openclaw agents` 命令的摘要构建（读全部 agent） |
| `src/system-agent/overview.ts` | 系统 agent 概览（枚举全部 agent） |

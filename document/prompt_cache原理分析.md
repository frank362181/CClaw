## 1. prompt cache原理分析

### 1. 核心原理：前缀匹配 与 KV Cache

prompt cache的底层机制建立在transformer推理的KV Cache机制之上。当模型处理输入时，每一层注意力机制都会为每个token生成key和value状态，这些状态统称为KV Cache。

前缀匹配（prefix matching）是prompt cache的工作方式：API从请求的第一个token开始逐字节比对，只要前缀与之前处理过的内容完全一致，就直接复用已有的KV Cache，跳过重新计算；一旦前缀中任何位置发生变化，该位置之后的所有缓存全部失效。这就意味着：

- 不是“内容去重” --- 不是检测重复段落，而是从开头开始的精确前缀匹配；
- 缓存不是以token为单位，而是以block为单位组织，通常16或32个block为一个块，与PagedAttention的block结构对齐；
- 匹配是字节级精确的，一个字符的差异就会导致后续全部失效

服务端存储的是KV Cache表示和缓存内容的密码学哈希，而非原始prompt文本。决定KV Cache大小的核心公示为：2 * 层数 * K V头数 * head_dim * 序列长度 * 精度字节数；其中 2 表示 key 和 value各一份，所有架构优化--- GQA(分组查询注意力)、MLA (多头潜在注意力)、KV量化---本质上都在压缩这个公示中的某个因子。各个不同主流的大模型厂家KV Cache大小常见的阈值有：1024、2048、4096等token数量的阈值，如果一个请求的缓存前缀必须要达到这些档位token数量要求，缓存机制才会真正生效。如果prompt的前缀短于这个阈值，即使显式标记了cache_control，缓存也会被忽略----不报错、不警告、也不产生折扣。

推理引擎在物理层面并不是以逐token为单位存储和匹配KV Cache,而是以缓存块粒度为基本单位来匹配。常见的块粒度包括：

- 256 token：vLLM、Nano-vLLM等推理引擎的默认块大小；
- 64 token：部分DeepSeek缓存API 的匹配力度；
- 16 token：vLLM的prefix_match_unit可以设置得更细，用于哈希计算

块粒度决定了缓存匹配的“对齐精度”，如果块的大小是256 token，如果 第200个token附近的内容，那么从第256个token开始的缓存块仍然可以复用；但第0--255这个块因为内容变了，会整体失效，即使变化只影响了其中几个token。

### 2. Claude Code实现

#### 1. 三层缓存链

Claude Code并非只缓存system prompt，而是在请求的三个层次上显式设置cache_control断点，形成完整的缓存链：

| 层级       | 内容                            | 变更时机                          |
| ---------- | ------------------------------- | --------------------------------- |
| 系统提示   | 核心指令、工具定义              | 已加载的工具定义集合变更时        |
| 项目上下文 | CLAUDE.md、自动记忆、未限定规则 | 会话开始，或/clear ，/compact之后 |
| 对话       | 用户消息、Claude回复、工具结果  | 每一轮对话                        |

每个请求最多支持4个缓存断点（cache_control标记）。Claude Code利用这些断点，将静态内容前置、动态内容后置，最大化跨会话的缓存复用。

#### 2. 缓存断点的实际放置

Claude Code的实际请求体中，cache_control的标记放置策略如下：

1. 系统提示末尾："cache_control":{"type":"ephemeral"}标记在system prompt的最后一个text block上；
2. 最后一条用户消息：addCacheBreakpoints()在最后一条消息上标记 cache_control,使所有历史消息都在缓存前缀内容，每轮只需处理新增消息；
3. 工具定义：工具数组渲染在system之前，对齐变更会破坏整个缓存。

#### 3.动态更新策略：用消息而非修改prompt

当信息需要更新时（如文件变更、时间戳），Claude Code不会修改系统提示，而是通过下一轮的<system-reminder>标签将更新消息附加到用户消息或工具结果中。这样既传递了新消息，又保持了缓存前缀的完整性。

#### 4.缓存失效的关键触发条件

Claude Code的缓存有几个容易踩坑失效场景：

- 切换模型：每个模型有独立的缓存，切换模型会导致整个请求重新计算，即使内容完全相同；
- 改变Effort Level：在多数模型上，每个effort level有独立缓存，中途改变会重新计算整个请求（Opus 5.5和Fable 5.1例外）
- 修改CLAUDE.md：由于CLAUDE.md位于项目上下文曾，修改后不会在当前会话中生效，需要等待重启或/clear
- 工具定义变更：工具数组位于缓存链的最前端，任何工具定义的改变（包括顺序变化、参数变化）都会使所有后续缓存失效

### 3. OpenClaw的实现

#### 1. 配置驱动的缓存策略

OpenClaw通过cacheRetention参数控制缓存保留策略，支持三个值：

- "none"：不是用缓存
- "short"：提供商的默认缓存窗口（通常5分钟）
- "long"：扩展缓存窗口（通常1小时）

该参数支持全局默认、按模型、按Agent三个层级的配置，合并顺序为：全局默认->按模型覆盖->Agent特定覆盖。

#### 2.跨Provider的适配层

OpenClaw的核心设计是Provider感知的缓存适配层，针对不同模型提供商采用不同的缓存注入策略：

| Provider             | 缓存策略                                                     |
| -------------------- | ------------------------------------------------------------ |
| Anthropic直链        | 在system/developer prompt block上注入cache_control:{type:"ephemeral"} |
| OpenRouter Anthropic | 通过createOpenRoutersSystemCacheWrapper注入cache_control     |
| OpenAI兼容           | 发送prompt_cache_key保持缓存路由稳定；prompt_cache_retention直接传递 |
| Amazon Bedrock       | 支持cachRetention透传                                        |

#### 3.prompt_cache_key与会话路由

OpenClaw向OpenAI 兼容端点发送prompt_cache_key，用于在多次请求间保持缓存路由的稳定性---确保同一个会话的请求被路由到拥有对应KV Cache的同一推理节点。对于自定义/本地OpenAI兼容服务器，需要通过compact.supportsPromptCacheKey:true 显式启用。

#### 4.缓存保温与上下文修剪

OpenClaw提供了两个辅助机制来维持缓存有效性：

- Heartbeat保温：配置heartbeat.every:"55m"可在缓存窗口国旗前发送保活请求，减少空闲后重复写入缓存的成本
- contextPruning.mode:"cache-till"：在缓存TTL窗口过期后自动修剪旧工具结果上下文，防止空闲后的请求重新缓存过大的历史记录

#### 5.缓存性能的差距

与Claude Code相比，OpenClaw的缓存命中率明显偏低。根据Galileo对167个生产Agent会话的分析，Claude Code的缓存命中率为92.7%，而OpenClaw仅为23.8%；Claude Code处理67600 token仅需0.02美金，而OpenClaw每轮平均处理18500 token，需要0.06美金。

造成这种差距的根源在于：Claude Code是Anthropic的自由产品，深度掌握了缓存断点的放置策略；而OpenClaw作为跨Provider的Agent的通用框架，缓存注入是外挂式的，无法像Claude Code那样精细控制请求结构的每个层次。

### 4. 关键启示

无论是Claude Code还是 OpenClaw，prompt cache的有效利用都遵循几个共同原则：静态内容前置、动态内容后置；保持模型和推理配置稳定；避免在前缀中引入易变量（时间戳、UUID、随机排序的工具定义）。在缓存定价模型下，传统的“精简prompt”策略可能反而适得其反---更长但更稳定的前缀往往比频繁变化的短前缀更经济。

## 2. 设计和实现prompt cache

### 1.核心架构原则：缓存感知的请求流水线

无论使用哪种网关或框架，多厂商prompt cache的实现都遵循一条基本流水线：

​	Agent 逻辑 → 上下文编排 → 缓存标记注入 → Provider 适配 → 上游 API
​                     				↑
​              			缓存键 / 会话粘性管理

这条流水线需要解决三个层面的问题：

1. 请求结构层：保证prompt前缀在多次请求字节级稳定，把静态内容(system prompt、工具定义)前置，动态内容滞后；
2. provider适配层：根据目标厂商的缓存机制，决定是否注入cache_control的标记，以及标记放在哪里；
3. 会话路由层：确保同一会话的请求被路由到持有对应KV Cache的同一推理节点，否则缓存即使存在也无法命中。

### 2.方案一：统一网关/适配层

LiteLLM：自动注入缓存检查点

LiteLLM是目前多厂商prompt cache适配最成熟的参考实现之一。它对两类厂商采取不同的策略：

| 厂商类型 | 代表                                       | LiteLLM策略                |
| -------- | ------------------------------------------ | -------------------------- |
| 显式缓存 | Anthropic,Bedrock,Vertex AI,DashScope/Qwen | 自动注入cache_control标记  |
| 隐式缓存 | OpenAI,DeepSeek,xAI                        | 无需标记，provider自动处理 |

对于显式缓存的厂商，LiteLLM可以通过一个配置项开启自动注入：

	litellm_settings:
		enable_anthropic_prompt_caching:true

  开启后，LiteLLM会在system prompt末尾和尾轮消息各放一个检查点，使稳定前缀被缓存，而检查点随对话推进。它还会遵守厂商的4个缓存块上限，且如果客户端已经自带了cache_control,LiteLLM会“让位”不重复注入。

对于DashScop/Qwen,LiteLLM的cache_control注入是原声支持的；对于DeepSeek，则依赖自动缓存，无需标记。关键限制：LiteLLM的自动注入默认只对Claude模型生效。要让Qwen或DeepSeek也走显式缓存路径(例如OpenRouter接入时)，需要扩展其allowlist，将qwen和deepseek加入CacheControlSupportedModels。






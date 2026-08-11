
# OpenClaw配置向解析

## auth段

定义模型在调用时的认证方式
    "auth":{ //表示调用AI时的认证方式
        "profiles":{
            "deepseek:default":{
                "provider":"deepseek", //对应的models.provider的提供商
                "mode":"api-key"
            }
        }
    },

## models 段

定义所有的AI的模型配置：
    "models":{
        "mode":"merge", // 取值有merge / override，是与内置的配置合并还是覆盖
        "providers":{
            "deepseek":{
                "baseUrl":"",
                "api":"openai-completions",
                "models":[
                    {
                        // 模型项字段说明：
                        //   id            模型标识（agents.*.model 用 provider/id 引用）
                        //   name          展示名
                        //   reasoning     是否推理型模型
                        //   input         输入模态：text | image | audio ...
                        //   cost          每百万 token 美元单价（input/output/cacheRead/cacheWrite）
                        //   contextWindow 上下文窗口 token 数
                        //   maxTokens     单次最大输出 token
                        //   compat        兼容参数：supportsReasoningEffort 推理力度；supportsUsageInStreaming 流式用量；maxTokensField 请求中 max tokens 字段名
                        "id": "deepseek-v4-flash",
                        "name": "DeepSeek V4 Flash",
                        "reasoning": true,
                        "input": [ "text" ],
                        "cost": {
                        "input": 0.14, // 输入单价（美元/百万 token）
                        "output": 0.28, // 输出单价
                        "cacheRead": 0.028, // 缓存命中读取单价
                        "cacheWrite": 0 // 缓存写入单价
                        },
                        "contextWindow": 1000000,
                        "maxTokens": 384000,
                        "compat": {
                        "supportsReasoningEffort": true,
                        "supportsUsageInStreaming": true,
                        "maxTokensField": "max_tokens"
                        },
                        "api": "openai-completions"
                    },
                    {
                        "id": "deepseek-v4-pro",
                        "name": "DeepSeek V4 Pro",
                        "reasoning": true,
                        "input": [ "text" ],
                        "cost": {
                        "input": 1.74,
                        "output": 3.48,
                        "cacheRead": 0.145,
                        "cacheWrite": 0
                        },
                        "contextWindow": 1000000,
                        "maxTokens": 384000,
                        "compat": {
                        "supportsReasoningEffort": true,
                        "supportsUsageInStreaming": true,
                        "maxTokensField": "max_tokens"
                        },
                        "api": "openai-completions"
                    }
                ]
            }
        }
    }

## agents 段

agents是OpenClaw智能体全局的默认配置，描述了【所有智能体在未单独覆盖时，默认如何选择模型、在哪个目录工作、如何管理上下文、能并行多少、如何展示思考过程。】

    "agents": {
        // agents.defaults：所有 agent 的共享基线（entries 中省略的字段继承这里）
        "defaults": {
            // model：主模型 + 回退链；可简写为字符串 "provider/model"（仅主模型）
            "model": {
                "primary": "deepseek/deepseek-v4-pro",
                // 修正：原 fallback 指向未定义的 custom-api-deepseek-com 提供商，改为同提供商内的 flash
                "fallbacks": [ "deepseek/deepseek-v4-flash" ]
            },
            // models：模型别名表（UI/渠道展示用友好名称；键为 provider/model）
            "models": {
                "deepseek/deepseek-v4-flash": { "alias": "DeepSeek-V4-Flash" },
                // 修正：原键名大小写不一致（V4-Pro），与 primary 对齐为小写
                "deepseek/deepseek-v4-pro": { "alias": "DeepSeek-V4-Pro" }
            },
            "workspace": "/Users/chuliang/.openclaw/workspace", // 默认工作区（文件操作根目录）
            // compaction：上下文压缩策略
            //   mode 可取值：default（整体摘要）| safeguard（分块摘要，长历史更稳）
            //   reserveTokensFloor 压缩时至少保留的 token 数
            "compaction": {
                "mode": "default",
                "reserveTokensFloor": 40000
            },
            "maxConcurrent": 4, // 主 agent 并发会话上限
            "subagents": {
                "maxConcurrent": 8 // 单个 agent 可派生子 agent 并发上限
            },
            // reasoningDefault：推理展示方式 off（不显示）| on（生成后附带）| stream（实时流式）
            "reasoningDefault": "stream",
            // toolProgressDetail：工具进度详细度 explain（简洁）| raw（附具体命令细节）
            "toolProgressDetail": "explain",
            // 共享技能基线：未设置 agents.entries.*.skills 的 agent 继承它（[]=禁用；省略=不限制）
            "skills": [ "coding-agent" ]
        },

        "entries": {
            // agents.entries.*：各 agent 定义
            //   继承规则：省略的字段继承 defaults；skills 为显式替换（不合并）
            //   字段：default（唯一 true 为默认 agent）| name | workspace | model
            //         identity{name,emoji,theme} | skills[] | tools{profile|allow|deny}
            // 默认 agent：未覆盖的字段全部继承 defaults
            "main": {
                "default": true, // 有且仅有一个默认 agent
                "name": "Main Assistant",
                "workspace": "/Users/chuliang/.openclaw/workspace",
                "identity": {
                "name": "Clawd",
                "emoji": "🦞", // 用于确认表情 ackReaction 等
                "theme": "helpful assistant"
                },
                "skills": [ "coding-agent" ],
                "reasoningDefault": "stream"
            },

            // 编码 agent：覆盖模型为快速低延迟的 flash，工作区独立
            "coding": {
                "name": "Coding Agent",
                "workspace": "/Users/chuliang/.openclaw/workspace-coding",
                "model": "deepseek/deepseek-v4-flash", // 覆盖 defaults 主模型
                "skills": [ "coding-agent" ],
                // tools.profile 可取值：full | minimal | coding | messaging
                "tools": {
                "profile": "coding"
                }
            },

            // 社交 agent：显式空技能列表（禁用技能），仅保留消息/会话能力
            "social": {
                "name": "Social Agent",
                "workspace": "/Users/chuliang/.openclaw/workspace-social",
                "skills": [], // 显式空列表 = 禁用所有技能
                "tools": {
                "allow": [ "message", "session" ] // 工具白名单：仅消息与会话
                }
            }
        }
    }

## tools段

系统级思考：工具配置形成一个“能力沙盒”--代理可读、写、执行，但被白名单、黑名单、安全模式三层过滤。任何新增工具（如camera.snap）若未在allow中声明，代理无法调用；而即使被允许，若在denyCommands中，也会被网关拒绝。双重校验是纵深防御的体现。

    // tools：工具策略
    //   profile 可取值：full | minimal | coding | messaging
    //   web.search.provider 可取值：brave | google | tavily | duckduckgo | serper | bing ...
    //   exec.security 可取值：full | workspace-only | sandbox ...
    //   exec.ask 可取值：off（不询问）| on（每次询问）| allowlist（白名单内不问）
    "tools": {
            "profile": "full",
            "web": {
            "search": {
                "enabled": true,
                "provider": "brave"
            }
        },
        "exec": {
            "host": "gateway",
            "security": "full",
            "ask": "off"
        },
        "allow": [ // 全局允许的工具：exec|read|write|edit|apply_patch| message|session|browser|process|cron|nodes ...
            "exec",
            "read",
            "write",
            "message",
            "session"
        ]
    }

tools段中定义的工具，必须与plugins中定义的配置相匹配才行。

## bingdings 段

    // bindings：多 agent 路由（把渠道账号路由到对应 agent）
    //   match.accountId 可取值：default（仅默认账号）| *（渠道内任意账号）| 具体账号名
    //   match.peer（可选）可精确到对端：{ kind: direct|group|channel, id }
    "bindings": [
        { "agentId": "main",   "match": { "channel": "feishu",   "accountId": "default" } },
        { "agentId": "main",   "match": { "channel": "telegram", "accountId": "*" } },
        { "agentId": "coding", "match": { "channel": "telegram", "accountId": "*" } }
    ]

该段定义了指定渠道、指定账号是只能使用哪个agent；* 表示所有账号

## messages 段

定义消息行为配置，核心是ack反应(ack reaction):当openclaw收到一条入站消息需要开始处理时，现在触发消息上发送一个emoji，表示"收到，正在处理"。
ackReactionScope决定什么时候发这个emoji。ackReactionScope的取值范围:
    - group-mentions:默认值 仅当收到了 @agent时发确认反应；私聊（DM）和没@的群消息都不发；
    - group-all:群里每一条消息都发
    - direct:只在私聊时
    - all：全部场景都发；
    - off/none:完全禁用 ack反应

什么情况下不起作用：渠道必须支持emoji反应：telegram、discord、slack、matix等
渠道启用了反应能力，需channels.<x>.actions.reactios: true 且配置了 ackReation
消息被判定未 需要agent处理 才触发，如被 @、DM、白名单等
优先级：渠道account -> 渠道 channel -> messages.ackReactionScope -> 默认group-mentions

## commands段

这是控制OpenClaw的内置斜杠命令(/commands,如 /help,/clear,/config,/mcp,/restart,/bash等)的启用与解析方式。字段含义：
    - native: 当前值 auto,在注册原生命令，如Discord/Teleram的原生命令，auto/off表示启用/禁用
    - nativeSkills: 当前值 auto，同上，对原生skills命令--把每个已启用的skill注册成一条可调用的命令
    - restart:true，允许/restart重启agent实例或进程
    - ownerDisplay raw所有者身份/命令的显示方式，raw=直接显示原始ID；hash表示进行hash后再显示
commands只有在输入/，解析/开头的命令时才会生效。纯API内部调用不经过命令解析，这个对他们不起作用。

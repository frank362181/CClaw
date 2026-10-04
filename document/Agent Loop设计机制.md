## Agent Loop设计机制

### 1. 状态机设计机制

当前Agent Loop有两条实现路径：同步路径runToolLoop和流式路径streamToolLoop，都位于chat_service.go中，代码结构一致---都是[调用LLM -> 执行工具 -> 结果回填 -> 再调用]的多轮循环，上限为AgentToolLoopMax = 30(工具调用轮次配置)。

#### 1. 进入循环之前

在进入到Agent Loop之前，先对会话进行规整，执行步骤如下：

1. 解析任务：ChatStream -> prepareChat，校验消息 -> 规整sessionId/turnId -> 混合路由解析Agent -> 解析LLP Provider -> 加载 SKILL.md -> 意图门筛选技能 -> 组装system prompt(含技能SOP) -> 生成首轮工具策略 plan -> 估算工具占用窗口
2. 并发/中断登记：ChatStream,获取Agent并发闸；派生streamCtx并注册到TurnSrtreamMgr(供中断使用)；叠加整轮运行超时runCtx
3. 装配上下文：assembleChatContext,按上下文策略读历史 -> 装配 [system + 历史摘要] + [历史消息] + [本轮用户消息]
4. 选路：ChatStream,若开启function-calling且Agent有工具 -> 走工具循环;若无走纯流失ChatCompletionStreamMessages

#### 2. 循环开始

1. 第一步：选择本轮工具集与tool_choice
   - 函数：turn.plan.RoundTools(visibleTools,i),位于harness/tool/tool_choice_policy.go:143行
   - 功能：第 0 轮 用首轮计划（命中force-call时可能发required，并显式关thinking）；第1轮起一律回全景工具 + auto。避免模型调用完强制工具函数后“看不见”契约工具函数而卡斯多步流程
2. 第二步：预算贴合
   - 函数：ctxmgr.FitMessages(turn.llm,messages,policy)，位于ctxmgr/context_assembler.go:316行
   - 功能：工具结果不断回填回挤压LLM的窗口，每轮请求前把最老的历史裁掉，防止LLM“超窗口”报错。
3. 第三步：调用LLM（带工具）
   - 函数：同步CompleteWithTools/流式：StreamWithTools,位于harness/llm_client.go:112/177行
   - 功能：发起一次chat/completions。返回content正文部分、calls(工具调用、arguments已按index拼接完整)、finishReason。
   - 流式额外事件：onChunk(正文增量)、onReasoning(思考增量)、onToolIntent(模型流式声明要调用工具时立即回传intent事件，并分配seq)
   - 重试：429/5XX/断流按指数退避重试；一旦已向客户端输出增量即不再重试（避免内容重复）。LLM拒绝调用强制调用参数时走一次降级重试。
4. 第四步：强制调用哨兵函数
   - 函数：tool.VerifyForcedCall(plan,calls,toolChoice,scope),位于tool/tool_choice_policy.go:262行
   - 功能：若本轮转发了required但模型没给任何工具调用，记录告警（用于可观测，不阻断）
5. 步骤5:错误处理
   - 功能：LLM调用出错 -> 包装为LlmError返回，整轮结束。
6. 步骤6:终止判断
   - 功能：模型给出最终文本、为请求工具 -> 唯一正常结束，返回content + finishReason
7. 步骤7:死循环守卫终止判断
   - 函数：guard.ShouldStop(),位于harness/security/chat_tool_guard.go:67
   - 功能：若首位已提示过一次，模型再次命中 -> 判断无效收敛(避免打扰),以ChatToolLoopGuardKind终止本轮(避免空转)，此时已产出的文本仍带回留痕
8. 步骤8:回填assistant消息
   - 函数：ctxmgr.BuildToolCallWithContentMessage(content,calls)
   - 功能：把本轮的文本 + tool_calls作为assistant消息追加进messages(协议要求：每个tool_call必须有配对的tool应答)
9. 步骤9:执行工具
   - 同步路径：串行execbox.RunScript(ctx,scriptFile,args)
   - 流式路径：通论多个工具并行goroutine 执行(RunScriptStream)：
     - 脚本解析失败 -> 不进execbox,直接按执行失败收尾；
     - 每个脚本stdout逐行经lineCh汇总，由唯一转发goroutine串行推送progress事件（保证SSE无并发写）
     - 静默心跳：超过ToolCallingHeartbeatInterval无输出时推[工具执行中...]进度行
     - 推送running/done/error事件
   - 函数：execbox.RunScript/execbox.RunScriptStream，位于harness/execbox/script_runner.go 91/99行
   - 功能：参数JSON走stdin,stdout为结果，内置超时（默认120S）、输出上限为256KB，退出码为非0或超限即失败
10. 步骤10:两阶段留痕
    - 函数record.Start,执行前登记，recorder.Finish执行后回填结果
    - 功能：工具执行中被中断时，[此次调用发生过]仍留在历史t_turn_tool，不丢痕
11. 步骤11:死循环判断于提示回填
    - 函数guard.ApplyLoopGuard(name,args,ok,result,onNotice)，位于chat_tool_guard.go：76行
    - 功能：命中规则（总量/连续失败/同工具/完全相同/交替调用）时：
      - 写告警日志；
      - 流失路径下发notice（提醒用户“模型在重复调用工具,已提醒换策略”）
      - 把守卫提示追加到工具结果之后回填给模型（提示必须进messages才起作用）
12. 步骤12:回填tool消息
    - 函数: ctxmgr.BuildToolResultMessage(callID,results)
    - 功能：把工具结果作为role=tool消息追加进messages,供下一轮模型消费。娶不到脚本也不能continue---缺一条消息会被LLM判断为协议错
13. 步骤13:进入下一轮
    - 循环回到步骤1，直到命中终止条件

### 2. 请求消息裁剪

在每轮LLM API调用前，都必须对请求消息进行token测算，所有的token的测算都是在securiy/token_budget.go中完成，FitMessages函数只是它的消费者。对token预算进行预测发生在4个地方：

1. 装配前：prepareChat 计算系统启动文件、工具函数的预留,在chat_service.go中

2. 装配时：assembleChatContext ->AssemblContext

   Policy := hCommon.GetContextPolicy(agent, int(llmProvider.ContextWindow))

   Policy.ToolsReserveTokens = toolsReserveTokens

   ...

   ctxmgr.AssembleContext(systemText,records,req.ReqMsg,policy)

3. 每轮调用时：FitMessage,在chat_service.go文件中streamToolLoop函数中720行左右的FitMessages中，FitMessages是根据预算去裁历史；

4. 构造请求体时：DynamicMaxTokens，该函数保证 用户消息估算 + max_tokens <= context_windows，避免长输入时被LLM拒绝。DynamicMaxTools由函数BuildChatCompletionRequestWithToolChoice,在同步调用LLM API的函数ComplteWithTools,与流式StreamWithTools两个函数中都将调用该函数。

### 3. Loop守卫机制

#### 1. Loop守卫现状

有两条防线可以终止Agent Loop，分别是：

- 轮次上限，hCommon.AgentToolLoopMax=30,硬上限，主动收敛；
- 死循环 security.LoopGuard, 模式匹配：
  - 正常结束：len(calls) == 0，返回最终文本，依赖LLM“愿意停”
  - 守卫终止：guard.ShouldStop(),第二次命中，仅覆盖守卫规则匹配
    - 总量，总共调用工具函数 800 次；
    - 连续失败：5次；
    - 同工具连续调用：10次，中间穿插其他工具，则失效；
    - 完全相同：3，AAA；
    - 二元交替：ABABAB；
  - 运行时超时：AgentRunTimeout = 600s，兜底但粗暴
  - 用户中断：streamCtx取消，依赖于用户在场

守卫拦不住的典型失控形态：

- 单调推进型：每次工具名/参数都不同（如每轮加个新过滤条件、翻下一页、换个搜索词），结果也不同 -> 5条规则全不命中，一路跑到800次或600s
- 3元以上调用周期：ABC ABC...
- 稀疏重复：重复之间夹一个不同调用 -> 计数被清零

因此，守卫对合法长任务和渐进式失控无法区分，二者都不能靠守卫判别。且存在其他与守卫无关的风险：

- 成本：每轮一次LLM调用。无上限 = 单轮token花费无界（仅受600s限制）
- 消息膨胀：messages只增不减，FitMessages不修改原来的slice，每轮EstimateInputTokens全量扫描 -> 轮次越多每轮越慢；
- 末组不可裁：FitMessages永远保留末组，当前轮工具链撑爆窗口时无解
- 并发资源占用：Agent并发闸（acquireAgentGate）整段持有，一个失控轮次占用600s，拖垮同agent其他用户；


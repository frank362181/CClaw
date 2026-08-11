{
    // agent 的默认配置
    "agentconfig": {
        "default": {
            "model": {
                "primary": "deepseek-v4-pro",
                "fallbacks": ["deepseek-v4-flash"]
            },
            "workspace": "",

            "compaction":{
                "mode":"default",
                "reserveTokensFloor": 40000
            },
            "concurrent":{
                "maxConcurrent":8,
                "subagents":4
            },

            "reasoningMode":"stream",
            "toolProgress":"explain",
            "skills":[]
        }
    },

    //定义消息的行为：group-mentions | group-all | direct | all | off/none
    "messages": {
        "ackReactionScope":"group-mentions"
    },

    // 会话隔离维度 :main | per-peer | per-channel-peer | per-account-channel-peer
    "session":{
        "dmScope":"per-channel-peer"
    },

    // 内置命令 native/nativeSkills:auto(自动启用) | off
    // ownerDisplay:raw/plain,原始格式/文本
    "commands":{
        "native":"auto",
        "nativeSkills":"auto",
        "restart":true,
        "ownerDisplay":"raw"
    }
}
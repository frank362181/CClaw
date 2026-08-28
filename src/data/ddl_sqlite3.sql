-- 创建 LLM 供应商配置表
CREATE TABLE t_llm (
    -- 主键与标识
                                id INTEGER NOT NULL primary key AUTOINCREMENT,  -- 记录ID，自增
                                provider_id VARCHAR(8) NOT NULL unique,         -- 大模型的ID，业务主键

    -- 归属与权限隔离
                                owner_id VARCHAR(16) NOT NULL DEFAULT '000000',  -- 与登录用户的category有关

    -- 基本信息
                                provider_name VARCHAR(64) NOT NULL,              -- LLM名称
                                alias_name VARCHAR(64) DEFAULT NULL,             -- LLM别名

    -- API连接信息
                                base_url VARCHAR(128) NOT NULL,                  -- API端点地址
                                model VARCHAR(64) DEFAULT NULL,                  -- 实际请求使用的模型名(厂商命名,如 deepseek-v4-pro);为空回退 provider_id
                                vendor VARCHAR(32) NOT NULL,                     -- LLM厂家名称
                                api VARCHAR(32) NOT NULL,                        -- API协议类型

    -- 能力参数
                                reasoning INTEGER NOT NULL DEFAULT 0,            -- 是否支持推理 (0/1)
                                reasoning_ex INTEGER DEFAULT 0,                  -- 是否支持推理力度参数 (0/1)
                                input INTEGER NOT NULL DEFAULT 0,                -- 支持的输入模态 (0:text/1:image/2:audio)
                                context_window INTEGER NOT NULL,                 -- 上下文窗口大小 (token数量)
                                max_tokens_field VARCHAR(64) DEFAULT NULL,       -- max_tokens的字段名
                                usage_in_streaming INTEGER NOT NULL DEFAULT 0,   -- 流式响应是否带用量 (0/1)

    -- 计费信息 (每百万token单价)
                                input_cost REAL NOT NULL,                        -- 输入单价
                                output_cost REAL NOT NULL,                       -- 输出单价
                                cached_read_cost REAL DEFAULT NULL,              -- 缓存读单价
                                cached_write_cost REAL DEFAULT NULL,            -- 缓存写单价

    -- 认证与状态
                                auth_mode INTEGER NOT NULL DEFAULT 0,            -- 认证方式 (0/1/2/3)
                                api_key VARCHAR(128) DEFAULT NULL,               -- 供应商鉴权密钥(不入 JSON,仅服务端读取)
                                disabled INTEGER NOT NULL DEFAULT 0,             -- 是否禁用 (0/1)
                                deleted INTEGER NOT NULL DEFAULT 0,              -- 是否删除 (0/1)
                                d_version INTEGER NOT NULL DEFAULT 0             -- 乐观锁版本号
);

-- 创建索引以提升查询性能
CREATE INDEX idx_t_llm_owner_id ON t_llm(owner_id);
CREATE INDEX idx_t_llm_vendor ON t_llm(vendor);


-- 创建 Agent 信息表
CREATE TABLE t_agent (
    -- 主键与标识
                         id INTEGER NOT NULL primary key AUTOINCREMENT,   -- 记录ID，自增
                         agent_id VARCHAR(8) NOT NULL unique,            -- Agent ID，业务主键

    -- 归属与权限隔离
                         owner_id VARCHAR(16) NOT NULL DEFAULT '000000',  -- 与登录用户的category有关

    -- 基本信息
                         agent_name VARCHAR(64) NOT NULL,                 -- Agent名称（可重复）
                         description VARCHAR(128) DEFAULT NULL,           -- Agent描述（用于列表展示）
                         primary_model VARCHAR(32) NOT NULL,              -- 主模型（LLM Provider ID）
                         fallbacks VARCHAR(128) DEFAULT NULL,             -- 兜底模型，逗号分割
                         is_default INTEGER NOT NULL DEFAULT 0,           -- 是否是默认模型

    -- 路径与工作空间
                         agent_dir VARCHAR(128) NOT NULL,                 -- agent启动文件路径（相对路径）,每个agent一个
                         workspace VARCHAR(256) NOT NULL,                 -- 工作空间,每个所属的单位一个工作空间

    -- 形象配置
                         image_name VARCHAR(32) DEFAULT NULL,             -- 公共形象名称
                         image_emoji VARCHAR(32) DEFAULT NULL,            -- 公共形象表情
                         image_theme VARCHAR(64) DEFAULT NULL,            -- 公共形象主题

    -- 上下文与压缩策略
                         compaction_mode INTEGER NOT NULL DEFAULT 0,      -- 上下文压缩策略 (0:default/1:safeguard)
                         reverse_token_floor INTEGER DEFAULT NULL,        -- 压缩时至少保留的token数量

    -- 并发与限制
                         max_concurrent INTEGER NOT NULL DEFAULT 1,       -- 主agent并发会话上限
                         subagents INTEGER NOT NULL DEFAULT 0,            -- 可派生sub agent的上限

    -- 推理与工具配置
                         reasoning_mode INTEGER NOT NULL DEFAULT 0,       -- 推理展示方式 (0:off/1:on/2:stream)
                         tool_progress INTEGER NOT NULL DEFAULT 0,        -- 工具执行进度 (0:explain/1:raw)

    -- 状态与乐观锁
                         deleted INTEGER NOT NULL DEFAULT 0,              -- 是否删除 (0/1)
                         disabled INTEGER NOT NULL DEFAULT 0,             -- 是否禁用 (0/1)
                         d_version INTEGER NOT NULL DEFAULT 0             -- 乐观锁版本号
);

-- 创建索引以提升查询性能
CREATE INDEX idx_t_agent_owner_id ON t_agent(owner_id);
CREATE INDEX idx_t_agent_agent_name ON t_agent(agent_name);

-- 插入一条 Agent 记录
INSERT INTO t_agent (
    agent_id,
    owner_id,
    agent_name,
    description,
    primary_model,
    agent_dir,
    workspace,
    max_concurrent,
    subagents
) VALUES (
             'agent_001',
             '100001',
             '不良资产智能助手',
             '生成不良资产尽调报告',
             'deep_seek_v4_flash',
             'amc_jk',
             'jk',
             10,
             3
         );


-- 创建 Skill 信息表
CREATE TABLE t_skill (
    id INTEGER NOT NULL primary key AUTOINCREMENT,
    skill_id VARCHAR(8) NOT NULL unique,
    owner_id VARCHAR(16) NOT NULL DEFAULT '000000',
    skill_name VARCHAR(64) NOT NULL,
    skill_dir VARCHAR(256) NOT NULL,
    skill_scope INTEGER NOT NULL DEFAULT 0,
    disabled INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0,
    d_version INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_t_skill_owner_id ON t_skill(owner_id);
CREATE INDEX idx_t_skill_skill_name ON t_skill(skill_name);
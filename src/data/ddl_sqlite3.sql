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


-- 创建 Agent 内容路由关键词规则表(方案C:关键词路由层)
CREATE TABLE t_agent_route_rule (
    id INTEGER NOT NULL primary key AUTOINCREMENT,
    owner_id VARCHAR(16) NOT NULL DEFAULT '000000',   -- 归属(与登录用户的 comCode 对齐)
    agent_id VARCHAR(8) NOT NULL,                     -- 目标 Agent(关联 t_agent.agent_id)
    keywords VARCHAR(512) NOT NULL,                   -- 逗号分隔的关键词,命中任一即路由
    priority INTEGER NOT NULL DEFAULT 0,              -- 命中优先级(数值大者优先)
    disabled INTEGER NOT NULL DEFAULT 0,              -- 是否禁用(0/1)
    deleted INTEGER NOT NULL DEFAULT 0                -- 是否删除(0/1)
);

CREATE INDEX idx_t_agent_route_rule_owner_id ON t_agent_route_rule(owner_id);
CREATE INDEX idx_t_agent_route_rule_agent_id ON t_agent_route_rule(agent_id);

-- 不良资产智能助手(agent_001)的关键词路由规则示例(按需调整)
INSERT INTO t_agent_route_rule (owner_id, agent_id, keywords, priority) VALUES (
    '100001',
    'agent_001',
    '不良资产,尽调,尽职调查,估值,评估,处置,清收,回收,债权,债务,抵押,质押,AMC,资产包,组包,诉讼,法拍,司法拍卖,核销,债转股,重组,资产保全,财务报表,财务分析,财务尽调,财务尽职调查,财务评估,财务处置,财务清收,财务回收,财务债权,财务债务,财务抵押,财务质押,财务AMC,财务资产包,财务组包,财务诉讼,财务法拍,财务司法拍卖,财务核销,财务债转股,财务重组,财务资产保全',
    10
);

-- ============================================================
-- 会话管理模块（SQLite3）
-- 严格按照 Excel 设计：表名、字段名、类型、长度、属性
-- 例外：session_id / turn_id 由 VARCHAR(32) 加宽为 VARCHAR(40)，
--       原因：session_id 为“单位编码前缀 + UUID”（见 common.NewCompactId），
--             40 可容纳前缀且保留完整 UUID 随机位；turn_id 为纯 UUID（32）。
-- ============================================================

-- 表 1：会话管理表 t_session
CREATE TABLE IF NOT EXISTS t_session (
                                         ID                  INTEGER PRIMARY KEY AUTOINCREMENT,  -- Auto Incr Not Null
                                         session_id          VARCHAR(40)  NOT NULL UNIQUE,       -- Not Null Primary Key（SQLite 用 UNIQUE 替代）
    session_name        VARCHAR(64)  NOT NULL,              -- 会话名称
    agent_id            VARCHAR(8)   NOT NULL,              -- 该 session 对应的 agent
    begin_timestamp     INTEGER      NOT NULL,              -- 会话开始时间（1970-1-1 0:0:0 起的秒数）
    chat_type           INTEGER      NOT NULL,              -- 聊天类型：0/1 私聊/群聊
    chat_channel        INTEGER      NOT NULL,              -- 聊天渠道
    sender              VARCHAR(16)  NOT NULL,              -- 创建 Session 的 UserName
    archived            INTEGER      NOT NULL DEFAULT 0,    -- 是否归档：0/1 否(default)/是
    archived_timestamp  INTEGER,                            -- 归档时间
    chat_data           VARCHAR(256),                       -- 聊天的历史文件，文件名
    ended               INTEGER      NOT NULL DEFAULT 0,    -- 是否结束：0(default)/1 否/是
    end_reason          INTEGER,                            -- 结束原因：0 空闲超时；1 手动结束；2 触发 compact
    end_timestamp       INTEGER,                            -- 结束时间
    removed             INTEGER      NOT NULL DEFAULT 0     -- 是否删除：0/1 否/是
);

-- 表 2：聊天记录表 t_turn
CREATE TABLE IF NOT EXISTS t_turn (
                                      ID          INTEGER PRIMARY KEY AUTOINCREMENT,  -- Auto Incr Not Null
                                      turn_id     VARCHAR(40)  NOT NULL UNIQUE,       -- Not Null Primary Key（SQLite 用 UNIQUE 替代）
    session_id  VARCHAR(40)  NOT NULL,              -- 会话ID，所属 Session
    seq         INTEGER      NOT NULL,              -- 消息序号，从 1 开始，无顺序则为 0
    chat_time   INTEGER      NOT NULL,              -- 聊天时间（1970-1-1 0:0:0 起的秒数）
    chat_index  INTEGER,                            -- 问题的索引（在 memory_{datetime}.md 中的位置）
    resp_time   INTEGER      NOT NULL,              -- 响应时间
    resp_index  INTEGER,                            -- 回复消息的索引位置
    FOREIGN KEY (session_id) REFERENCES t_session(session_id)
);

-- 表 3：聊天调用工具表 t_turn_tool
CREATE TABLE IF NOT EXISTS t_turn_tool (
                                           ID          INTEGER PRIMARY KEY AUTOINCREMENT,  -- Auto Incr Not Null
                                           turn_id     VARCHAR(64)  NOT NULL,              -- turn ID
    seq         INTEGER      NOT NULL,              -- 调用工具的顺序号
    tool_called INTEGER      NOT NULL DEFAULT 0,    -- 是否调用了工具：0/1 否(default)/是
    call_time   INTEGER,                            -- 调用的时间
    tool_name   VARCHAR(64)  NOT NULL,              -- 工具名称
    tool_resp   VARCHAR(256),                       -- tool 的返回值
    resp_time   INTEGER,                            -- 返回的时间
    FOREIGN KEY (turn_id) REFERENCES t_turn(turn_id)
);

-- 可选索引，提升查询效率
CREATE INDEX IF NOT EXISTS idx_t_session_agent_id ON t_session(agent_id);
CREATE INDEX IF NOT EXISTS idx_t_session_sender ON t_session(sender);
CREATE INDEX IF NOT EXISTS idx_t_turn_session_id ON t_turn(session_id);
CREATE INDEX IF NOT EXISTS idx_t_turn_tool_turn_id ON t_turn_tool(turn_id);

-- 幂等保证（阶段1双写新增）：同一会话内 seq 唯一。
-- 会话内 seq/索引分配由服务端串行化，该唯一约束作为最后一道防线，
-- 防止重试或多实例并发导致同一轮被重复落库。
CREATE UNIQUE INDEX IF NOT EXISTS idx_t_turn_session_seq ON t_turn(session_id, seq);
# 项目简介

## 1. done

- 生成SSL所需的cert/key文件命令：(open command line with Administrator)
  - 生成CA私钥
    openssl genrsa -out ca.key 2048 //生成CA私钥
  - 生成CA根证书
    openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 -out ca.crt -subj "/C=CN/ST=Beijing/L=Beijing/O=Dev/CN=Local CA"
  - 生成服务器私钥
    openssl genrsa -out server.key 2048
  - 生成CSR（不包含SAN）
    openssl req -new -key server.key -out server.csr  -subj "/C=CN/ST=Beijing/L=Beijing/O=Dev/CN=localhost"
    - 使用CA签署，同时添加SAN
      - 创建server.cnf文件，其文件内容：
        [req]
        default_bits = 2048
        prompt = no
        default_md = sha256
        distinguished_name = dn
        req_extensions = v3_req

        [dn]
        C = CN
        ST = Beijing
        L = Beijing
        O = Dev
        CN = localhost
      
        [v3_req]
        keyUsage = keyEncipherment, digitalSignature
        extendedKeyUsage = serverAuth
        subjectAltName = @alt_names
      
        [alt_names]
        DNS.1 = localhost
        DNS.2 = *.localhost
        IP.1 = 127.0.0.1
        IP.2 = ::1
      - 执行1：
        openssl req -new -key server.key -out server.csr -config server.cnf
      - 执行2：
        openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365 -sha256 -extensions v3_req -extfile server.cnf
  - 验证证书
    openssl x509 -in server.crt -text -noout | findstr "Subject Alternative Name"
  - 添加证书
    certutil -addstore "Root" ca.crt
    

- 基于sql.DB, gorm.DB进行数据库操作，实现分离
- 设置当前工作目录为 -- D:/MyIdea/CClaw/src
- 重新生成swagger文档
  - cd D:/MyIdea/CClaw/src/agentService
  - swag fmt ## 清空缓存
  - swag init -d .,./../platformutility,./../cclawservice --parseInternal --parseDependency --parseDependency ## 生成swagger文档

- Session相关
  - 通过SessionId(string) 唯一标识
  - 比如在chat.deepseek.com或类似的聊天AI聊天平台上点击“开启新对话”或新建聊条，就表示一个新的Session开始
  - 一个Session下会有聊天记录，通过聊天历史进行管理
  
- 聊天历史
  - 有很多message构成，每个message通过messageId进行标识
  - 每个message有方向，即用户的问题，与AI的回复

## 2 本系统agent设计机制
- 确定系统的工作目录，skill目录，session目录，memory目录等
- skill的目录应该有系统的skill目录，个人的skill目录，项目的skill目录三种；
  - 个人的skill目录应该是：workspace/{person_}/{agent}/skill
  - 项目的skill：workspace/{project_name}/{agent}/skill
- session 目录依赖于skill目录，即/workspace/skill/{session_name}/***
- memory目录依赖于session:workspace/skill/{session_name}/{memory_date}

## 3 Agent启动文件读取的时机和机制
### 1. 应用启动时（预热,首次读）
AGENTS.md,SOUL.md等启动文件的真正读取的点是PromptRepository.LoadWorkspace() -> 内部循环 -> readAgentDocumentLocked()。读取启动
文件有两个触发时机，且因为指纹缓存，绝大多数情况下并不会真的读盘。即进程启动时，应该将default agent给读取进来

### 2. 每次组装时
这里才是读取agent启动文件的主路径，且按需读取。PromptService.BuildSystemPromptEx()第一步：
  if err := service.promptRepo.LoadWorkspace(workspaceRoot)
这才是业务的主入 -- 当一次会话需要构建system prompt时，才会走到这里。其函数LoadWorkspace()函数有一道短路行为，基本上直接读取缓存即可实现

这里的短路行为是通过对agent的启动文件的进行修改指纹计算（即size+mtime）对7个启动文件进行计算，只要有一个修改了以及换了工作区，就重新读取

### 3.读取SKILL.md
读取SKILL.md的时机与agent启动文件是不同的，SKILL.md只是在每次BuildSystemPromptEx()都会ClearSkills后重新读取；由于skill文件路径由调用方
作为参数传入，由于数量较少，直接读取即可

### 4. 压缩机制
放入到system prompt中并非md文件的所有内容，AGENTS.md/SOUL.md等启动文件进入system prompt前，会经过一条“受控快照”处理管线：
  - 剥离front matter
    在md文件中以"---"包裹的部分为metadata，不会进入到prompt，metadata部分是用来与用户的意图进行判断使用的
  - 按文件预算截断
    单文件默认上限：bootstrap.md上限为20,000字节；user.md上限为4,000字节
    超限的话用trimBootstrapContent截断，并留下可读标记让模型知道内容不完整
    AGENTS.md有专门的trimAgentBootstrapContent（生成policy digest摘要）
  - 按总量预算压缩（最重要）
    
     不是每个文件各自为政，而是按顺序累加一个总盘子：
     const totalMaxChars = opts?.totalMaxChars ?? DEFAULT_BOOTSTRAP_TOTAL_MAX_CHARS; // 60000
     let remainingTotalChars = totalMaxChars;
     for (const file of files) {
     if (remainingTotalChars <= 0) break;          // 预算耗尽，后面文件直接不注入
     const fileMaxChars = Math.min(fileBudget, remainingTotalChars);
     ...
     }
     
    raw = 磁盘原文的长度，injected = 实际进入到prompt的长度；只有文件足够小、总量够用时两者才想等。
  - 缺失/空白处理
    - 空白或纯空白内容 -> 整个跳过 （skips empty or whitespace-only content）
    - 必须文件缺失- > [MISSING] Expected at: /path/AGENTS.md标记
  
  - 按会话范围过滤
  - 最后以阅读指引的Project Context节注入

### 5. 注入到system prompt后不是干放着，还带语义指引
  buildProjectContextSection渲染为：
  ines.push("Loaded project context:");
  if (hasSoulFile) lines.push("SOUL.md: persona/tone. Follow it unless higher-priority instructions override.");
  if (hasMemoryFile) lines.push("MEMORY.md: durable non-profile facts and decisions; use when relevant...");
  if (hasUserFile) lines.push("USER.md: durable user preferences and profile directives; follow unless higher-priority instructions override.");
  for (const file of params.files) lines.push(`## ${file.path}`, "", sanitizeContextFileContentForPrompt(file.content), "");

即每个文件配一句“怎么用”的引导，且截断时注入：
  [Bootstrap truncation warning]
  Some workspace bootstrap files were truncated before Project Context injection.
  Treat Project Context as partial and read the relevant files directly if details seem missing.
明确告诉模型“这是部分快照，细节缺失要用read工具读取原文件“。

### 6. 总结
  AGENTS.md/SOUL.md等并不是挣分md原文进入到system prompt，而是经过“剥离front matter -> 每文件 20K/USER 4K -> 总量 60K预算截断 -> 缺失/空白处理
  -> 按会话处理范围过滤”后的受控快照；切在OpenClaw/Codex里还会进一步按文件语义分流（规则->Project Context、人格->指令通道、记忆->工具、技能->目录）,
  并附上截断警告与“怎么用这份文件”的阅读指引。

  而在当前项目中实现的：
  SplitFrontMatter只注入Content正文中
  MaxBootstrapFileChars = 20000 / MaxUserDocChars = 4000
  MaxBootstrapTotalChars = 60000 + enforceTotalBudget
  [MISSING] Expected at: <path>
  ## Bootstrap Context Notice 
  isKindAllowedInScope（subagent仅AGENTS、corn无MEMORY)
  skill只注入metadata

## 4. front matter机制
  front matter的本质：它是可选的、用来把metadata和正文给分割开来，而不是进入到system prompt的闸门。若无front matter判断机制则非常简单
  front matter只是负责切分，并不负责把关

### 1 判断正文是什么 <- front matter只影响这一步
  首行是"---" 且有闭合的"---"行，有front matter， "---"包裹的之间的YAML元数据剥离，只取后面的正文 进入到prompt的正文
  首行不是"---"则就是普通的md,无front matter，整个md就是正文，进入到prompt正文的候选区
  首行是"---"无闭合的"---"，视为普通的md,整个md就是正文,进度到prompt正文的候选区，不判断为front matter
### 2 正文能否进入/进入多少到system prompt <- 由预算/空白/缺失/范围决定，与front matter无关
  最终决定哪些内容进步到system prompt中的判断依据是：
  - 有front matter的就取body部分，没有的就取整个文件内容
  - 然后经过：空白->预算->范围-> 三道过滤，剩下的才是真正放入到system prompt中的内容

对于启动文件来说，没有front matter时整个文件内容放入到system prompt反而是期望行为，因为AGENTS.md/SOUL.md本质就是“要喂给模型的指令”，用户写的
多长、写什么，就该让模型看到什么（只要不超预算）。



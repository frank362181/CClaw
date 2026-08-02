
# AI Chat · 智能对话助手

一个轻量级、模块化的 AI 聊天 Web 应用，界面风格与 Kimi 保持一致，支持流式对话输出、多会话管理和登录鉴权，纯前端 HTML + CSS + JavaScript 实现，易于集成至任意后端服务。

## ✨ 功能特性

- **登录认证**：提供登录界面，支持用户名/密码验证（演示账号 `demo/123456`），状态持久化存储。
- **多会话管理**：左侧边栏展示历史对话列表，支持新建、切换、删除对话。
- **流式聊天**：AI 回复以逐字/逐词方式流式输出，模拟真实打字机效果。
- **消息管理**：用户与 AI 消息以不同气泡样式区分，支持自动滚动、时间戳显示。
- **移动端适配**：侧边栏在手机端自动折叠为抽屉菜单，触摸操作友好。
- **模块化架构**：代码按功能分层（UI / 状态 / API / 流式引擎 / 认证），易于维护和扩展。

## 工程结构

    /chat-app/
    ├── index.html # 应用外壳：仅声明 <login-page> / <chat-page> 组件挂载点
    ├── README.md # 项目文档 (当前文件)
    ├── config/
    │ └── env.js # 环境配置 (API地址/Mock开关)
    ├── src/
    │ ├── styles/
    │ │ └── main.css # 全局样式 (Kimi 风格，通过组件 Shadow DOM 引入)
    │ ├─ scripts/
    │ │ ├── main.js # 应用编排层：挂载组件、注入回调、路由切换、全局事件
    │ │ ├── modules/
    │ │ │ ├── component.js # 组件加载器 (fetch 视图 + Shadow DOM 隔离)
    │ │ │ ├── config.js # 应用配置 (导入 env.js)
    │ │ │ ├── state.js # 状态管理 (localStorage 持久化，纯逻辑)
    │ │ │ ├── api.js # API 接口层 (预留 RESTful 调用位置，纯逻辑)
    │ │ │ ├── login.js # 登录页组件控制器 (LoginPage)
    │ │ │ ├── chat.js # 聊天页组件控制器 (ChatPage)
    │ │ │ ├── auth.js # 认证管理 (登录/登出，协调两个组件)
    │ │ │ ├── stream.js # 流式输出引擎 (依赖 ChatPage)
    │ │ │ └── ui.js # [已废弃] 原 UI 层，已被 login.js / chat.js 替代
    │ │ └── utils/
    │ │   └── helper.js # 工具函数 (ID生成/时间格式化等)
    │ └── pages/
    │   ├── login.html # 登录页组件视图 (纯 HTML 片段)
    │   └── chat.html # 聊天页组件视图 (纯 HTML 片段)
    └── .gitignore # (建议) Git 忽略文件

## 组件化设计（高内聚 · 低耦合）

- **视图与逻辑分离**：每个页面组件 = 视图片段（`pages/*.html`，纯 HTML）+ 控制器
  （`scripts/modules/*.js`，对外仅暴露公开方法，内部 DOM 通过 Shadow DOM 隔离）。
- **`index.html` 只做外壳**：不包含任何页面业务结构，仅声明组件挂载点，由
  `component.js` 动态加载并注入 Shadow DOM（支持 `file://` 与 HTTP 两种打开方式）。
- **交互解耦**：组件通过回调（`onSend` / `onSubmit` 等）与外部协作，会话切换/删除
  通过 `document` 自定义事件广播；`main.js` 作为唯一编排层串联各组件。
- **状态与接口独立**：`state.js` / `api.js` / `config.js` 为纯逻辑模块，不依赖任何组件。

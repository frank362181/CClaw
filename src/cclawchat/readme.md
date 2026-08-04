# CClaw Chat

资产管理智能助手的原生 Web 前端。项目使用 HTML、CSS、ES Modules 和 Shadow DOM，无编译步骤。

## 运行

必须通过 HTTP(S) 静态服务器运行，不能直接使用 `file://` 打开，因为页面组件通过 `fetch()` 加载。

```powershell
python -m http.server 8080
```

然后访问 `http://localhost:8080/`。后端地址和运行模式在 `config/env.js` 中配置。

## 配置

- `API_BASE_URL`：后端 API 地址。
- `ENABLE_MOCK`：`true` 使用本地模拟回复；`false` 调用 `/chat` 流式接口。
- `CHAT_STREAM_FORMAT`：真实聊天响应格式，支持 `sse` 或 `text`。
- `SESSION_CHECK_PATH`：恢复页面会话时用于验证令牌的受保护只读接口。
- `REQUEST_TIMEOUT`：Axios 普通请求超时。

登录和退出始终调用真实后端。聊天请求在非 Mock 模式下发送与后端 `ChatRequest` 对应的 JSON。

## 结构

```text
cclawchat/
├── config/env.js
├── pages/                 # Shadow DOM 视图片段
├── scripts/main.js        # 应用编排和聊天抽屉
├── scripts/modules/       # API、认证、状态、流式引擎和页面控制器
├── scripts/utils/
├── styles/main.css
├── test/                  # Node 内置测试
└── index.html
```

## 状态与认证

- 登录令牌只保存在 `sessionStorage`，关闭标签页后失效；后端仍必须验证每个受保护请求。
- 页面刷新恢复会话时，会调用 `SESSION_CHECK_PATH` 验证令牌。
- 对话历史按规范化用户名分别保存在 `localStorage`，登出后内存中的对话立即清空。
- 每个对话最多保留 200 条消息，流式输出只在结束、取消或失败时持久化。

当前后端使用 `AUTHORIZATION` 请求头传递 token，前端请求不携带 Cookie。生产环境如要改用 `HttpOnly`、`Secure`、`SameSite` Cookie，需要同步调整后端 CORS 与会话实现，并通过 HTTP 响应头配置 CSP、HSTS 和 `frame-ancestors`。

## 检查

需要 Node.js 22 或更高版本：

```powershell
npm test
npm run check
```

Axios 1.7.9 当前作为本地 UMD 文件放在 `scripts/modules/vendor/`。升级时应同时更新此处的版本记录，并核对来源和许可证。

/**
 * ============================================================
 * 根据部署环境修改 NODE_ENV 和 API_BASE_URL
 * ============================================================
 */

export const ENV = {
    APP_TITLE : '资产管理智能助手', //在浏览器上的标题栏显示的应用名称
    
    // 环境标识: 'development' | 'production' | 'test'
    NODE_ENV: 'development',

    // 后端 API 基础地址 (替换为真实地址)
    API_BASE_URL: 'https://localhost:8443/api',

    // 应用版本
    APP_VERSION: '1.0.0',

    // 是否启用 Mock 数据 (开发调试用)
    // true: 使用模拟流式回复 (无需后端)
    // false: 调用真实后端接口
    ENABLE_MOCK: false,

    // 预留:LLM 流式引擎接入后的聊天返回格式: 'sse' | 'text'（当前真实聊天为一次性 JSON 应答）
    CHAT_STREAM_FORMAT: 'sse',

    // 使用一个受保护的只读接口验证恢复的会话
    SESSION_CHECK_PATH: '/getsessionlist',

    // 请求超时时间 (毫秒)
    REQUEST_TIMEOUT: 30000,

    // 是否开启调试日志
    DEBUG: true,
};

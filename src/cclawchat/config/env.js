/**
 * ============================================================
 * 根据部署环境修改 NODE_ENV 和 API_BASE_URL
 * ============================================================
 */

export const ENV = {
    // 环境标识: 'development' | 'production' | 'test'
    NODE_ENV: 'development',

    // 后端 API 基础地址 (替换为真实地址)
    API_BASE_URL: 'https://api.example.com/v1',

    // 应用版本
    APP_VERSION: '1.0.0',

    // 是否启用 Mock 数据 (开发调试用)
    // true: 使用模拟流式回复 (无需后端)
    // false: 调用真实后端接口
    ENABLE_MOCK: true,

    // 请求超时时间 (毫秒)
    REQUEST_TIMEOUT: 30000,

    // 是否开启调试日志
    DEBUG: true,
};
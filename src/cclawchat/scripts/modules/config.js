/**
 * ============================================================
 * 应用配置
 * ============================================================
 */

import { ENV } from '../../config/env.js?v=1.0.4';

export const CONFIG = {
    // 应用基础信息
    APP_NAME: 'CClaw Chat',
    VERSION: ENV.APP_VERSION,

    // API 配置
    API_BASE: ENV.API_BASE_URL,
    TIMEOUT: ENV.REQUEST_TIMEOUT,

    // 存储键名
    STORAGE_KEY: 'ai_chat_data',
    USER_KEY: 'ai_chat_user',

    // 业务限制
    MAX_MESSAGES_PER_CONV: 200,

    // 流式模拟延迟范围 (ms) —— 仅当 ENABLE_MOCK = true 时生效
    STREAM_DELAY_MIN: 15,
    STREAM_DELAY_MAX: 45,

    // 默认用户名
    DEFAULT_USERNAME: '用户',

    // 运行时标志 (从环境变量派生)
    IS_DEV: ENV.NODE_ENV === 'development',
    IS_PROD: ENV.NODE_ENV === 'production',
    ENABLE_MOCK: ENV.ENABLE_MOCK,
    DEBUG: ENV.DEBUG,
};
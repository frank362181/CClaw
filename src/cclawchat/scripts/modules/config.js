/**
 * ============================================================
 * 应用配置
 * ============================================================
 */

import { ENV } from '../../config/env.js';

export const CONFIG = {
    // 应用基础信息
    APP_NAME: ENV.APP_TITLE,
    VERSION: ENV.APP_VERSION,

    // API 配置
    API_BASE: ENV.API_BASE_URL,
    TIMEOUT: ENV.REQUEST_TIMEOUT,

    // 签名密钥（HMAC-SHA256，须与后端 signKey 一致；纯浏览器下非保密，仅防随意篡改）
    SIGN_KEY: '123456',

    // 存储键名
    STORAGE_KEY_PREFIX: 'cclaw_chat_data:',
    USER_KEY: 'cclaw_chat_session',

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
    CHAT_STREAM_FORMAT: ENV.CHAT_STREAM_FORMAT || 'sse',
    SESSION_CHECK_PATH: ENV.SESSION_CHECK_PATH || '/getsessionlist',
    DEBUG: ENV.DEBUG,
};

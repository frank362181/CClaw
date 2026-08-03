/**
 * ============================================================
 * HTTP 客户端包（基于 axios）— 全局可复用的 http 实例
 * ============================================================
 * 定位类似 config/env.js：任意模块 import 即可全局使用，无需重复配置。
 *
 * 说明：
 *  - axios 库本体由 index.html 以 <script> 通过 UMD 全局引入（本地 vendor，
 *    挂载到 window.axios），本包在其之上只做一次实例化与全局拦截器配置。
 *  - 导出：
 *      axios —— 原始 axios（需要自定义新实例等场景时使用）
 *      http  —— 已配置的统一实例（业务模块推荐直接使用）
 *  - 后端鉴权参数（TIMESTAMP 请求头 + NONCE/TIMESTAMP/SIGNATURE querystring）
 *    由请求拦截器自动注入；响应统一解包 —— 业务模块不感知这些细节。
 */

import { CONFIG } from './config.js';
import { USER_DEFINED_HEADER, USER_DEFINED_QUERYSTRING } from './config.js';
import { getRandomNonce } from '../utils/helper.js';

// 原始 axios 库（全局 UMD；若未来改为打包/ESM 方式，可换成 import axios from 'axios'）
export const axios = window.axios;

// 统一 axios 实例：收敛 baseURL / 超时 / 默认请求头
export const http = axios.create({
    baseURL: CONFIG.API_BASE,
    timeout: CONFIG.TIMEOUT,
    headers: { 'Content-Type': 'application/json' },
});

// 全局默认签名：默认占位 '1'，可用 setSignature() 修改。
// 单次请求可用 axios 配置中的 signature 字段覆盖（优先级更高）。
let currentSignature = '1';

/**
 * 设置全局默认签名，用于填充 querystring 的 SIGNATURE。
 * @param {string|number} signature 请求内容的签名值
 */
export function setSignature(signature) {
    currentSignature = signature != null ? String(signature) : '1';
}

// 请求拦截器：自动注入后端约定的 时间戳 / 随机盐 / 签名
http.interceptors.request.use((config) => {
    const timestamp = String(Math.floor(Date.now() / 1000)); // 当前时间（秒）
    const nonce = getRandomNonce();   // 随机盐

    // 请求头：TIMESTAMP
    config.headers[USER_DEFINED_HEADER.HEADER_NAME_TIMESTAMP] = timestamp;

    // 签名取值优先级：单请求 config.signature > 全局默认 currentSignature
    const signature = config.signature != null ? String(config.signature) : currentSignature;
    delete config.signature; // 仅为填充用，不随请求发送

    // querystring：NONCE + TIMESTAMP + SIGNATURE
    config.params = {
        [USER_DEFINED_QUERYSTRING.QUERYSTRING_NAME_NONCE]: nonce,
        [USER_DEFINED_QUERYSTRING.QUERYSTRING_NAME_TIMESTAMP]: timestamp,
        [USER_DEFINED_QUERYSTRING.QUERYSTRING_NAME_SIGNATURE]: signature,
        ...config.params,
    };
    return config;
});

// 响应拦截器：统一解包业务 JSON；网络 / HTTP 错误转为友好中文提示
http.interceptors.response.use(
    (response) => response.data,
    (error) => {
        const prefix = error.response
            ? `请求失败 (HTTP ${error.response.status})`
            : '网络请求失败，无法连接服务器';
        return Promise.reject(new Error(`${prefix}：${error.message || ''}`));
    }
);

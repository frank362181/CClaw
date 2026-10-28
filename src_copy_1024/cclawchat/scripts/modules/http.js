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
 *  - 响应统一解包；认证头由需要鉴权的业务接口显式传入。
 *  - 不在浏览器中伪造请求签名；服务端认证依赖会话令牌。
 */

import { CONFIG } from './config.js';

// 原始 axios 库（全局 UMD；若未来改为打包/ESM 方式，可换成 import axios from 'axios'）
export const axios = window.axios;

// 统一 axios 实例：收敛 baseURL / 超时 / 默认请求头
export const http = axios.create({
    baseURL: CONFIG.API_BASE,
    timeout: CONFIG.TIMEOUT,
    headers: { 'Content-Type': 'application/json' },
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

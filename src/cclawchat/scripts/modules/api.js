import { randomDelay,getRandomNonce } from '../utils/helper.js';
import {HmacSHA256Hex} from '../utils/hasher.js'
import { CONFIG } from './config.js';
import { http } from './http.js';

const MOCK_REPLIES = {
    greeting: '你好！很高兴见到你 👋 有什么我可以帮你的吗？',
    help: '当然可以！我可以帮你解答问题、提供建议、或者和你闲聊。告诉我你想聊什么吧～',
};

function selectMockReply(content) {
    const lower = content.toLowerCase();
    if (content.includes('你好') || lower.includes('hi') || lower.includes('hello')) return MOCK_REPLIES.greeting;
    if (content.includes('帮助') || lower.includes('help')) return MOCK_REPLIES.help;
    return `我明白了。关于「${content}」，建议先明确目标和约束，再逐步验证可行方案。`;
}

async function runMockStream(content, signal, onChunk, onComplete) {
    let current = '';
    for (const char of selectMockReply(content)) {
        if (signal.aborted) return;
        await new Promise(resolve => setTimeout(resolve, randomDelay(CONFIG.STREAM_DELAY_MIN, CONFIG.STREAM_DELAY_MAX)));
        if (signal.aborted) return;
        current += char;
        onChunk(current);
    }
    if (!signal.aborted) onComplete(current);
}

async function runRealStream(convId, content, token, signal, onChunk, onComplete) {
    const response = await fetch(`${CONFIG.API_BASE}/chat`, {
        method: 'POST',
        signal,
        credentials: 'omit',
        headers: {
            'Content-Type': 'application/json',
            'Accept': CONFIG.CHAT_STREAM_FORMAT === 'sse' ? 'text/event-stream' : 'text/plain',
            ...(token ? { AUTHORIZATION: token } : {}),
        },
        body: JSON.stringify({ request_msg: content, session_id: convId, chat_type: 0, chat_channel: 1 }),
    });
    if (!response.ok) throw new Error(`聊天请求失败 (HTTP ${response.status})`);
    if (!response.body) throw new Error('服务器未返回可读取的响应流');

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let fullContent = '';
    let pending = '';
    const consumeSseEvent = event => {
        const data = event.split(/\r?\n/)
            .filter(line => line.startsWith('data:'))
            .map(line => line.slice(5).trimStart())
            .join('\n');
        if (!data || data === '[DONE]') return;
        let chunk = data;
        try {
            const parsed = JSON.parse(data);
            chunk = parsed.content ?? parsed.delta ?? parsed.data?.content ?? '';
        } catch { /* plain SSE data */ }
        fullContent += chunk;
        onChunk(fullContent);
    };
    while (!signal.aborted) {
        const { value, done } = await reader.read();
        if (done) break;
        pending += decoder.decode(value, { stream: true });
        if (CONFIG.CHAT_STREAM_FORMAT === 'sse') {
            const events = pending.split(/\r?\n\r?\n/);
            pending = events.pop() || '';
            events.forEach(consumeSseEvent);
        } else {
            fullContent += pending;
            pending = '';
            onChunk(fullContent);
        }
    }
    pending += decoder.decode();
    if (CONFIG.CHAT_STREAM_FORMAT === 'sse' && pending.trim()) consumeSseEvent(pending);
    if (CONFIG.CHAT_STREAM_FORMAT === 'text' && pending) {
        fullContent += pending;
        onChunk(fullContent);
    }
    if (!signal.aborted) onComplete(fullContent);
}

export const API = {
    sendMessageStream(convId, content, token, onChunk, onComplete, onError) {
        const controller = new AbortController();
        const task = CONFIG.ENABLE_MOCK
            ? runMockStream(content, controller.signal, onChunk, onComplete)
            : runRealStream(convId, content, token, controller.signal, onChunk, onComplete);
        task.catch(error => {
            if (error.name !== 'AbortError' && !controller.signal.aborted) onError?.(error);
        });
        return controller;
    },

    async login(username, password) {
        try {
            // 请求体：签名基于实际发送的 JSON 字符串计算
            const body = { username: username.trim(), password };
            // TIMESTAMP = 当前秒数；NONCE = 随机数值
            const TIMESTAMP = String(Math.floor(Date.now() / 1000));
            const NONCE = getRandomNonce();
            // SIGNATURE = HmacSHA256Hex(请求body字符串 + TIMESTAMP + NONCE)
            const SIGNATURE = await HmacSHA256Hex(CONFIG.SIGN_KEY, JSON.stringify(body) + TIMESTAMP + NONCE);

            const json = await http.post('/login', body, {
                params: { TIMESTAMP, NONCE, SIGNATURE },
            });
            // 后端 CommonResponse 字段为 respCode/respMsg（camelCase），勿写成 resp_code
            if (json.respCode !== 0 || !json.data) throw new Error(json.respMsg || '用户名或密码错误');
            // 后端 LoginSession 用户名字段为 loginName（非 userName）、签发时间为 issuedAt（非 genAt）
            const userName = json.data.loginName || username.trim();
            const generatedAt = Number(json.data.issuedAt) || Math.floor(Date.now() / 1000);
            const lifetime = Number(json.data.expiredLength) || 0;
            return {
                success: true,
                user: { id: userName, userName, displayName: json.data.displayName || userName },
                token: json.data.token,
                expiresAt: lifetime > 0 ? (generatedAt + lifetime) * 1000 : null,
            };
        } catch (error) {
            throw new Error(error.message || '登录失败，请重试');
        }
    },

    async validateSession(token) {
        if (!token) return false;
        const json = await http.get(CONFIG.SESSION_CHECK_PATH, { headers: { AUTHORIZATION: token } });
        if (json && typeof json.respCode === 'number' && json.respCode !== 0) {
            throw new Error(json.respMsg || '登录会话已失效');
        }
        return true;
    },

    async logout(token) {
        await http.post('/logout', null, { headers: { AUTHORIZATION: token } });
        return { success: true };
    },

    /**
     * 获取当前登录用户的 Agents（智能体）列表。
     * - 后端通过 token 识别当前用户（owner），无需前端传用户标识。
     * - 遵循项目安全要求：GET 请求同样携带 TIMESTAMP / NONCE / SIGNATURE（防重放）。
     * @param {string} token 登录令牌（AUTHORIZATION 请求头）
     * @returns {Promise<Array<object>>} 后端返回的 Agent 原始对象数组
     */
    async getAgents(token) {
        if (!token) throw new Error('未登录，无法获取智能体列表');
        // 签名基于「请求内容 + TIMESTAMP + NONCE」；GET 无请求体，内容为空串
        const TIMESTAMP = String(Math.floor(Date.now() / 1000));
        const NONCE = getRandomNonce();
        const SIGNATURE = await HmacSHA256Hex(CONFIG.SIGN_KEY, '' + TIMESTAMP + NONCE);

        const json = await http.get('/agents', {
            params: { TIMESTAMP, NONCE, SIGNATURE },
            headers: { AUTHORIZATION: token },
        });
        // 后端 CommonResponse 字段为 respCode/respMsg（camelCase）
        if (json && typeof json.respCode === 'number' && json.respCode !== 0) {
            throw new Error(json.respMsg || '获取智能体列表失败');
        }
        return Array.isArray(json?.data) ? json.data : [];
    },

    /**
     * 获取当前登录用户的 Skills（技能）列表（对应后端 GetSkills）。
     * - 后端通过 token 识别当前用户（owner），无需前端传用户标识。
     * - 遵循项目安全要求：GET 请求同样携带 TIMESTAMP / NONCE / SIGNATURE（防重放）。
     * @param {string} token 登录令牌（AUTHORIZATION 请求头）
     * @returns {Promise<Array<object>>} 后端返回的 Skill 原始对象数组
     */
    async getSkills(token) {
        if (!token) throw new Error('未登录，无法获取技能列表');
        // 签名基于「请求内容 + TIMESTAMP + NONCE」；GET 无请求体，内容为空串
        const TIMESTAMP = String(Math.floor(Date.now() / 1000));
        const NONCE = getRandomNonce();
        const SIGNATURE = await HmacSHA256Hex(CONFIG.SIGN_KEY, '' + TIMESTAMP + NONCE);

        const json = await http.get('/skills', {
            params: { TIMESTAMP, NONCE, SIGNATURE },
            headers: { AUTHORIZATION: token },
        });
        // 后端 CommonResponse 字段为 respCode/respMsg（camelCase）
        if (json && typeof json.respCode === 'number' && json.respCode !== 0) {
            throw new Error(json.respMsg || '获取技能列表失败');
        }
        return Array.isArray(json?.data) ? json.data : [];
    },

    /**
     * 更新当前登录用户的一个 Agent（后端乐观锁，body 需携带 dVersion）。
     * - 遵循项目安全要求：POST 携带 TIMESTAMP / NONCE / SIGNATURE，认证头 AUTHORIZATION。
     * @param {string} token 登录令牌（AUTHORIZATION 请求头）
     * @param {object} agent TAgent 对象（含 dVersion 乐观锁）
     * @returns {Promise<object>} 后端 CommonResponse
     */
    async updateAgent(token, agent) {
        if (!token) throw new Error('未登录，无法保存智能体');
        // 签名基于「请求内容 + TIMESTAMP + NONCE」；内容为实际发送的 JSON 字符串
        const body = JSON.stringify(agent);
        const TIMESTAMP = String(Math.floor(Date.now() / 1000));
        const NONCE = getRandomNonce();
        const SIGNATURE = await HmacSHA256Hex(CONFIG.SIGN_KEY, body + TIMESTAMP + NONCE);

        const json = await http.post('/agent/update', agent, {
            params: { TIMESTAMP, NONCE, SIGNATURE },
            headers: { AUTHORIZATION: token },
        });
        if (json && typeof json.respCode === 'number' && json.respCode !== 0) {
            throw new Error(json.respMsg || '保存智能体失败');
        }
        return json;
    },

    /**
     * 获取指定 Agent 的启动文件清单（仅元数据，不含文件内容）。
     * - 遵循项目安全要求：POST 携带 TIMESTAMP / NONCE / SIGNATURE，认证头 AUTHORIZATION。
     * @param {string} token 登录令牌（AUTHORIZATION 请求头）
     * @param {string} agentId 智能体 ID
     * @returns {Promise<Array<object>>} 启动文件列表（AgentDocument 数组）
     */
    async getStartupFiles(token, agentId) {
        if (!token) throw new Error('未登录，无法获取启动文件');
        const body = JSON.stringify({ agentId });
        const TIMESTAMP = String(Math.floor(Date.now() / 1000));
        const NONCE = getRandomNonce();
        const SIGNATURE = await HmacSHA256Hex(CONFIG.SIGN_KEY, body + TIMESTAMP + NONCE);

        const json = await http.post('/agent/startupFiles', { agentId }, {
            params: { TIMESTAMP, NONCE, SIGNATURE },
            headers: { AUTHORIZATION: token },
        });
        if (json && typeof json.respCode === 'number' && json.respCode !== 0) {
            throw new Error(json.respMsg || '获取启动文件失败');
        }
        return Array.isArray(json?.data) ? json.data : [];
    },

    /**
     * 获取指定 Agent 指定启动文件的内容（文件名须命中后端启动文件白名单）。
     * - 遵循项目安全要求：POST 携带 TIMESTAMP / NONCE / SIGNATURE，认证头 AUTHORIZATION。
     * @param {string} token 登录令牌（AUTHORIZATION 请求头）
     * @param {string} agentId 智能体 ID
     * @param {string} fileName 启动文件名（如 AGENTS.md）
     * @returns {Promise<object>} 启动文件对象（AgentDocument，含 content）
     */
    async getStartupFile(token, agentId, fileName) {
        if (!token) throw new Error('未登录，无法获取启动文件');
        const body = JSON.stringify({ agentId, fileName });
        const TIMESTAMP = String(Math.floor(Date.now() / 1000));
        const NONCE = getRandomNonce();
        const SIGNATURE = await HmacSHA256Hex(CONFIG.SIGN_KEY, body + TIMESTAMP + NONCE);

        const json = await http.post('/agent/startupFile', { agentId, fileName }, {
            params: { TIMESTAMP, NONCE, SIGNATURE },
            headers: { AUTHORIZATION: token },
        });
        if (json && typeof json.respCode === 'number' && json.respCode !== 0) {
            throw new Error(json.respMsg || '获取启动文件内容失败');
        }
        return json?.data || null;
    },

};

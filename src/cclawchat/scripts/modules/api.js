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

async function runMockStream(content, signal, onChunk, onComplete, onStatus) {
    onStatus?.({ status: 'thinking', text: '思考中…' });
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

/**
 * 真实聊天请求（同步模式）：POST /api/chat。
 * - 与 GetAgents/GetSkills 等受保护接口一致，携带 TIMESTAMP/NONCE/SIGNATURE（防重放），
 *   并附 AUTHORIZATION 令牌，故会经过后端全局 filter 与认证 interceptor。
 * - 请求体字段与后端 model.ChatRequest 的 json 标签（camelCase）对齐。
 * - 一次性返回 JSON 应答（ChatResponse.reply）；默认走流式（runRealChatStream），
 *   当 CHAT_STREAM_FORMAT 非 'sse' 时作为回退路径使用。
 */
async function runRealChat(convId, content, token, signal, onChunk, onComplete) {
    const body = {
        sessionId: convId,
        requestMsg: content,
        chatType: 0,    // 私聊
        chatChannel: 1, // WEB 端
    };
    // 签名基于实际发送的 JSON 字符串计算（与 login 一致）
    const TIMESTAMP = String(Math.floor(Date.now() / 1000));
    const NONCE = getRandomNonce();
    const SIGNATURE = await HmacSHA256Hex(CONFIG.SIGN_KEY, JSON.stringify(body) + TIMESTAMP + NONCE);

    const json = await http.post('/chat', body, {
        params: { TIMESTAMP, NONCE, SIGNATURE },
        headers: { AUTHORIZATION: token },
        signal,
    });
    // 后端 CommonResponse 字段为 respCode/respMsg（camelCase）
    if (json && typeof json.respCode === 'number' && json.respCode !== 0) {
        throw new Error(json.respMsg || '聊天请求失败');
    }
    const reply = json?.data?.reply || '';
    onChunk(reply);
    onComplete(reply);
}

/**
 * 真实流式聊天请求：POST /api/chat/stream（SSE）。
 * - 通过原生 fetch 读取 response.body 流并解析 SSE 事件：
 *     data: {"status":"...","text":"..."} → Agent 阶段状态，回调 onStatus(内容开始前)
 *     data: {"reply":"..."}   → 增量文本，累加后回调 onChunk(累计内容)
 *     data: {"delta":"..."}   → 思考过程增量，回调 onReasoning
 *     data: {phase,seq,name}  → 工具调用进度，回调 onTool
 *     data: {code,msg}        → 收尾提示(truncated/content_filter/incomplete)，回调 onNotice
 *     data: {ts}              → 心跳保活，忽略
 *     data: [DONE]            → 结束标记，触发 onComplete
 *     data: {"msg":"..."}     → 后端错误事件，抛出 Error
 * - 携带 TIMESTAMP/NONCE/SIGNATURE（防重放）与 AUTHORIZATION 令牌，与登录一致。
 * - 支持 signal 取消（AbortController），取消时静默结束不触发完成回调。
 */
async function runRealChatStream(convId, content, token, signal, onChunk, onComplete, onStatus, onReasoning, onTool, onNotice) {
    const body = {
        sessionId: convId,
        requestMsg: content,
        chatType: 0,    // 私聊
        chatChannel: 1, // WEB 端
    };
    const TIMESTAMP = String(Math.floor(Date.now() / 1000));
    const NONCE = getRandomNonce();
    const SIGNATURE = await HmacSHA256Hex(CONFIG.SIGN_KEY, JSON.stringify(body) + TIMESTAMP + NONCE);

    const resp = await fetch(
        `${CONFIG.API_BASE}/chat/stream?TIMESTAMP=${encodeURIComponent(TIMESTAMP)}&NONCE=${encodeURIComponent(NONCE)}&SIGNATURE=${encodeURIComponent(SIGNATURE)}`,
        {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Accept': 'text/event-stream',
                'AUTHORIZATION': token || '',
            },
            body: JSON.stringify(body),
            signal,
        }
    );

    if (!resp.ok) {
        let msg = `请求失败 (HTTP ${resp.status})`;
        try {
            const j = await resp.json();
            msg = j.respMsg || msg;
        } catch (_) { /* 忽略非 JSON 响应 */ }
        throw new Error(msg);
    }
    if (!resp.body) throw new Error('当前浏览器不支持流式响应');

    const reader = resp.body.getReader();
    const decoder = new TextDecoder('utf-8');
    let buffer = '';
    let accumulated = '';

    // 处理单个 data 事件；返回 true 表示收到结束标记 [DONE]
    const handleEvent = (data) => {
        if (data === '[DONE]') return true;
        let payload;
        try { payload = JSON.parse(data); } catch (_) { return false; }
        if (payload && payload.status) {
            // Agent 阶段状态（拆解任务/思考等），仅在内容开始前出现
            onStatus?.(payload);
            return false;
        }
        if (payload && typeof payload.delta === 'string' && payload.delta) {
            // 思考过程增量（reasoning_content），独立于正式回复
            onReasoning?.(payload.delta);
            return false;
        }
        if (payload && payload.interrupt !== undefined) {
            // 服务端主动中断（用户点击中断按钮，思考/推理阶段尚无正式回复）：由读取循环统一收尾
            return true;
        }
        if (payload && payload.phase && typeof payload.name === 'string') {
            // 工具调用进度事件（intent/running/done/error），供工具卡片渲染
            onTool?.(payload);
            return false;
        }
        if (payload && typeof payload.code === 'string' && typeof payload.msg === 'string') {
            // 收尾提示（truncated/content_filter/incomplete）：独立事件，不覆盖已输出内容
            onNotice?.(payload);
            return false;
        }
        if (payload && typeof payload.reply === 'string' && payload.reply) {
            accumulated += payload.reply;
            onChunk(accumulated);
        } else if (payload && payload.msg) {
            throw new Error(payload.msg);
        }
        return false;
    };

    while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        // 按空行切分 SSE 事件块
        let sep;
        while ((sep = buffer.indexOf('\n\n')) !== -1) {
            const block = buffer.slice(0, sep);
            buffer = buffer.slice(sep + 2);
            for (const line of block.split('\n')) {
                if (!line.startsWith('data:')) continue;
                if (handleEvent(line.slice(5).trim())) {
                    onComplete(accumulated);
                    return;
                }
            }
        }
        if (signal.aborted) break;
    }

    // 客户端主动取消：不再触发完成回调
    if (signal.aborted) return;
    onComplete(accumulated);
}

export const API = {
    sendMessageStream(convId, content, token, onChunk, onComplete, onError, onStatus, onReasoning, onTool, onNotice) {
        const controller = new AbortController();
        // SSE 为默认真实路径；ENABLE_MOCK 时走模拟流；CHAT_STREAM_FORMAT 非 'sse' 时回退同步 JSON
        const useStream = CONFIG.CHAT_STREAM_FORMAT === 'sse';
        const task = CONFIG.ENABLE_MOCK
            ? runMockStream(content, controller.signal, onChunk, onComplete, onStatus)
            : useStream
                ? runRealChatStream(convId, content, token, controller.signal, onChunk, onComplete, onStatus, onReasoning, onTool, onNotice)
                : runRealChat(convId, content, token, controller.signal, onChunk, onComplete);
        task.catch(error => {
            if (error.name !== 'AbortError' && !controller.signal.aborted) onError?.(error);
        });
        return controller;
    },

    /**
     * 中断指定会话进行中的流式聊天：POST /api/chat/interrupt。
     * - 携带 TIMESTAMP / NONCE / SIGNATURE（防重放）与 AUTHORIZATION 令牌，与聊天一致。
     * - 仅在聊天已开始（流式进行中）时后端支持；会话未在聊天时返回业务错误。
     * - 请求超时缩短（不受全局 120s 影响），保证中断操作尽快返回。
     * @param {string} convId 会话标识（对应发起聊天时的 sessionId）
     * @param {string} token 登录令牌（AUTHORIZATION 请求头）
     */
    async interruptChat(convId, token) {
        if (!convId) throw new Error('缺少会话标识，无法中断');
        const body = { sessionId: convId };
        const TIMESTAMP = String(Math.floor(Date.now() / 1000));
        const NONCE = getRandomNonce();
        const SIGNATURE = await HmacSHA256Hex(CONFIG.SIGN_KEY, JSON.stringify(body) + TIMESTAMP + NONCE);
        const json = await http.post('/chat/interrupt', body, {
            params: { TIMESTAMP, NONCE, SIGNATURE },
            headers: { AUTHORIZATION: token || '' },
            timeout: 5000,
        });
        // 后端 CommonResponse 字段为 respCode/respMsg（camelCase）
        if (json && typeof json.respCode === 'number' && json.respCode !== 0) {
            throw new Error(json.respMsg || '中断请求失败');
        }
        return json;
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

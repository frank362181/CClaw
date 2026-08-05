import { randomDelay,getRandomNonce } from '../utils/helper.js';
import { md5 } from '../utils/md5.js';
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
            // SIGNATURE = MD5(请求body字符串 + TIMESTAMP + NONCE)
            const SIGNATURE = md5(JSON.stringify(body) + TIMESTAMP + NONCE);

            const json = await http.post('/login', body, {
                params: { TIMESTAMP, NONCE, SIGNATURE },
            });
            if (json.resp_code !== 0 || !json.data) throw new Error(json.resp_msg || '用户名或密码错误');
            const userName = json.data.userName || username.trim();
            const generatedAt = Number(json.data.genAt) || Math.floor(Date.now() / 1000);
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
        if (json && typeof json.resp_code === 'number' && json.resp_code !== 0) {
            throw new Error(json.resp_msg || '登录会话已失效');
        }
        return true;
    },

    async logout(token) {
        await http.post('/logout', null, { headers: { AUTHORIZATION: token } });
        return { success: true };
    },
};

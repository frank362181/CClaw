/**
 * ============================================================
 * API 接口层 — 预留后端 RESTful 接口
 * ============================================================
 */

import { CONFIG } from './config.js?v=1.0.4';
import { genId, randomDelay } from '../utils/helper.js?v=1.0.4';

export const API = {
    /**
     * 发送消息并获取流式响应 (模拟实现)
     * 实际项目替换为真实 fetch 调用
     */
    sendMessageStream(convId, content, onChunk, onComplete, onError) {
        const controller = new AbortController();
        const signal = controller.signal;

        // 模拟流式响应
        (async () => {
            try {
                // 模拟 AI 回复
                let replyText = '';
                if (content.includes('你好') || content.includes('hi') || content.includes('hello')) {
                    replyText = '你好！很高兴见到你 👋 有什么我可以帮你的吗？';
                } else if (content.includes('帮助') || content.includes('help')) {
                    replyText = '当然可以！我可以帮你解答问题、提供建议、或者和你闲聊。告诉我你想聊什么吧～';
                } else if (content.includes('天气')) {
                    replyText = '天气问题需要联网查询，不过我可以给你一些通用建议：出门记得看天气预报哦！🌤️';
                } else if (content.includes('谢谢') || content.includes('感谢')) {
                    replyText = '不客气！能帮到你是我的荣幸 ❤️ 还有别的问题随时找我。';
                } else if (content.length > 20) {
                    replyText = '你提出了一个很有深度的问题 🤔 让我认真思考一下…\n\n' +
                        '从我的分析来看，这个问题涉及到多个方面。建议你从优先级最高的部分开始处理，' +
                        '然后逐步推进。如果需要更具体的建议，可以告诉我更多细节。';
                } else {
                    const templates = [
                        '我明白了。关于「' + content + '」，我的看法是…',
                        '这是个好问题！让我从几个角度来回答：',
                        '收到！我正在处理你的问题，请稍等…',
                        '有趣的问题！我建议你这样考虑：',
                    ];
                    const pick = templates[Math.floor(Math.random() * templates.length)];
                    replyText = pick + '\n\n' +
                        '1. 首先，理解问题的本质\n' +
                        '2. 然后，探索可能的方案\n' +
                        '3. 最后，选择最适合的路径\n\n' +
                        '你觉得这个思路怎么样？';
                }

                const chars = replyText.split('');
                let currentText = '';
                let isCancelled = false;
                signal.addEventListener('abort', () => { isCancelled = true; });

                for (let i = 0; i < chars.length; i++) {
                    if (isCancelled) {
                        onComplete(currentText);
                        return;
                    }
                    await new Promise(r => setTimeout(r, randomDelay(12, 40)));
                    const char = chars[i];
                    if (/[\s，。、！？；：,.]/.test(char)) {
                        await new Promise(r => setTimeout(r, 8));
                    }
                    currentText += char;
                    onChunk(currentText);
                }
                onComplete(currentText);
            } catch (err) {
                if (err.name !== 'AbortError') {
                    onError && onError(err);
                }
            }
        })();

        return controller;
    },

    /**
     * 用户登录接口
     * 调用后端 POST {API_BASE}/login（HTTPS）。
     * 按后端约定携带：
     *   - 请求头 TIMESTAMP：当前时间（秒）
     *   - 请求头 SIGNATURE：请求内容的签名（当前为占位值 1）
     *   - querystring TIMESTAMP：与请求头 TIMESTAMP 一致
     *   - querystring nonce：随机盐值
     */
    async login(username, password) {
        const url = new URL(CONFIG.API_BASE + '/login');

        // 当前时间（1970-1-1 以来的秒数）
        const timestamp = String(Math.floor(Date.now() / 1000));
        // 随机盐值（nonce）
        const nonce = genId() + '_' + Math.random().toString(36).slice(2, 12);

        // querystring：TIMESTAMP（与 header 一致）+ nonce（随机值）
        url.searchParams.set('TIMESTAMP', timestamp);
        url.searchParams.set('nonce', nonce);

        const body = JSON.stringify({ username: username.trim(), password });

        let resp;
        try {
            resp = await fetch(url.toString(), {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'TIMESTAMP': timestamp,
                    'SIGNATURE': '1', // TODO: 后续按实际签名算法基于请求内容计算
                },
                body,
            });
        } catch (err) {
            throw new Error('网络请求失败，无法连接服务器（' + err.message + '）');
        }

        let json;
        try {
            json = await resp.json();
        } catch (err) {
            throw new Error('服务器响应格式错误');
        }

        if (json.resp_code === 0 && json.data) {
            const username2 = json.data.username || username.trim();
            return {
                success: true,
                user: { id: username2, username: username2 },
                token: json.data.token,
            };
        }
        throw new Error(json.resp_msg || '用户名或密码错误');
    },

    /**
     * 【预留】登出接口
     */
    async logout(token) {
        return Promise.resolve({ success: true });
    },

    /**
     * 【预留】获取对话历史
     */
    async getConversations(token) {
        return Promise.resolve({ success: true, data: [] });
    },
};
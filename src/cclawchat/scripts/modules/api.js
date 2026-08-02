/**
 * ============================================================
 * API 接口层 — 预留后端 RESTful 接口
 * ============================================================
 */

import { CONFIG } from './config.js';
import { genId, randomDelay } from '../utils/helper.js';

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
     * 【预留】用户登录接口
     */
    async login(username, password) {
        // 模拟登录验证
        return new Promise((resolve, reject) => {
            setTimeout(() => {
                if (username && username.trim().length > 0 && password && password.length >= 4) {
                    resolve({
                        success: true,
                        user: { id: genId(), username: username.trim() },
                        token: 'mock_token_' + genId(),
                    });
                } else {
                    reject(new Error('用户名或密码错误'));
                }
            }, 600);
        });
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
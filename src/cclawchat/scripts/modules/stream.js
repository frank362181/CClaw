/**
 * ============================================================
 * 流式输出引擎
 * ============================================================
 */

import { API } from './api.js';
import { State } from './state.js';
import { ChatPage } from './chat.js';
import { genId } from '../utils/helper.js';

export const StreamEngine = {
    _controller: null,
    _active: false,

    start(convId, userContent, onComplete, onError) {
        if (this._active) {
            this.cancel();
        }

        this._active = true;
        const msgId = genId();

        // 预占位：将本条 AI 消息同步写入会话历史（与 DOM 占位共用 msgId），
        // 保证流式完成/失败后消息能正确持久化并在重渲染时保留。
        State.addMessage(convId, 'assistant', '', msgId);

        // 创建流式消息占位
        ChatPage.appendStreamingMessage(convId, msgId);

        let fullContent = '';
        let isCompleted = false;

        const handleChunk = (chunk) => {
            fullContent = chunk;
            ChatPage.updateStreamingMessage(msgId, chunk, false);
        };

        const handleComplete = (finalContent) => {
            if (isCompleted) return;
            isCompleted = true;
            this._active = false;
            this._controller = null;
            State.updateMessageContent(convId, msgId, finalContent || fullContent);
            ChatPage.updateStreamingMessage(msgId, finalContent || fullContent, true);
            // 重新渲染以保持一致性
            const conv = State.getCurrentConversation();
            if (conv) {
                ChatPage.renderMessages(conv);
            }
            ChatPage.setInputEnabled(true);
            ChatPage.updateSendButton();
            onComplete && onComplete(finalContent || fullContent);
        };

        const handleError = (err) => {
            this._active = false;
            this._controller = null;
            ChatPage.setInputEnabled(true);
            ChatPage.updateSendButton();
            const errorMsg = err?.message || '网络错误，请稍后重试';
            ChatPage.updateStreamingMessage(msgId, '⚠️ ' + errorMsg, true);
            State.updateMessageContent(convId, msgId, '⚠️ ' + errorMsg);
            onError && onError(err);
        };

        const controller = API.sendMessageStream(
            convId,
            userContent,
            handleChunk,
            handleComplete,
            handleError
        );
        this._controller = controller;

        return { msgId, cancel: () => this.cancel() };
    },

    cancel() {
        if (this._controller) {
            try {
                this._controller.abort();
            } catch (e) { /* ignore */ }
            this._controller = null;
        }
        this._active = false;
        ChatPage.setInputEnabled(true);
        ChatPage.updateSendButton();
    },

    isActive() {
        return this._active;
    },
};
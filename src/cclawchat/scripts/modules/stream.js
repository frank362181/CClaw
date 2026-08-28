import { API } from './api.js?v=20260826';
import { State } from './state.js';
import { ChatPage } from './chat.js';
import { genId } from '../utils/helper.js';

// 中断兜底超时：通知后端中断后，若后端未及时关闭流，超时后本地中止以保证“立即停止”。
const INTERRUPT_FALLBACK_MS = 3000;

export const StreamEngine = {
    _activeRequest: null,

    start(convId, userContent, onComplete, onError) {
        this.cancel();
        const requestId = genId();
        const msgId = genId();
        const request = { requestId, convId, msgId, controller: null, content: '', statusText: '', reasoning: '', interruptFallback: null };
        this._activeRequest = request;
        State.isStreaming = true;
        ChatPage.updateSendButton(); // 流式中:发送按钮变为中断按钮
        State.addMessage(convId, 'assistant', '', msgId);
        ChatPage.appendStreamingMessage(convId, msgId);

        const isCurrent = () => this._activeRequest?.requestId === requestId;
        const finish = () => {
            if (!isCurrent()) return false;
            this._activeRequest = null;
            State.isStreaming = false;
            if (request.interruptFallback) {
                clearTimeout(request.interruptFallback);
                request.interruptFallback = null;
            }
            ChatPage.setInputEnabled(true);
            ChatPage.updateSendButton();
            return true;
        };

        request.controller = API.sendMessageStream(
            convId,
            userContent,
            State.user?.token,
            content => {
                if (!isCurrent()) return;
                request.content = content;
                State.updateMessageContent(convId, msgId, content, { persist: false });
                ChatPage.updateStreamingMessage(msgId, content, false);
            },
            content => {
                if (!isCurrent()) return;
                request.content = content || request.content;
                State.updateMessageStatus(convId, msgId, '');
                State.updateMessageContent(convId, msgId, request.content);
                ChatPage.updateStreamingMessage(msgId, request.content, true);
                if (!finish()) return;
                onComplete?.(request.content);
            },
            error => {
                if (!isCurrent()) return;
                // 内容已送达过:保留已输出内容,不再用错误提示整体覆盖(内容对用户仍有价值)
                if (request.content) {
                    State.updateMessageStatus(convId, msgId, '');
                    State.updateMessageContent(convId, msgId, request.content);
                    ChatPage.updateStreamingMessage(msgId, request.content, true);
                    if (!finish()) return;
                    onComplete?.(request.content);
                    return;
                }
                const message = `⚠️ ${error?.message || '网络错误，请稍后重试'}`;
                State.updateMessageStatus(convId, msgId, '');
                State.updateMessageContent(convId, msgId, message);
                ChatPage.updateStreamingMessage(msgId, message, true);
                if (!finish()) return;
                onError?.(error);
            },
            status => {
                // 内容开始前,展示 Agent 当前阶段(拆解任务/思考/推理等)
                if (!isCurrent()) return;
                request.statusText = status?.text || '';
                State.updateMessageStatus(convId, msgId, request.statusText);
                ChatPage.updateStreamingStatus(msgId, status?.text);
            },
            delta => {
                // 思考过程(reasoning_content)增量:实时展示思考过程(不写入 content)
                if (!isCurrent()) return;
                request.reasoning = (request.reasoning || '') + delta;
                State.updateMessageReasoning(convId, msgId, request.reasoning);
                ChatPage.updateStreamingReasoning(msgId, request.reasoning);
            },
        );
        return { msgId, cancel: () => this.cancel(requestId) };
    },

    /**
     * 用户点击中断按钮：停止当前会话的流式聊天（思考/推理/流式输出）。
     * - 立即标记消息“已中断”并给出反馈（保留已输出内容）；
     * - 通知后端中断 API，后端取消该会话的 LLM 流式上下文并关闭 SSE（主机制）；
     * - 后端未及时关闭流或接口失败时，由本地中止兜底，保证“立即停止”。
     * @param {string} convId 会话标识
     * @returns {Promise<boolean>} 是否存在可中断的流
     */
    async interrupt(convId) {
        const request = this._activeRequest;
        if (!request || request.convId !== convId) return false;
        const requestId = request.requestId;

        // 先标记“已中断”（无论后端经流关闭还是本地中止收尾，用户都能看到反馈）
        State.markMessageInterrupted(request.convId, request.msgId);
        State.updateMessageStatus(request.convId, request.msgId, '');
        ChatPage.markStreamingInterrupted(request.msgId);

        // 兜底：后端未及时关闭流时本地中止
        request.interruptFallback = setTimeout(() => {
            if (this.isActive() && this._activeRequest?.requestId === requestId) {
                this.cancel(requestId, { interrupted: true });
            }
        }, INTERRUPT_FALLBACK_MS);

        // 通知后端停止对应会话的流式聊天（主机制）
        try {
            await API.interruptChat(convId, State.user?.token);
        } catch (_) {
            // 后端不可达/会话未在聊天：由本地中止兜底
            this.cancel(requestId, { interrupted: true });
        }
        return true;
    },

    cancel(requestId, opts = {}) {
        const request = this._activeRequest;
        if (!request || (requestId && request.requestId !== requestId)) return false;
        this._activeRequest = null;
        State.isStreaming = false;
        if (request.interruptFallback) {
            clearTimeout(request.interruptFallback);
            request.interruptFallback = null;
        }
        request.controller?.abort();
        State.updateMessageStatus(request.convId, request.msgId, '');
        State.updateMessageContent(request.convId, request.msgId, request.content);
        ChatPage.updateStreamingMessage(request.msgId, request.content, true);
        if (opts.interrupted) {
            State.markMessageInterrupted(request.convId, request.msgId);
            ChatPage.markStreamingInterrupted(request.msgId);
        }
        ChatPage.setInputEnabled(true);
        ChatPage.updateSendButton();
        return true;
    },

    isActive() {
        return Boolean(this._activeRequest);
    },
};

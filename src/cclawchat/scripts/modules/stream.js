import { API } from './api.js?v=20260826';
import { State } from './state.js';
import { ChatPage } from './chat.js';
import { genId } from '../utils/helper.js';

export const StreamEngine = {
    _activeRequest: null,

    start(convId, userContent, onComplete, onError) {
        this.cancel();
        const requestId = genId();
        const msgId = genId();
        const request = { requestId, convId, msgId, controller: null, content: '', statusText: '', reasoning: '' };
        this._activeRequest = request;
        State.isStreaming = true;
        State.addMessage(convId, 'assistant', '', msgId);
        ChatPage.appendStreamingMessage(convId, msgId);

        const isCurrent = () => this._activeRequest?.requestId === requestId;
        const finish = () => {
            if (!isCurrent()) return false;
            this._activeRequest = null;
            State.isStreaming = false;
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

    cancel(requestId) {
        const request = this._activeRequest;
        if (!request || (requestId && request.requestId !== requestId)) return false;
        this._activeRequest = null;
        State.isStreaming = false;
        request.controller?.abort();
        State.updateMessageStatus(request.convId, request.msgId, '');
        State.updateMessageContent(request.convId, request.msgId, request.content);
        ChatPage.updateStreamingMessage(request.msgId, request.content, true);
        ChatPage.setInputEnabled(true);
        ChatPage.updateSendButton();
        return true;
    },

    isActive() {
        return Boolean(this._activeRequest);
    },
};

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
        const request = { requestId, convId, msgId, controller: null, content: '' };
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
                State.updateMessageContent(convId, msgId, request.content);
                ChatPage.updateStreamingMessage(msgId, request.content, true);
                if (!finish()) return;
                onComplete?.(request.content);
            },
            error => {
                if (!isCurrent()) return;
                const message = `⚠️ ${error?.message || '网络错误，请稍后重试'}`;
                State.updateMessageContent(convId, msgId, message);
                ChatPage.updateStreamingMessage(msgId, message, true);
                if (!finish()) return;
                onError?.(error);
            },
            status => {
                // 内容开始前,展示 Agent 当前阶段(拆解任务/思考/推理等)
                if (!isCurrent()) return;
                ChatPage.updateStreamingStatus(msgId, status?.text);
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

/**
 * ============================================================
 * 聊天页组件控制器 (ChatPage)
 * ============================================================
 * 高内聚：仅负责聊天页视图（侧边栏 / 会话 / 消息 / 输入区）的渲染与交互。
 * 低耦合：不直接依赖 App，通过注入回调与外部协作；会话切换 / 删除
 *         通过 document 自定义事件对外广播。
 */

import { State } from './state.js';
import { getInitials, formatTime } from '../utils/helper.js';

export const ChatPage = {
    comp: null,
    els: {},

    /**
     * 初始化组件
     * @param {object} comp 组件实例（mountComponent 返回）
     * @param {{ onSend?: Function, onNewChat?: Function, onClearChat?: Function, onHideChat?: Function }} handlers
     */
    init(comp, handlers = {}) {
        this.comp = comp;
        this.els = {
            chatPage: comp.query('#chatPage'),
            messageList: comp.query('#messageList'),
            emptyState: comp.query('#emptyState'),
            chatInput: comp.query('#chatInput'),
            btnSend: comp.query('#btnSend'),
            btnHideChat: comp.query('#btnHideChat'),
            btnNewChat: comp.query('#btnNewChat'),
            btnClearChat: comp.query('#btnClearChat'),
            chatTitle: comp.query('#chatTitle'),
        };

        // 输入区自适应高度 + 发送按钮状态
        this.els.chatInput.addEventListener('input', () => {
            this.els.chatInput.style.height = 'auto';
            this.els.chatInput.style.height = Math.min(this.els.chatInput.scrollHeight, 160) + 'px';
            this.updateSendButton();
        });

        // 回车发送（Shift+Enter 换行）
        this.els.chatInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                handlers.onSend?.();
            }
            this.updateSendButton();
        });

        // 工具栏按钮
        this.els.btnSend.addEventListener('click', () => handlers.onSend?.());
        this.els.btnHideChat.addEventListener('click', () => handlers.onHideChat?.());
        this.els.btnNewChat.addEventListener('click', () => handlers.onNewChat?.());
        this.els.btnClearChat.addEventListener('click', () => handlers.onClearChat?.());
    },

    show() {
        this.els.chatPage.style.display = 'flex';
        requestAnimationFrame(() => {
            this.els.chatPage.classList.add('active');
        });
    },

    hide() {
        this.els.chatPage.classList.remove('active');
        this.els.chatPage.style.display = 'none';
    },



    renderMessages(conv) {
        const container = this.els.messageList;
        const empty = this.els.emptyState;

        if (!conv || !conv.messages || conv.messages.length === 0) {
            container.querySelectorAll('.message').forEach(el => el.remove());
            empty.style.display = 'flex';
            return;
        }

        empty.style.display = 'none';
        container.querySelectorAll('.message').forEach(el => el.remove());

        const fragment = document.createDocumentFragment();
        conv.messages.forEach(msg => {
            const isUser = msg.role === 'user';
            const div = document.createElement('div');
            div.className = `message ${isUser ? 'user' : 'assistant'}`;
            div.dataset.msgId = msg.id;

            const avatar = document.createElement('div');
            avatar.className = 'avatar';
            avatar.textContent = isUser ? (State.user ? getInitials(State.user.username) : 'U') : 'AI';

            const bubbleWrap = document.createElement('div');
            bubbleWrap.style.display = 'flex';
            bubbleWrap.style.flexDirection = 'column';
            bubbleWrap.style.maxWidth = '100%';

            const bubble = document.createElement('div');
            bubble.className = 'bubble';
            bubble.innerHTML = msg.content.replace(/\n/g, '<br/>');

            const time = document.createElement('div');
            time.className = 'timestamp';
            time.textContent = formatTime(msg.timestamp);

            bubbleWrap.appendChild(bubble);
            bubbleWrap.appendChild(time);

            if (isUser) {
                div.appendChild(bubbleWrap);
                div.appendChild(avatar);
            } else {
                div.appendChild(avatar);
                div.appendChild(bubbleWrap);
            }

            fragment.appendChild(div);
        });

        container.appendChild(fragment);
        this.scrollToBottom();
    },

    appendStreamingMessage(convId, msgId) {
        const container = this.els.messageList;
        const empty = this.els.emptyState;
        empty.style.display = 'none';

        const existing = container.querySelector(`.message[data-msg-id="${msgId}"]`);
        if (existing) existing.remove();

        const div = document.createElement('div');
        div.className = 'message assistant streaming';
        div.dataset.msgId = msgId;

        const avatar = document.createElement('div');
        avatar.className = 'avatar';
        avatar.textContent = 'AI';

        const bubbleWrap = document.createElement('div');
        bubbleWrap.style.display = 'flex';
        bubbleWrap.style.flexDirection = 'column';
        bubbleWrap.style.maxWidth = '100%';

        const bubble = document.createElement('div');
        bubble.className = 'bubble';
        bubble.innerHTML = '<span class="cursor-blink"></span>';

        const time = document.createElement('div');
        time.className = 'timestamp';
        time.textContent = formatTime(Date.now());

        bubbleWrap.appendChild(bubble);
        bubbleWrap.appendChild(time);

        div.appendChild(avatar);
        div.appendChild(bubbleWrap);

        container.appendChild(div);
        this.scrollToBottom();
        return bubble;
    },

    updateStreamingMessage(msgId, content, isComplete = false) {
        const container = this.els.messageList;
        const msgEl = container.querySelector(`.message[data-msg-id="${msgId}"]`);
        if (!msgEl) return;

        const bubble = msgEl.querySelector('.bubble');
        if (!bubble) return;

        const escaped = content.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        const html = escaped.replace(/\n/g, '<br/>');

        if (isComplete) {
            bubble.innerHTML = html;
            msgEl.classList.remove('streaming');
            const timeEl = msgEl.querySelector('.timestamp');
            if (timeEl) timeEl.textContent = formatTime(Date.now());
        } else {
            bubble.innerHTML = html + '<span class="cursor-blink"></span>';
        }
        this.scrollToBottom();
    },

    // 内容开始前展示 Agent 当前阶段状态(拆解任务/思考/推理等)
    updateStreamingStatus(msgId, statusText) {
        const container = this.els.messageList;
        const msgEl = container.querySelector(`.message[data-msg-id="${msgId}"]`);
        if (!msgEl) return;

        const bubble = msgEl.querySelector('.bubble');
        if (!bubble) return;

        const text = (statusText || '思考中…').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        bubble.innerHTML = `<span class="thinking-indicator"></span><span class="thinking-text">${text}</span><span class="cursor-blink"></span>`;
        this.scrollToBottom();
    },

    scrollToBottom() {
        const container = this.els.messageList;
        requestAnimationFrame(() => {
            container.scrollTop = container.scrollHeight;
        });
    },

    // 输入框与发送按钮是否可用。
    // 以 State.isStreaming 为最终约束:流式输出未结束前始终灰化,直到流式结束才恢复可用,
    // 避免抽屉开关等其它调用方在流式期间误把输入区重新启用。
    setInputEnabled(enabled) {
        const input = this.els.chatInput;
        const btn = this.els.btnSend;
        input.disabled = !enabled || State.isStreaming;
        btn.disabled = !enabled || State.isStreaming;
        if (enabled && !State.isStreaming) {
            input.focus();
        }
    },

    clearInput() {
        this.els.chatInput.value = '';
        this.els.chatInput.style.height = 'auto';
        this.els.chatInput.focus();
        this.updateSendButton();
    },

    updateSendButton() {
        const val = this.els.chatInput.value.trim();
        this.els.btnSend.disabled = !val || State.isStreaming || this.els.chatInput.disabled;
    },

    updateUserInfo(username) {
        // 对话历史侧边栏已移除：用户信息展示由主页(HomePage)负责，此处无需操作。
    },

    updateChatTitle(title) {
        // 防护：视图缺失 #chatTitle 时不抛错（避免拖垮登录/会话流程）
        if (this.els.chatTitle) {
            this.els.chatTitle.textContent = title || '新对话';
        }
    },

    getInputValue() {
        return this.els.chatInput.value.trim();
    },

    isInputDisabled() {
        return this.els.chatInput.disabled;
    },

    focusInput() {
        this.els.chatInput.focus();
    },

    reset() {
        if (this.els.chatTitle) {
            this.els.chatTitle.textContent = '新对话';
        }
        this.els.messageList.querySelectorAll('.message').forEach(el => el.remove());
        this.els.emptyState.style.display = 'flex';
    },
};

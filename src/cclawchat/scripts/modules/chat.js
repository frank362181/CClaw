/**
 * ============================================================
 * 聊天页组件控制器 (ChatPage)
 * ============================================================
 * 高内聚：仅负责聊天页视图（侧边栏 / 会话 / 消息 / 输入区）的渲染与交互。
 * 低耦合：不直接依赖 App，通过注入回调与外部协作；会话切换 / 删除
 *         通过 document 自定义事件对外广播。
 */

import { State } from './state.js';
import { getInitials, formatTime, formatDate, truncate } from '../utils/helper.js';

export const ChatPage = {
    comp: null,
    els: {},

    /**
     * 初始化组件
     * @param {object} comp 组件实例（mountComponent 返回）
     * @param {{ onSend?: Function, onNewChat?: Function, onClearChat?: Function, onLogout?: Function, onConfirmLogout?: Function }} handlers
     */
    init(comp, handlers = {}) {
        this.comp = comp;
        this.els = {
            chatPage: comp.query('#chatPage'),
            chatSidebar: comp.query('#chatSidebar'),
            sidebarOverlay: comp.query('#sidebarOverlay'),
            conversationList: comp.query('#conversationList'),
            messageList: comp.query('#messageList'),
            emptyState: comp.query('#emptyState'),
            chatInput: comp.query('#chatInput'),
            btnSend: comp.query('#btnSend'),
            btnNewChat: comp.query('#btnNewChat'),
            btnLogout: comp.query('#btnLogout'),
            btnClearChat: comp.query('#btnClearChat'),
            btnToggleSidebar: comp.query('#btnToggleSidebar'),
            chatTitle: comp.query('#chatTitle'),
            userNameDisplay: comp.query('#userNameDisplay'),
            userAvatar: comp.query('#userAvatar'),
            // 退出确认弹窗
            logoutConfirm: comp.query('#logoutConfirm'),
            btnCancelLogout: comp.query('#btnCancelLogout'),
            btnConfirmLogout: comp.query('#btnConfirmLogout'),
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
        this.els.btnNewChat.addEventListener('click', () => handlers.onNewChat?.());
        this.els.btnClearChat.addEventListener('click', () => handlers.onClearChat?.());
        // 点击退出：先弹出二次确认（是否真正退出由外部决定）
        this.els.btnLogout.addEventListener('click', () => handlers.onLogout?.());
        this.els.btnToggleSidebar.addEventListener('click', () => this.toggleSidebar());
        this.els.sidebarOverlay.addEventListener('click', () => this.closeSidebar());

        // 退出确认弹窗：取消则关闭，确认则回调外部执行退出
        this.els.btnCancelLogout.addEventListener('click', () => this.closeLogoutConfirm());
        this.els.btnConfirmLogout.addEventListener('click', () => handlers.onConfirmLogout?.());
        this.els.logoutConfirm.addEventListener('click', (e) => {
            if (e.target === this.els.logoutConfirm) this.closeLogoutConfirm();
        });
    },

    show() {
        this.closeLogoutConfirm();
        this.els.chatPage.style.display = 'flex';
        requestAnimationFrame(() => {
            this.els.chatPage.classList.add('active');
        });
    },

    openLogoutConfirm() {
        this.els.logoutConfirm.classList.add('show');
    },

    closeLogoutConfirm() {
        this.els.logoutConfirm.classList.remove('show');
    },

    hide() {
        this.els.chatPage.classList.remove('active');
        this.els.chatPage.style.display = 'none';
    },

    toggleSidebar(open) {
        const isOpen = this.els.chatSidebar.classList.contains('open');
        const target = open !== undefined ? open : !isOpen;
        this.els.chatSidebar.classList.toggle('open', target);
        this.els.sidebarOverlay.classList.toggle('active', target);
        document.body.style.overflow = target ? 'hidden' : '';
    },

    closeSidebar() {
        this.toggleSidebar(false);
    },

    renderConversations(conversations, currentId) {
        const container = this.els.conversationList;
        if (!conversations || conversations.length === 0) {
            container.innerHTML = `
                <div style="text-align:center;padding:var(--space-xl) 0;color:var(--color-text-muted);font-size:var(--font-size-sm);">
                    <div style="font-size:32px;margin-bottom:var(--space-sm);">📭</div>
                    暂无对话<br/>点击 「+」 开始新对话
                </div>
            `;
            return;
        }

        let html = '';
        conversations.forEach(conv => {
            const isActive = conv.id === currentId;
            const lastMsg = conv.messages.length > 0 ? conv.messages[conv.messages.length - 1] : null;
            const preview = lastMsg ? truncate(lastMsg.content, 32) : '空对话';
            const timeLabel = formatDate(conv.updatedAt);

            html += `
                <div class="conversation-item ${isActive ? 'active' : ''}" data-conv-id="${conv.id}">
                    <span class="conv-icon">${isActive ? '●' : '○'}</span>
                    <span class="conv-title">${truncate(conv.title, 26)}</span>
                    <span class="conv-time">${timeLabel}</span>
                    <button class="conv-delete" data-conv-id="${conv.id}" title="删除对话">✕</button>
                </div>
            `;
        });
        container.innerHTML = html;

        container.querySelectorAll('.conversation-item').forEach(item => {
            item.addEventListener('click', (e) => {
                if (e.target.closest('.conv-delete')) return;
                const convId = item.dataset.convId;
                if (convId && convId !== State.currentConvId) {
                    // 由外部 App 处理切换（低耦合：通过事件广播）
                    const event = new CustomEvent('switch-conversation', { detail: { convId } });
                    document.dispatchEvent(event);
                }
                if (window.innerWidth <= 768) {
                    this.closeSidebar();
                }
            });
        });

        container.querySelectorAll('.conv-delete').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.stopPropagation();
                const convId = btn.dataset.convId;
                if (convId && confirm('确定要删除这个对话吗？')) {
                    const event = new CustomEvent('delete-conversation', { detail: { convId } });
                    document.dispatchEvent(event);
                }
            });
        });
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

    scrollToBottom() {
        const container = this.els.messageList;
        requestAnimationFrame(() => {
            container.scrollTop = container.scrollHeight;
        });
    },

    setInputEnabled(enabled) {
        const input = this.els.chatInput;
        const btn = this.els.btnSend;
        input.disabled = !enabled;
        btn.disabled = !enabled;
        if (enabled) {
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
        const display = username || '用户';
        this.els.userNameDisplay.textContent = display;
        this.els.userAvatar.textContent = getInitials(display);
    },

    updateChatTitle(title) {
        this.els.chatTitle.textContent = title || '新对话';
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
        this.closeLogoutConfirm();
        this.els.chatTitle.textContent = '新对话';
        this.els.messageList.querySelectorAll('.message').forEach(el => el.remove());
        this.els.emptyState.style.display = 'flex';
        this.els.conversationList.innerHTML = '';
        this.closeSidebar();
    },
};

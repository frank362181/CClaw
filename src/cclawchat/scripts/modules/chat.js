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

// 发送/中断按钮图标:使用内联 SVG 而非文本字形(➤/⏹ + 变体选择符),
// 避免 Safari 等系统把字形按 emoji 绘制(带背景的彩色方块/箭头),出现"覆盖一层图标"的观感。
const ICON_SEND = '<svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z"/></svg>';
const ICON_INTERRUPT = '<svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M6 6h12v12H6z"/></svg>';

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

        // 工具栏按钮：流式中发送按钮变为中断按钮，点击中断；平时点击发送
        this.els.btnSend.addEventListener('click', () => {
            if (State.isStreaming) {
                handlers.onInterrupt?.();
            } else {
                handlers.onSend?.();
            }
        });
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

            if (isUser) {
                bubble.innerHTML = msg.content.replace(/\n/g, '<br/>');
            } else {
                // 思考过程块(会话内存中存在时渲染;默认折叠展示)
                if (msg.reasoningText) {
                    const rw = document.createElement('details');
                    rw.className = 'reasoning-wrap';
                    const summary = document.createElement('summary');
                    summary.textContent = `${State.agentEmoji || '🤖'} 思考过程`;
                    const textEl = document.createElement('div');
                    textEl.className = 'reasoning-text';
                    textEl.innerHTML = this._escapeHtml(msg.reasoningText).replace(/\n/g, '<br/>');
                    rw.appendChild(summary);
                    rw.appendChild(textEl);
                    bubble.appendChild(rw);
                }

                const contentEl = document.createElement('span');
                contentEl.className = 'stream-content';
                if (!msg.content && msg.statusText && State.isStreaming) {
                    // 流式"思考/准备"阶段内容尚未到达:抽屉收起再展开触发全量重渲染时,
                    // 依据暂存的 statusText 恢复 thinking 指示(仅 UI,不写 content)。
                    contentEl.innerHTML = `<span class="thinking-indicator"></span><span class="thinking-text">${this._escapeHtml(msg.statusText)}</span>`;
                    div.classList.add('streaming');
                } else {
                    contentEl.innerHTML = msg.content.replace(/\n/g, '<br/>');
                }
                bubble.appendChild(contentEl);
                if (div.classList.contains('streaming')) {
                    bubble.appendChild(this._makeCursor());
                }
                if (msg.interrupted) {
                    bubble.appendChild(this._makeInterruptedTag());
                }
            }

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
        const contentEl = document.createElement('span');
        contentEl.className = 'stream-content';
        bubble.appendChild(contentEl);
        bubble.appendChild(this._makeCursor());

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

        const contentEl = bubble.querySelector('.stream-content');
        if (!contentEl) return;

        contentEl.innerHTML = this._escapeHtml(content).replace(/\n/g, '<br/>');

        if (isComplete) {
            bubble.querySelectorAll('.cursor-blink').forEach(el => el.remove());
            msgEl.classList.remove('streaming');
            const timeEl = msgEl.querySelector('.timestamp');
            if (timeEl) timeEl.textContent = formatTime(Date.now());
        } else if (!bubble.querySelector('.cursor-blink')) {
            bubble.appendChild(this._makeCursor());
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

        const contentEl = bubble.querySelector('.stream-content');
        if (!contentEl) return;

        contentEl.innerHTML = `<span class="thinking-indicator"></span><span class="thinking-text">${this._escapeHtml(statusText || '思考中…')}</span>`;
        if (!bubble.querySelector('.cursor-blink')) {
            bubble.appendChild(this._makeCursor());
        }
        this.scrollToBottom();
    },

    // 内容开始前,实时展示模型思考过程(reasoning_content)增量文本。
    // 首次收到增量时创建可折叠块(展开状态实时可见);后续只更新文本,不打断用户手动折叠。
    updateStreamingReasoning(msgId, reasoningText) {
        const container = this.els.messageList;
        const msgEl = container.querySelector(`.message[data-msg-id="${msgId}"]`);
        if (!msgEl) return;

        const bubble = msgEl.querySelector('.bubble');
        if (!bubble) return;

        let rw = bubble.querySelector('.reasoning-wrap');
        if (!rw) {
            rw = document.createElement('details');
            rw.className = 'reasoning-wrap';
            rw.open = true; // 实时思考过程默认展开
            const summary = document.createElement('summary');
            summary.textContent = `${State.agentEmoji || '🤖'} 思考过程`;
            const textEl = document.createElement('div');
            textEl.className = 'reasoning-text';
            rw.appendChild(summary);
            rw.appendChild(textEl);
            const contentEl = bubble.querySelector('.stream-content');
            bubble.insertBefore(rw, contentEl || bubble.firstChild);
            // 思考过程已可见,清掉"正在思考…"占位
            if (contentEl) contentEl.innerHTML = '';
        }

        const textEl = rw.querySelector('.reasoning-text');
        textEl.innerHTML = this._escapeHtml(reasoningText).replace(/\n/g, '<br/>');
        this.scrollToBottom();
    },

    scrollToBottom() {
        const container = this.els.messageList;
        requestAnimationFrame(() => {
            container.scrollTop = container.scrollHeight;
        });
    },

    // HTML 转义(与各流式更新方法保持一致的转义规则)
    _escapeHtml(str) {
        return String(str ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    },

    // 创建流式光标元素
    _makeCursor() {
        const c = document.createElement('span');
        c.className = 'cursor-blink';
        return c;
    },

    // 创建“已中断”标记元素
    _makeInterruptedTag() {
        const tag = document.createElement('span');
        tag.className = 'interrupted-tag';
        tag.textContent = '\u23F9\uFE0E 已中断'; // ⏹ 文本呈现,避免 emoji 背景图样
        return tag;
    },

    // 在流式消息上追加“已中断”标记（用户点击中断按钮后）。
    markStreamingInterrupted(msgId) {
        const container = this.els.messageList;
        const msgEl = container.querySelector(`.message[data-msg-id="${msgId}"]`);
        if (!msgEl) return;
        const bubble = msgEl.querySelector('.bubble');
        if (!bubble) return;
        if (bubble.querySelector('.interrupted-tag')) return; // 防重复
        bubble.appendChild(this._makeInterruptedTag());
        this.scrollToBottom();
    },

    // 输入框与发送按钮是否可用。
    // 以 State.isStreaming 为最终约束:流式输出未结束前输入框始终灰化,直到流式结束才恢复可用,
    // 避免抽屉开关等其它调用方在流式期间误把输入区重新启用。
    // 发送按钮在流式中由 updateSendButton 切换为中断按钮(始终可点)。
    setInputEnabled(enabled) {
        const input = this.els.chatInput;
        input.disabled = !enabled || State.isStreaming;
        if (enabled && !State.isStreaming) {
            input.focus();
        }
        this.updateSendButton();
    },

    clearInput() {
        this.els.chatInput.value = '';
        this.els.chatInput.style.height = 'auto';
        this.els.chatInput.focus();
        this.updateSendButton();
    },

    updateSendButton() {
        const btn = this.els.btnSend;
        const icon = btn.querySelector('.send-icon');
        if (State.isStreaming) {
            // 流式/思考中:发送按钮变为中断按钮,始终可点(点击即中断)。
            btn.disabled = false;
            btn.classList.add('interrupt');
            btn.title = '中断生成';
            this._setSendIcon(icon, ICON_INTERRUPT);
            return;
        }
        btn.classList.remove('interrupt');
        btn.title = '发送';
        this._setSendIcon(icon, ICON_SEND);
        const val = this.els.chatInput.value.trim();
        btn.disabled = !val || this.els.chatInput.disabled;
    },

    // 原子替换发送按钮图标:先清空再插入,避免部分浏览器(Safari)对 SVG 的
    // innerHTML 替换不彻底,导致旧的发送/中断图标残留叠加(表现为按钮上覆盖一层方块)。
    _setSendIcon(icon, svgHtml) {
        if (!icon) return;
        icon.textContent = '';
        icon.insertAdjacentHTML('beforeend', svgHtml);
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

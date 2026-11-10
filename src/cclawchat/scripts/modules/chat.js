/**
 * ============================================================
 * 聊天页组件控制器 (ChatPage)
 * ============================================================
 * 高内聚：仅负责聊天页视图（历史会话抽屉 / 消息 / 输入区）的渲染与交互。
 * 低耦合：不直接依赖 App，通过注入回调与外部协作（会话切换 / 删除由 App 落库）。
 */

import { State } from './state.js';
import { getInitials, formatTime, formatDate, truncate, escapeHtml } from '../utils/helper.js';

// 发送/中断按钮图标:使用内联 SVG 而非文本字形(➤/⏹ + 变体选择符),
// 避免 Safari 等系统把字形按 emoji 绘制(带背景的彩色方块/箭头),出现"覆盖一层图标"的观感。
const ICON_SEND = '<svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z"/></svg>';
const ICON_INTERRUPT = '<svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M6 6h12v12H6z"/></svg>';

// 历史会话抽屉：只展示最近 15 条（与后端 SessionLimitPage 一致）
const HISTORY_LIMIT = 15;

// 会话历史轮次：只展示最近 15 轮（与后端 SessionLimitPage 一致）
const TURNS_LIMIT = 15;

// 轮次状态（与后端 TurnStatus* 对齐）
// 0=正常 1=中断且无产出 2=中断但有部分产出 3=助手记录未落盘
const TURN_INTERRUPTED = new Set([1, 2, 3]);

// 中断来源（与后端 Interrupt* 对齐）
const INTERRUPT_REASON_TEXT = {
    0: '',
    1: '用户中断',
    2: '客户端断开',
    3: '运行超时',
    4: '上游流被取消',
    5: '上游/流错误',
    6: '已中断（来源未知）',
};

// 空状态默认提示语（服务端会话本地无缓存时会被替换为说明文案）
const DEFAULT_EMPTY_HINT = '在下方输入消息，AI 助手将为您提供智能回复';
const REMOTE_SESSION_HINT = '该会话暂无本地消息；继续发送将续写在同一会话中。';

export const ChatPage = {
    comp: null,
    els: {},
    _sessions: null,     // 后端返回的会话主题（null=尚未加载）
    _sessionsError: '',  // 加载失败原因（非空时降级为本地会话）
    _sessionsReqId: 0,   // 请求序号，用于丢弃过期响应

    /**
     * 初始化组件
     * @param {object} comp 组件实例（mountComponent 返回）
     * @param {{ onSend?: Function, onNewChat?: Function, onClearChat?: Function,
     *           onSelectConversation?: Function, onDeleteConversation?: Function }} handlers
     */
    init(comp, handlers = {}) {
        this.comp = comp;
        this.handlers = handlers;
        this.els = {
            chatPage: comp.query('#chatPage'),
            messageList: comp.query('#messageList'),
            emptyState: comp.query('#emptyState'),
            chatInput: comp.query('#chatInput'),
            btnSend: comp.query('#btnSend'),
            btnNewChat: comp.query('#btnNewChat'),
            btnClearChat: comp.query('#btnClearChat'),
            chatTitle: comp.query('#chatTitle'),
            chatAgent: comp.query('#chatAgent'),
            // 历史会话抽屉
            btnHistory: comp.query('#btnHistory'),
            historyDrawer: comp.query('#historyDrawer'),
            historyOverlay: comp.query('#historyOverlay'),
            historyList: comp.query('#historyList'),
            btnHistoryNew: comp.query('#btnHistoryNew'),
            btnHistoryClose: comp.query('#btnHistoryClose'),
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
        this.els.btnNewChat.addEventListener('click', () => handlers.onNewChat?.());
        this.els.btnClearChat.addEventListener('click', () => handlers.onClearChat?.());

        // 历史会话抽屉：打开 / 关闭 / 新建 / 切换 / 删除
        this.els.btnHistory.addEventListener('click', () => this.toggleHistory());
        this.els.btnHistoryClose.addEventListener('click', () => this.closeHistory());
        this.els.historyOverlay.addEventListener('click', () => this.closeHistory());
        this.els.btnHistoryNew.addEventListener('click', () => {
            this.closeHistory();
            handlers.onNewChat?.();
        });
        this.els.historyList.addEventListener('click', (e) => {
            const del = e.target.closest('[data-conv-delete]');
            if (del) {
                e.stopPropagation();
                handlers.onDeleteConversation?.(del.dataset.convDelete);
                return;
            }
            const item = e.target.closest('[data-conv-id]');
            if (item) handlers.onSelectConversation?.(item.dataset.convId, item.dataset.convTitle || '', item.dataset.convAgent || '');
        });
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && this.isHistoryOpen()) this.closeHistory();
        });
    },

    show() {
        // 宿主元素（主界面右侧 40% 分栏）与内部页面一起显示
        this.comp.el.style.display = 'flex';
        this.els.chatPage.style.display = 'flex';
        requestAnimationFrame(() => {
            this.els.chatPage.classList.add('active');
        });
    },

    hide() {
        this.closeHistory();
        this.els.chatPage.classList.remove('active');
        this.els.chatPage.style.display = 'none';
        this.comp.el.style.display = 'none';
    },



    renderMessages(conv) {
        const container = this.els.messageList;
        const empty = this.els.emptyState;
        this._clearTurns(); // 实时消息渲染时移除历史轮次视图

        if (!conv || !conv.messages || conv.messages.length === 0) {
            container.querySelectorAll('.message').forEach(el => el.remove());
            empty.style.display = 'flex';
            this.setEmptyStateHint(conv?.placeholder ? REMOTE_SESSION_HINT : '');
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

                // 工具调用记录(会话内存中存在时渲染;重渲染为终态展示)
                if (Array.isArray(msg.toolLog) && msg.toolLog.length) {
                    const tl = document.createElement('div');
                    tl.className = 'tool-list';
                    msg.toolLog.forEach(t => {
                        const phaseClass = t.phase === 'done' ? 'done' : (t.phase === 'error' ? 'error' : 'pending');
                        const statusText = { intent: '⏳ 等待执行', running: '⟳ 执行中…', done: '✓ 完成', error: '✗ 失败' }[t.phase] || '';
                        const it = document.createElement('div');
                        it.className = `tool-item ${phaseClass}`;
                        const header = document.createElement('div');
                        header.className = 'tool-header';
                        header.innerHTML = `<span class="tool-icon">⚙️</span><span class="tool-name">${this._escapeHtml(t.name)}</span><span class="tool-status">${this._escapeHtml(statusText)}</span>`;
                        it.appendChild(header);
                        if (t.detail) {
                            const detail = document.createElement('div');
                            detail.className = 'tool-detail';
                            detail.textContent = t.detail;
                            it.appendChild(detail);
                        }
                        tl.appendChild(it);
                    });
                    bubble.appendChild(tl);
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
                // 收尾提示(truncated/content_filter/incomplete):独立于回答内容的警示行
                if (Array.isArray(msg.notices) && msg.notices.length) {
                    const nl = document.createElement('div');
                    nl.className = 'notice-list';
                    msg.notices.forEach(n => {
                        const alert = n.level === 'error';
                        const it = document.createElement('div');
                        it.className = alert ? 'stream-notice alert-error' : 'stream-notice';
                        it.textContent = `${alert ? '⛔' : '⚠️'} ${n.msg || ''}`;
                        nl.appendChild(it);
                    });
                    bubble.appendChild(nl);
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

    // 流式工具调用进度:渲染/更新工具卡片(intent 创建行,progress 执行日志追加,running/done/error 更新状态)
    updateStreamingTool(msgId, tool) {
        const container = this.els.messageList;
        const msgEl = container.querySelector(`.message[data-msg-id="${msgId}"]`);
        if (!msgEl) return;
        const bubble = msgEl.querySelector('.bubble');
        if (!bubble) return;

        // 惰性创建工具区(reasoning 之后、正式内容之前)
        let list = bubble.querySelector('.tool-list');
        if (!list) {
            list = document.createElement('div');
            list.className = 'tool-list';
            const contentEl = bubble.querySelector('.stream-content');
            bubble.insertBefore(list, contentEl || null);
        }

        let item = list.querySelector(`.tool-item[data-seq="${tool.seq}"]`);
        if (!item) {
            item = document.createElement('div');
            item.className = 'tool-item pending';
            item.dataset.seq = tool.seq;
            const header = document.createElement('div');
            header.className = 'tool-header';
            header.innerHTML = '<span class="tool-icon">⚙️</span><span class="tool-name"></span><span class="tool-status"></span>';
            item.appendChild(header);
            const detail = document.createElement('div');
            detail.className = 'tool-detail';
            item.appendChild(detail);
            list.appendChild(item);
        }

        const nameEl = item.querySelector('.tool-name');
        const statusEl = item.querySelector('.tool-status');
        const detailEl = item.querySelector('.tool-detail');
        if (tool.name) nameEl.textContent = tool.name;

        if (tool.phase === 'progress') {
            // 工具执行过程输出:逐行追加到 detail(作为执行日志),状态保持"执行中"
            item.classList.remove('pending', 'done', 'error');
            item.classList.add('running');
            statusEl.textContent = '⟳ 执行中…';
            if (tool.detail) {
                detailEl.textContent = detailEl.textContent
                    ? detailEl.textContent + '\n' + tool.detail
                    : tool.detail;
            }
            this.scrollToBottom();
            return;
        }

        const phases = {
            intent: { cls: 'pending', text: '⏳ 等待执行' },
            running: { cls: 'running', text: '⟳ 执行中…' },
            done: { cls: 'done', text: '✓ 完成' },
            error: { cls: 'error', text: '✗ 失败' },
        };
        const p = phases[tool.phase] || phases.intent;
        item.className = `tool-item ${p.cls}`;
        statusEl.textContent = p.text;
        // done/error:仅在无执行日志累积(detail 为空)时写入结果,避免覆盖 progress 已展示的过程输出
        if (tool.detail && !detailEl.textContent.trim()) detailEl.textContent = tool.detail;
        this.scrollToBottom();
    },

    // 流式收尾提示(truncated/content_filter/incomplete):在气泡内独立渲染,不覆盖回答内容。
    // 同一 code 只保留一条。
    updateStreamingNotice(msgId, notice) {
        const container = this.els.messageList;
        const msgEl = container.querySelector(`.message[data-msg-id="${msgId}"]`);
        if (!msgEl || !notice || !notice.code) return;
        const bubble = msgEl.querySelector('.bubble');
        if (!bubble) return;

        let list = bubble.querySelector('.notice-list');
        if (!list) {
            list = document.createElement('div');
            list.className = 'notice-list';
            bubble.appendChild(list);
        }
        let item = list.querySelector(`.stream-notice[data-code="${notice.code}"]`);
        const isAlert = notice.level === 'error'; // 错误级告警:用户可自行处理的错误(欠费/密钥/内容合规等)
        if (!item) {
            item = document.createElement('div');
            item.dataset.code = notice.code;
            list.appendChild(item);
        }
        item.className = isAlert ? 'stream-notice alert-error' : 'stream-notice';
        item.textContent = `${isAlert ? '⛔' : '⚠️'} ${notice.msg || ''}`;
        this.scrollToBottom();
    },

    /* ========== 历史会话抽屉 ========== */

    isHistoryOpen() {
        return this.els.historyDrawer.classList.contains('open');
    },

    toggleHistory() {
        if (this.isHistoryOpen()) this.closeHistory();
        else this.openHistory();
    },

    /** 打开抽屉：立即展开，并向后端拉取当前用户的会话主题（最近 15 条，倒序） */
    openHistory() {
        this.els.historyDrawer.classList.add('open');
        this.els.historyDrawer.setAttribute('aria-hidden', 'false');
        this.els.historyOverlay.classList.add('active');
        this.els.btnHistory.classList.add('active');
        this._loadSessions();
    },

    closeHistory() {
        this.els.historyDrawer.classList.remove('open');
        this.els.historyDrawer.setAttribute('aria-hidden', 'true');
        this.els.historyOverlay.classList.remove('active');
        this.els.btnHistory.classList.remove('active');
        this._sessionsReqId += 1; // 关闭后丢弃在途请求结果
    },

    /** 拉取会话主题列表；失败时降级展示本地会话，避免抽屉空白 */
    async _loadSessions() {
        const reqId = ++this._sessionsReqId;
        this._sessions = null;
        this._sessionsError = '';
        this.renderHistory(true);

        try {
            const list = await this.handlers.onLoadSessions?.();
            if (reqId !== this._sessionsReqId) return;
            this._sessions = this._normalizeSessions(list);
        } catch (err) {
            if (reqId !== this._sessionsReqId) return;
            this._sessionsError = err?.message || '加载历史会话失败';
            console.warn('[Sessions] 获取会话列表失败，使用本地会话数据：', err);
        }
        this.renderHistory();
    },

    /**
     * 渲染历史会话列表。
     * - 数据源为后端 GetSessions 返回的会话主题（TSession），按 BeginTimestamp 倒序取前 HISTORY_LIMIT 条；
     * - 后端不可用时降级为本地会话（localStorage），并给出提示。
     * @param {boolean} [loading] true 时渲染加载占位
     */
    renderHistory(loading = false) {
        const list = this.els.historyList;
        if (!list) return;

        if (loading) {
            list.innerHTML = '<div class="history-empty">正在加载历史会话…</div>';
            return;
        }

        const sessions = this._sessions !== null
            ? this._sessions
            : this._localSessionsFallback();

        const items = sessions
            .slice()
            .sort((a, b) => (b.time || 0) - (a.time || 0))
            .slice(0, HISTORY_LIMIT);

        const notice = this._sessionsError
            ? `<div class="history-notice">⚠️ ${escapeHtml(this._sessionsError)}，已显示本地记录</div>`
            : '';

        if (items.length === 0) {
            list.innerHTML = `${notice}<div class="history-empty">暂无历史会话</div>`;
            return;
        }

        list.innerHTML = notice + items.map(item => {
            const active = item.id === State.currentConvId;
            const local = State.conversations.some(c => c.id === item.id);
            return `
                <div class="history-item${active ? ' active' : ''}">
                    <button type="button" class="history-item-main" data-conv-id="${escapeHtml(item.id)}" data-conv-title="${escapeHtml(item.title)}" data-conv-agent="${escapeHtml(item.agentId || '')}">
                        <span class="history-item-title">${escapeHtml(truncate(item.title || '新对话', 24))}</span>
                        <span class="history-item-meta">${escapeHtml(item.metaText)}</span>
                    </button>
                    ${local ? `<button type="button" class="history-item-del" data-conv-delete="${escapeHtml(item.id)}" title="移除本地记录">🗑</button>` : ''}
                </div>
            `;
        }).join('');
    },

    /** 后端 TSession → 抽屉视图项 { id, title, time, metaText } */
    _normalizeSessions(list) {
        return (Array.isArray(list) ? list : []).map(session => {
            const id = String(session?.SessionId ?? session?.sessionId ?? '');
            const beginTs = Number(session?.BeginTimestamp ?? session?.beginTimestamp ?? 0);
            const time = beginTs > 0 ? beginTs * 1000 : (Number(session?.updatedAt) || 0);
            const ended = Boolean(session?.Ended ?? session?.ended);
            const dateText = time ? formatDate(time) : '未知时间';
            const metaText = ended ? `${dateText} · 已结束` : dateText;
            return {
                id,
                title: String(session?.SessionName ?? session?.sessionName ?? '新对话'),
                // 会话绑定的智能体：选定该会话后，后续消息沿用同一 Agent
                agentId: String(session?.AgentId ?? session?.agentId ?? ''),
                time,
                metaText,
            };
        }).filter(item => item.id);
    },

    /** 从抽屉列表中移除一条会话（仅影响当前展示，后端记录仍在） */
    removeSessionFromList(convId) {
        if (!Array.isArray(this._sessions)) return;
        this._sessions = this._sessions.filter(item => item.id !== convId);
        this.renderHistory();
    },

    /** 后端不可用时的本地会话（localStorage）映射，字段与后端会话视图一致 */
    _localSessionsFallback() {
        return State.conversations.map(conv => {
            const count = Array.isArray(conv.messages) ? conv.messages.length : 0;
            return {
                id: conv.id,
                title: conv.title || '新对话',
                agentId: conv.agentId || '',
                time: conv.updatedAt || conv.createdAt || 0,
                metaText: `${formatDate(conv.updatedAt || conv.createdAt || Date.now())} · ${count} 条消息`,
            };
        });
    },

    scrollToBottom() {
        const container = this.els.messageList;
        requestAnimationFrame(() => {
            container.scrollTop = container.scrollHeight;
        });
    },

    scrollToTop() {
        const container = this.els.messageList;
        requestAnimationFrame(() => {
            container.scrollTop = 0;
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
        // 用户信息展示由主页(HomePage)负责，聊天面板内无需操作。
    },

    updateChatTitle(title, agentId) {
        // 防护：视图缺失 #chatTitle 时不抛错（避免拖垮登录/会话流程）
        if (this.els.chatTitle) {
            this.els.chatTitle.textContent = title || '新对话';
        }
        this.updateChatAgent(agentId);
    },

    /** 会话绑定的智能体徽标：有 agentId 才显示，便于确认历史会话将继续由哪个 Agent 处理 */
    updateChatAgent(agentId) {
        const badge = this.els.chatAgent;
        if (!badge) return;
        const id = String(agentId || '').trim();
        badge.textContent = id ? `🧩 ${id}` : '';
        badge.hidden = !id;
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

    /* ========== 历史轮次（选中历史会话时展示） ========== */

    /** 轮次加载中占位 */
    showTurnLoading() {
        this._clearTurns();
        this.els.emptyState.style.display = 'none';
        const box = document.createElement('div');
        box.className = 'turn-loading';
        box.textContent = '正在加载历史对话…';
        this.els.messageList.appendChild(box);
    },

    /** 轮次加载失败提示 */
    showTurnError(message) {
        this._clearTurns();
        this.els.emptyState.style.display = 'none';
        const box = document.createElement('div');
        box.className = 'turn-error';
        box.textContent = `⚠️ ${message || '加载历史对话失败'}`;
        this.els.messageList.appendChild(box);
    },

    /**
     * 渲染某会话的历史对话（最近 TURNS_LIMIT 轮，按时间倒序：最新一轮在最上方）。
     *
     * 期望后端返回的单个轮次结构（字段大小写不敏感，前端同时兼容常见别名）：
     * {
     *   Seq: number,            // 轮次序号（从 1 开始）
     *   ChatTime: number,       // 提问时间，Unix 秒（毫秒亦可）
     *   RespTime: number,       // 回答时间，Unix 秒（毫秒亦可）
     *   Status: 0 | 1 | 2,      // 0=正常 1=中断无产出 2=中断有部分产出
     *   Question: string,       // 用户提问正文
     *   Answer: string,         // 助手回复正文
     *   Reasoning?: string,     // 可选：思考过程（reasoning_content）
     *   Tools?: Array<{         // 可选：本轮工具调用
     *     ToolName: string, ToolResp: string, CallTime: number, RespTime: number
     *   }>
     * }
     * @param {Array<object>} turns 后端轮次原始对象数组
     */
    renderTurns(turns) {
        this._clearTurns();
        const items = this._normalizeTurns(turns)
            .sort((a, b) => (b.time - a.time) || (b.seq - a.seq))
            .slice(0, TURNS_LIMIT);

        if (!items.length) {
            this.els.emptyState.style.display = 'flex';
            return;
        }
        // 中断轮即使没有正文也承载信息（"回答已中断：原因"），不能因此被当作"空卡片"过滤掉
        const hasContent = (turn) => Boolean(turn.question || turn.answer || turn.reasoning || turn.tools.length || turn.interrupted);
        if (!items.some(hasContent)) {
            // 后端只返回轮次元数据（没有 Question/Answer 正文），一次性说清楚，避免一排空卡片
            this.els.emptyState.style.display = 'none';
            const box = document.createElement('div');
            box.className = 'turn-empty-state';
            box.textContent = `服务端返回了 ${items.length} 轮记录，但没有对话正文（Question/Answer 字段为空），暂无法展示聊天内容。`;
            this.els.messageList.appendChild(box);
            return;
        }

        this.els.emptyState.style.display = 'none';
        const fragment = document.createDocumentFragment();
        items.forEach(turn => fragment.appendChild(this._makeTurnNode(turn)));
        this.els.messageList.appendChild(fragment);
        this.scrollToTop(); // 倒序展示：最新一轮在顶部
    },

    /**
     * TTurn → 视图项。
     * 时间统一转成毫秒；正文从多个候选字段里取第一个非空值，便于后端循序补充字段。
     */
    _normalizeTurns(list) {
        const STATUS_TEXT = { 0: '正常', 1: '已中断', 2: '部分中断' };
        const pick = (obj, keys) => {
            for (const key of keys) {
                const value = obj?.[key];
                if (typeof value === 'string' && value.trim()) return value;
            }
            return '';
        };

        return (Array.isArray(list) ? list : []).map(turn => {
            const chatTime = this._toMs(turn?.ChatTime ?? turn?.chatTime);
            const respTime = this._toMs(turn?.RespTime ?? turn?.respTime);
            const status = Number(turn?.Status ?? turn?.status ?? 0);
            const reason = Number(turn?.InterruptReason ?? turn?.interruptReason ?? 0);
            const tools = Array.isArray(turn?.Tools) ? turn.Tools
                : Array.isArray(turn?.tools) ? turn.tools
                : Array.isArray(turn?.ToolList) ? turn.ToolList
                : [];

            // 兼容后端把一轮拆成 messages 数组返回：取最后一条 user / assistant
            const messages = Array.isArray(turn?.Messages) ? turn.Messages
                : Array.isArray(turn?.messages) ? turn.messages
                : [];
            const roleOf = (m) => String(m?.Role ?? m?.role ?? '').toLowerCase();
            const textOf = (m) => String(m?.Content ?? m?.content ?? m?.Text ?? m?.text ?? '');
            const lastUser = [...messages].reverse().find(m => roleOf(m) === 'user');
            const lastAssistant = [...messages].reverse().find(m => roleOf(m) === 'assistant');

            // 后端 AnswerInterrupted=助手记录是 assistant:interrupted（只产出了部分回复）。
            // 以它为准补充中断判定，并回落到「部分中断」，避免正文被截断却看不出原因。
            const answerInterrupted = turn?.AnswerInterrupted === true || turn?.answerInterrupted === true;
            const interrupted = TURN_INTERRUPTED.has(status) || answerInterrupted;
            const effStatus = TURN_INTERRUPTED.has(status) ? status : (answerInterrupted ? 2 : status);

            return {
                seq: Number(turn?.Seq ?? turn?.seq ?? 0),
                time: chatTime || respTime || 0,
                respTime,
                status,
                interrupted,
                statusText: STATUS_TEXT[status] || '',
                interruptText: this._interruptText(effStatus, reason),
                question: pick(turn, ['Question', 'question', 'ReqMsg', 'reqMsg', 'UserMsg', 'userMsg', 'Prompt', 'prompt']) || (lastUser ? textOf(lastUser) : ''),
                answer: pick(turn, ['Answer', 'answer', 'Reply', 'reply', 'RespMsg', 'respMsg', 'AssistantMsg', 'assistantMsg', 'Content', 'content']) || (lastAssistant ? textOf(lastAssistant) : ''),
                reasoning: pick(turn, ['Reasoning', 'reasoning', 'ReasoningText', 'reasoningText', 'ReasoningContent', 'reasoningContent']),
                tools: tools.map(tool => ({
                    name: String(tool?.ToolName ?? tool?.toolName ?? tool?.name ?? '工具'),
                    detail: String(tool?.ToolResp ?? tool?.toolResp ?? tool?.detail ?? tool?.ToolResp ?? ''),
                    time: this._toMs(tool?.CallTime ?? tool?.callTime) || this._toMs(tool?.RespTime ?? tool?.respTime),
                })),
            };
        }).filter(item => item.seq > 0 || item.time > 0);
    },

    /** 中断文案：结合 status（有没有产出）和 reason（谁打断的）生成一句人读说明 */
    _interruptText(status, reason) {
        if (!TURN_INTERRUPTED.has(status)) return '';
        // reason=0 表示源头没记录中断来源（例如只靠 AnswerInterrupted 判定出来），此时不加原因后缀
        const reasonText = reason in INTERRUPT_REASON_TEXT ? INTERRUPT_REASON_TEXT[reason] : '原因未知';
        const suffix = reasonText ? `：${reasonText}` : '';
        if (status === 1) return `\u23F9\uFE0E 回答已中断${suffix}（本轮未产生回复）`;
        if (status === 3) return `\u26A0\uFE0E 回复未保存${suffix}`;
        return `\u23F9\uFE0E 回答已中断${suffix}`;
    },

    /** 时间戳统一转为毫秒（后端可能给秒或毫秒） */
    _toMs(value) {
        const num = Number(value) || 0;
        if (num <= 0) return 0;
        return num > 1e12 ? num : num * 1000;
    },

    /** 单轮节点：提问气泡 + 回答气泡（不显示"第 N 轮"） */
    _makeTurnNode(turn) {
        const wrap = document.createElement('div');
        wrap.className = 'turn-item';

        const body = document.createElement('div');
        body.className = 'turn-body';
        if (turn.question) {
            body.appendChild(this._makeTurnMessage('user', turn.question));
        }
        body.appendChild(this._makeAssistantTurn(turn));

        if (turn.time) {
            const meta = document.createElement('div');
            meta.className = 'turn-meta';
            meta.textContent = `${formatDate(turn.time)} ${formatTime(turn.time)}`;
            body.appendChild(meta);
        }

        wrap.appendChild(body);
        return wrap;
    },

    /** 气泡行（用户提问右对齐 / 助手回答左对齐） */
    _makeTurnMessage(role, text) {
        const row = document.createElement('div');
        row.className = `turn-msg ${role}`;

        const avatar = document.createElement('div');
        avatar.className = 'turn-avatar';
        avatar.textContent = role === 'user'
            ? (State.user ? getInitials(State.user.username) : 'U')
            : 'AI';

        const bubble = document.createElement('div');
        bubble.className = 'turn-bubble';
        bubble.innerHTML = this._escapeHtml(text).replace(/\n/g, '<br/>');

        if (role === 'user') {
            row.appendChild(bubble);
            row.appendChild(avatar);
        } else {
            row.appendChild(avatar);
            row.appendChild(bubble);
        }
        return row;
    },

    /**
     * 助手回答：
     * - 正常结束：展示思考过程（可选）、工具卡片（可选）与回答正文；
     * - 被中断：展示已产出的部分回答（若有），并明确标注中断原因；
     * - 完全无产出：仅展示中断说明。
     */
    _makeAssistantTurn(turn) {
        const row = this._makeTurnMessage('assistant', '');
        const bubble = row.querySelector('.turn-bubble');

        if (turn.reasoning) {
            const details = document.createElement('details');
            details.className = 'reasoning-wrap';
            const summary = document.createElement('summary');
            summary.textContent = `${State.agentEmoji || '🤖'} 思考过程`;
            const text = document.createElement('div');
            text.className = 'reasoning-text';
            text.innerHTML = this._escapeHtml(turn.reasoning).replace(/\n/g, '<br/>');
            details.appendChild(summary);
            details.appendChild(text);
            bubble.appendChild(details);
        }

        if (turn.tools.length) {
            const list = document.createElement('div');
            list.className = 'tool-list';
            turn.tools.forEach(tool => {
                const item = document.createElement('div');
                item.className = 'tool-item done';
                const header = document.createElement('div');
                header.className = 'tool-header';
                header.innerHTML = `<span class="tool-icon">⚙️</span><span class="tool-name">${this._escapeHtml(tool.name)}</span>`;
                if (tool.time) {
                    const time = document.createElement('span');
                    time.className = 'tool-status';
                    time.textContent = formatTime(tool.time);
                    header.appendChild(time);
                }
                item.appendChild(header);
                if (tool.detail) {
                    const detail = document.createElement('div');
                    detail.className = 'tool-detail';
                    detail.textContent = tool.detail;
                    item.appendChild(detail);
                }
                list.appendChild(item);
            });
            bubble.appendChild(list);
        }

        if (turn.answer) {
            const content = document.createElement('div');
            content.className = 'turn-answer';
            content.innerHTML = this._escapeHtml(turn.answer).replace(/\n/g, '<br/>');
            bubble.appendChild(content);
        }

        if (turn.interrupted) {
            // 中断：以醒目标记替代"完整回答"，并给出中断来源
            const tag = document.createElement('div');
            tag.className = 'interrupted-tag turn-interrupt';
            tag.textContent = turn.interruptText;
            bubble.appendChild(tag);
        } else if (!turn.answer) {
            const empty = document.createElement('div');
            empty.className = 'turn-empty';
            empty.textContent = '该轮暂无回复内容';
            bubble.appendChild(empty);
        }
        return row;
    },

    _clearTurns() {
        this.els.messageList.querySelectorAll('.turn-item, .turn-loading, .turn-error, .turn-empty-state').forEach(el => el.remove());
    },

    /** 设置空状态提示语；不传则恢复默认文案 */
    setEmptyStateHint(text) {
        const p = this.els.emptyState?.querySelector('p');
        if (p) p.textContent = text || DEFAULT_EMPTY_HINT;
    },

    reset() {
        this.closeHistory();
        this.setEmptyStateHint();
        this._sessions = null;      // 切换登录用户后按新用户重新拉取
        this._sessionsError = '';
        if (this.els.chatTitle) {
            this.els.chatTitle.textContent = '新对话';
        }
        this.els.historyList.innerHTML = '';
        this._clearTurns();
        this.els.messageList.querySelectorAll('.message').forEach(el => el.remove());
        this.els.emptyState.style.display = 'flex';
    },
};

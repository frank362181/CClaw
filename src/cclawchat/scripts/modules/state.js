import { CONFIG } from './config.js';
import { genId, safeJSON, newSessionId, normalizeSessionId } from '../utils/helper.js';

function normalizeConversation(conv) {
    if (!conv || typeof conv !== 'object' || !Array.isArray(conv.messages)) return null;
    return {
        id: String(conv.id || genId()),
        title: String(conv.title || '新对话'),
        messages: conv.messages
            .filter(msg => msg && (msg.role === 'user' || msg.role === 'assistant'))
            .slice(-CONFIG.MAX_MESSAGES_PER_CONV)
            .map(msg => ({
                id: String(msg.id || genId()),
                role: msg.role,
                content: String(msg.content ?? ''),
                timestamp: Number(msg.timestamp) || Date.now(),
                interrupted: Boolean(msg.interrupted),
            })),
        createdAt: Number(conv.createdAt) || Date.now(),
        updatedAt: Number(conv.updatedAt) || Date.now(),
        // 会话绑定的 Agent（由 GetSessions 带回）：必须持久化，否则刷新后无法续用同一 Agent
        agentId: String(conv.agentId || ''),
        // true=仅服务端存在、本地无历史缓存的占位会话（GetSessionTurns 未实现，历史不可回放）
        placeholder: Boolean(conv.placeholder),
    };
}

export const State = {
    user: null,
    conversations: [],
    currentConvId: null,
    isStreaming: false,
    // 当前 Agent 展示用表情(思考过程等 UI 图标的来源);由 HomePage 加载智能体列表后按默认 Agent 同步
    agentEmoji: '🤖',

    _storageKey(username = this.user?.username) {
        if (!username) return null;
        return CONFIG.STORAGE_KEY_PREFIX + encodeURIComponent(username.toLowerCase());
    },

    loadSession() {
        const raw = sessionStorage.getItem(CONFIG.USER_KEY);
        const session = raw ? safeJSON(raw, null) : null;
        if (!session || typeof session.username !== 'string' || typeof session.token !== 'string') {
            this.user = null;
            return null;
        }
        if (session.expiresAt && Date.now() >= session.expiresAt) {
            sessionStorage.removeItem(CONFIG.USER_KEY);
            this.user = null;
            return null;
        }
        this.user = session;
        this.loadConversations();
        return session;
    },

    loadConversations() {
        this.conversations = [];
        this.currentConvId = null;
        const key = this._storageKey();
        if (!key) return;
        try {
            const data = safeJSON(localStorage.getItem(key), null);
            if (!data || !Array.isArray(data.conversations)) return;
            this.conversations = data.conversations.map(normalizeConversation).filter(Boolean);
            this.currentConvId = this.conversations.some(c => c.id === data.currentConvId)
                ? data.currentConvId
                : this.conversations[0]?.id || null;
        } catch (error) {
            console.warn('Conversation state load failed:', error);
        }
    },

    save() {
        const key = this._storageKey();
        if (!key) return;
        localStorage.setItem(key, JSON.stringify({
            version: 2,
            conversations: this.conversations,
            currentConvId: this.currentConvId,
        }));
    },

    saveSession() {
        if (!this.user) {
            sessionStorage.removeItem(CONFIG.USER_KEY);
            return;
        }
        sessionStorage.setItem(CONFIG.USER_KEY, JSON.stringify(this.user));
    },

    getCurrentConversation() {
        return this.conversations.find(c => c.id === this.currentConvId) || null;
    },

    /** 确保指定 id 的会话存在（服务端已有、本地缺失时创建占位，便于继续同一 session）；返回会话对象 */
    ensureConversation(convId, title, agentId) {
        const agent = String(agentId || '').trim();
        const existing = this.conversations.find(c => c.id === convId);
        if (existing) {
            if (agent) existing.agentId = agent;
            this.currentConvId = existing.id;
            this.save();
            return existing;
        }

        // 历史数据兼容：旧版本本地会话用裸 UUID，后端落库时会加单位编码前缀，
        // 这里按同样规则反查，命中则沿用原会话（避免同一会话出现两条本地记录）。
        const legacy = this.conversations.find(c => `${normalizeSessionId(this.user?.comCode)}${normalizeSessionId(c.id)}` === convId);
        if (legacy) {
            legacy.id = convId; // 统一为后端权威 ID
            if (agent) legacy.agentId = agent;
            this.currentConvId = legacy.id;
            this.save();
            return legacy;
        }
        const conv = {
            id: String(convId),
            title: title || '新对话',
            agentId: agent, // 后端会话绑定的 Agent；为空表示未绑定（交由后端路由）
            messages: [],
            createdAt: Date.now(),
            updatedAt: Date.now(),
            placeholder: true,
        };
        this.conversations.unshift(conv);
        this.currentConvId = conv.id;
        this.save();
        return conv;
    },

    /** 切换当前会话；返回是否切换成功 */
    setCurrentConversation(convId) {
        const conv = this.conversations.find(c => c.id === convId);
        if (!conv) return false;
        this.currentConvId = conv.id;
        this.save();
        return true;
    },

    /** 删除指定会话；若删除的是当前会话则切到最近一条（无则新建），返回是否删除成功 */
    deleteConversation(convId) {
        const index = this.conversations.findIndex(c => c.id === convId);
        if (index < 0) return false;
        this.conversations.splice(index, 1);
        if (this.currentConvId === convId) {
            const next = this.conversations[0];
            if (next) {
                this.currentConvId = next.id;
                this.save();
            } else {
                this.createConversation('新对话'); // 内部会保存并更新 currentConvId
            }
        } else {
            this.save();
        }
        return true;
    },

    /** 绑定/更新某会话的 Agent（历史会话选定后固定使用它继续对话） */
    setConversationAgent(convId, agentId) {
        const conv = this.conversations.find(c => c.id === convId);
        if (!conv) return null;
        const agent = String(agentId || '').trim();
        if (conv.agentId === agent) return conv;
        conv.agentId = agent;
        this.save();
        return conv;
    },

    createConversation(title) {
        const conv = { id: newSessionId(this.user?.comCode), title: title || '新对话', messages: [], createdAt: Date.now(), updatedAt: Date.now() };
        this.conversations.unshift(conv);
        this.currentConvId = conv.id;
        this.save();
        return conv;
    },

    addMessage(convId, role, content, id, { persist = true } = {}) {
        const conv = this.conversations.find(c => c.id === convId);
        if (!conv || (role !== 'user' && role !== 'assistant')) return null;
        const msg = { id: id || genId(), role, content: String(content ?? ''), timestamp: Date.now() };
        conv.messages.push(msg);
        conv.placeholder = false; // 已有本地消息后不再按占位会话处理
        if (conv.messages.length > CONFIG.MAX_MESSAGES_PER_CONV) {
            conv.messages.splice(0, conv.messages.length - CONFIG.MAX_MESSAGES_PER_CONV);
        }
        conv.updatedAt = Date.now();
        if (conv.messages.length === 1 && role === 'user') {
            conv.title = msg.content.slice(0, 30) + (msg.content.length > 30 ? '…' : '') || '新对话';
        }
        if (persist) this.save();
        return msg;
    },

    updateMessageContent(convId, msgId, newContent, { persist = true } = {}) {
        const conv = this.conversations.find(c => c.id === convId);
        const msg = conv?.messages.find(item => item.id === msgId);
        if (!msg) return false;
        msg.content = String(newContent ?? '');
        conv.updatedAt = Date.now();
        if (persist) this.save();
        return true;
    },

    // 记录流式"思考/准备"阶段的状态文案(仅内存,不持久化到 localStorage)。
    // 内容尚未到达时抽屉被收起再展开会触发 renderMessages 全量重渲染,
    // 该字段用于在此场景下恢复 thinking 指示;内容到达/结束/取消时以空字符串清除。
    updateMessageStatus(convId, msgId, statusText, { persist = false } = {}) {
        const conv = this.conversations.find(c => c.id === convId);
        const msg = conv?.messages.find(item => item.id === msgId);
        if (!msg) return false;
        msg.statusText = String(statusText ?? '');
        if (persist) this.save();
        return true;
    },

    // 记录流式思考过程(reasoning_content)的增量文本(仅内存,不持久化)。
    // 与 statusText 类似,供抽屉重渲染/消息重渲染时恢复"思考过程"折叠块。
    updateMessageReasoning(convId, msgId, reasoningText, { persist = false } = {}) {
        const conv = this.conversations.find(c => c.id === convId);
        const msg = conv?.messages.find(item => item.id === msgId);
        if (!msg) return false;
        msg.reasoningText = String(reasoningText ?? '');
        if (persist) this.save();
        return true;
    },

    // 记录工具调用进度到消息(仅内存,不持久化到 localStorage)。
    // 同一 seq 的事件更新同一条目;供抽屉重渲染/消息重渲染时恢复工具调用卡片。
    updateMessageTool(convId, msgId, tool, { persist = false } = {}) {
        const conv = this.conversations.find(c => c.id === convId);
        const msg = conv?.messages.find(item => item.id === msgId);
        if (!msg || !tool || tool.seq == null) return false;
        if (!Array.isArray(msg.toolLog)) msg.toolLog = [];
        const idx = msg.toolLog.findIndex(t => t.seq === tool.seq);
        const entry = {
            phase: tool.phase,
            seq: tool.seq,
            name: String(tool.name || ''),
            detail: String(tool.detail || ''),
        };
        if (idx >= 0) {
            // 同一 seq 的后续事件可能**不带** detail(done 只报完成态,不再回传结果 JSON):
            // 此时保留已累积的执行进度文本,否则重渲染时进度日志会被清空(与流式实时视图不一致)。
            if (!entry.detail) entry.detail = String(msg.toolLog[idx].detail || '');
            msg.toolLog[idx] = entry;
        } else {
            msg.toolLog.push(entry);
        }
        if (persist) this.save();
        return true;
    },

    // 记录流式收尾提示(truncated/content_filter/incomplete;仅内存,不持久化)。
    // 同一 code 去重;供抽屉重渲染/消息重渲染时恢复提示。
    updateMessageNotice(convId, msgId, notice, { persist = false } = {}) {
        const conv = this.conversations.find(c => c.id === convId);
        const msg = conv?.messages.find(item => item.id === msgId);
        if (!msg || !notice || !notice.code) return false;
        if (!Array.isArray(msg.notices)) msg.notices = [];
        const entry = { code: String(notice.code), msg: String(notice.msg || '') };
        if (notice.level) entry.level = String(notice.level); // error 级告警需保留样式信息(重渲染时使用)
        const idx = msg.notices.findIndex(n => n.code === entry.code);
        if (idx >= 0) msg.notices[idx] = entry;
        else msg.notices.push(entry);
        if (persist) this.save();
        return true;
    },

    // 标记消息为“已中断”（用户点击中断按钮停止流式聊天）。
    // 持久化，便于重渲染/刷新后仍展示中断标记。
    markMessageInterrupted(convId, msgId) {
        const conv = this.conversations.find(c => c.id === convId);
        const msg = conv?.messages.find(item => item.id === msgId);
        if (!msg) return false;
        msg.interrupted = true;
        this.save();
        return true;
    },

    clearCurrentConversation() {
        const conv = this.getCurrentConversation();
        if (!conv) return false;
        conv.messages = [];
        conv.title = '新对话';
        conv.updatedAt = Date.now();
        this.save();
        return true;
    },

    login(username, token, { displayName, expiresAt, comCode } = {}) {
        if (!username || !token) throw new Error('登录响应缺少用户名或令牌');
        this.user = {
            username,
            displayName: displayName || username,
            token,
            expiresAt: Number(expiresAt) || null,
            comCode: comCode || null,
        };
        this.saveSession();
        this.loadConversations();
        if (this.conversations.length === 0) this.createConversation('新对话');
        return this.user;
    },

    logout() {
        this.user = null;
        this.conversations = [];
        this.currentConvId = null;
        this.isStreaming = false;
        sessionStorage.removeItem(CONFIG.USER_KEY);
    },

    isLoggedIn() {
        return Boolean(this.user?.username && this.user?.token && (!this.user.expiresAt || Date.now() < this.user.expiresAt));
    },
};

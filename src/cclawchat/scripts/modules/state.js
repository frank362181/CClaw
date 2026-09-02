import { CONFIG } from './config.js';
import { genId, safeJSON } from '../utils/helper.js';

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

    createConversation(title) {
        const conv = { id: genId(), title: title || '新对话', messages: [], createdAt: Date.now(), updatedAt: Date.now() };
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
        if (idx >= 0) msg.toolLog[idx] = entry;
        else msg.toolLog.push(entry);
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

    login(username, token, { displayName, expiresAt } = {}) {
        if (!username || !token) throw new Error('登录响应缺少用户名或令牌');
        this.user = { username, displayName: displayName || username, token, expiresAt: Number(expiresAt) || null };
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

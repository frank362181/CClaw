/**
 * ============================================================
 * 状态管理
 * ============================================================
 */

import { CONFIG } from './config.js?v=1.0.4';
import { genId, safeJSON } from '../utils/helper.js?v=1.0.4';

export const State = {
    user: null,
    conversations: [],
    currentConvId: null,
    isStreaming: false,
    _initialized: false,

    load() {
        try {
            const raw = localStorage.getItem(CONFIG.STORAGE_KEY);
            if (raw) {
                const data = safeJSON(raw, null);
                if (data && Array.isArray(data.conversations)) {
                    this.conversations = data.conversations;
                    this.currentConvId = data.currentConvId || null;
                }
            }
            const userRaw = localStorage.getItem(CONFIG.USER_KEY);
            if (userRaw) {
                this.user = safeJSON(userRaw, null);
            }
        } catch (e) {
            console.warn('State load error:', e);
        }
        this._initialized = true;
    },

    save() {
        try {
            localStorage.setItem(CONFIG.STORAGE_KEY, JSON.stringify({
                conversations: this.conversations,
                currentConvId: this.currentConvId,
            }));
            if (this.user) {
                localStorage.setItem(CONFIG.USER_KEY, JSON.stringify(this.user));
            } else {
                localStorage.removeItem(CONFIG.USER_KEY);
            }
        } catch (e) {
            console.warn('State save error:', e);
        }
    },

    getCurrentConversation() {
        if (!this.currentConvId) return null;
        return this.conversations.find(c => c.id === this.currentConvId) || null;
    },

    getMessages(convId) {
        const conv = this.conversations.find(c => c.id === convId);
        return conv ? conv.messages : [];
    },

    createConversation(title) {
        const conv = {
            id: genId(),
            title: title || '新对话',
            messages: [],
            createdAt: Date.now(),
            updatedAt: Date.now(),
        };
        this.conversations.unshift(conv);
        this.currentConvId = conv.id;
        this.save();
        return conv;
    },

    deleteConversation(convId) {
        const idx = this.conversations.findIndex(c => c.id === convId);
        if (idx === -1) return false;
        this.conversations.splice(idx, 1);
        if (this.currentConvId === convId) {
            this.currentConvId = this.conversations.length > 0 ? this.conversations[0].id : null;
        }
        this.save();
        return true;
    },

    addMessage(convId, role, content, id) {
        const conv = this.conversations.find(c => c.id === convId);
        if (!conv) return null;
        const msg = {
            id: id || genId(),
            role: role,
            content: content,
            timestamp: Date.now(),
        };
        conv.messages.push(msg);
        conv.updatedAt = Date.now();
        if (conv.messages.length === 1 && role === 'user') {
            const title = content.slice(0, 30) + (content.length > 30 ? '…' : '');
            conv.title = title || '新对话';
        }
        this.save();
        return msg;
    },

    updateMessageContent(convId, msgId, newContent) {
        const conv = this.conversations.find(c => c.id === convId);
        if (!conv) return false;
        const msg = conv.messages.find(m => m.id === msgId);
        if (!msg) return false;
        msg.content = newContent;
        conv.updatedAt = Date.now();
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

    login(username, token) {
        this.user = {
            id: genId(),
            username: username || CONFIG.DEFAULT_USERNAME,
            token: token || 'mock_token_' + genId(),
            loggedInAt: Date.now(),
        };
        if (this.conversations.length === 0) {
            this.createConversation('新对话');
        } else if (!this.currentConvId) {
            this.currentConvId = this.conversations[0].id;
        }
        this.save();
        return this.user;
    },

    logout() {
        this.user = null;
        localStorage.removeItem(CONFIG.USER_KEY);
    },

    isLoggedIn() {
        return this.user !== null && this.user.username !== undefined;
    },
};
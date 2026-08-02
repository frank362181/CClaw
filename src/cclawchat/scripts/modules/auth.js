/**
 * ============================================================
 * 认证管理
 * ============================================================
 */

import { API } from './api.js';
import { State } from './state.js';
import { LoginPage } from './login.js';
import { ChatPage } from './chat.js';
import { StreamEngine } from './stream.js';

export const Auth = {
    async login(username, password) {
        try {
            const result = await API.login(username, password);
            if (result.success) {
                State.login(result.user.username, result.token);
                ChatPage.updateUserInfo(result.user.username);
                ChatPage.renderConversations(State.conversations, State.currentConvId);
                const conv = State.getCurrentConversation();
                if (conv) {
                    ChatPage.renderMessages(conv);
                    ChatPage.updateChatTitle(conv.title);
                } else {
                    const newConv = State.createConversation('新对话');
                    ChatPage.renderMessages(newConv);
                    ChatPage.updateChatTitle(newConv.title);
                    ChatPage.renderConversations(State.conversations, State.currentConvId);
                }
                ChatPage.setInputEnabled(true);
                ChatPage.updateSendButton();
                // 页面切换：隐藏登录组件，展示聊天组件
                LoginPage.hide();
                ChatPage.show();
                ChatPage.focusInput();
                return true;
            }
            return false;
        } catch (err) {
            LoginPage.showError(err.message || '登录失败，请重试');
            return false;
        }
    },

    async logout() {
        if (StreamEngine.isActive()) {
            StreamEngine.cancel();
        }
        try {
            if (State.user?.token) {
                await API.logout(State.user.token);
            }
        } catch (e) { /* ignore */ }
        State.logout();
        // 页面切换：隐藏聊天组件、重置状态，并展示登录组件。
        // 使用 try/finally 确保即使异常也能回到登录页。
        try {
            ChatPage.hide();
            ChatPage.reset();
            ChatPage.setInputEnabled(false);
        } finally {
            LoginPage.show();
            LoginPage.hideError();
        }
    },

    restore() {
        State.load();
        if (State.isLoggedIn()) {
            ChatPage.updateUserInfo(State.user.username);
            if (State.conversations.length === 0) {
                State.createConversation('新对话');
            }
            if (!State.currentConvId) {
                State.currentConvId = State.conversations[0]?.id || null;
            }
            State.save();
            ChatPage.renderConversations(State.conversations, State.currentConvId);
            const conv = State.getCurrentConversation();
            if (conv) {
                ChatPage.renderMessages(conv);
                ChatPage.updateChatTitle(conv.title);
            }
            ChatPage.setInputEnabled(true);
            ChatPage.updateSendButton();
            LoginPage.hide();
            ChatPage.show();
            return true;
        } else {
            ChatPage.hide();
            ChatPage.setInputEnabled(false);
            LoginPage.show();
            return false;
        }
    },
};
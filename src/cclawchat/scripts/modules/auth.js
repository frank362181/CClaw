/**
 * ============================================================
 * 认证管理
 * ============================================================
 */

import { API } from './api.js?v=1.0.8';
import { State } from './state.js?v=1.0.4';
import { LoginPage } from './login.js?v=1.0.4';
import { ChatPage } from './chat.js?v=1.0.6';
import { HomePage } from './home.js?v=1.0.7';
import { StreamEngine } from './stream.js?v=1.0.6';

export const Auth = {
    async login(username, password) {
        try {
            const result = await API.login(username, password);
            if (result.success) {
                State.login(result.user.username, result.token);
                ChatPage.updateUserInfo(result.user.username);
                HomePage.updateUserInfo(result.user.username);
                const conv = State.getCurrentConversation();
                if (conv) {
                    ChatPage.renderMessages(conv);
                    ChatPage.updateChatTitle(conv.title);
                } else {
                    const newConv = State.createConversation('新对话');
                    ChatPage.renderMessages(newConv);
                    ChatPage.updateChatTitle(newConv.title);
                }
                ChatPage.setInputEnabled(true);
                ChatPage.updateSendButton();
                // 页面切换：隐藏登录组件，展示主页（聊天以抽屉形式提供）
                LoginPage.hide();
                HomePage.show();
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
        // 页面切换：隐藏主页与聊天组件、重置状态，并展示登录组件。
        // 使用 try/finally 确保即使异常也能回到登录页。
        try {
            ChatPage.hide();
            ChatPage.reset();
            ChatPage.setInputEnabled(false);
            HomePage.hide();
            HomePage.reset();
        } finally {
            LoginPage.show();
            LoginPage.hideError();
        }
    },

    restore() {
        State.load();
        if (State.isLoggedIn()) {
            ChatPage.updateUserInfo(State.user.username);
            HomePage.updateUserInfo(State.user.username);
            if (State.conversations.length === 0) {
                State.createConversation('新对话');
            }
            if (!State.currentConvId) {
                State.currentConvId = State.conversations[0]?.id || null;
            }
            State.save();
            const conv = State.getCurrentConversation();
            if (conv) {
                ChatPage.renderMessages(conv);
                ChatPage.updateChatTitle(conv.title);
            }
            ChatPage.setInputEnabled(true);
            ChatPage.updateSendButton();
            LoginPage.hide();
            HomePage.show();
            return true;
        } else {
            ChatPage.hide();
            ChatPage.setInputEnabled(false);
            HomePage.hide();
            LoginPage.show();
            return false;
        }
    },
};
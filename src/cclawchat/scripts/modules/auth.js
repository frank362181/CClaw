/**
 * ============================================================
 * 认证管理
 * ============================================================
 */

import { API } from './api.js';
import { State } from './state.js';
import { LoginPage } from './login.js';
import { ChatPage } from './chat.js';
import { HomePage } from './home.js';
import { StreamEngine } from './stream.js';

export const Auth = {
    async login(username, password) {
        // ① 真正的登录失败：网络错误 / 后端返回失败（API.login 失败会 throw）
        let result;
        try {
            result = await API.login(username, password);
        } catch (err) {
            LoginPage.showError(err.message || '登录失败，请重试');
            return false;
        }

        // 防御：返回结构不符合预期（无 success）也按失败处理
        if (!result || result.success !== true) {
            LoginPage.showError('登录失败，请重试');
            return false;
        }

        // ② 持久化登录态：这是登录成功的核心，保存失败视为未登录成功
        try {
            State.login(result.user.userName, result.token, {
                displayName: result.user.displayName,
                expiresAt: result.expiresAt,
                comCode: result.user.comCode,
            });
        } catch (err) {
            console.error('登录状态持久化失败:', err);
            LoginPage.showError('登录状态保存失败，请重试');
            return false;
        }

        // ③ 渲染会话与页面切换：渲染异常只记录日志，不再误报为“登录失败”
        try {
            ChatPage.updateUserInfo(result.user.userName);
            HomePage.updateUserInfo(result.user.userName, result.user.displayName);
            const conv = State.getCurrentConversation();
            if (conv) {
                ChatPage.renderMessages(conv);
                ChatPage.updateChatTitle(conv.title, conv.agentId);
            } else {
                const newConv = State.createConversation('新对话');
                ChatPage.renderMessages(newConv);
                ChatPage.updateChatTitle(newConv.title, newConv.agentId);
            }
            ChatPage.setInputEnabled(true);
            ChatPage.updateSendButton();
        } catch (err) {
            console.error('登录成功但页面渲染异常:', err);
        } finally {
            // 无论渲染是否异常都切到主页，避免卡在登录页（右侧聊天面板常驻）
            LoginPage.hide();
            HomePage.show();
            ChatPage.show();
        }

        return true;
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

    async restore() {
        State.loadSession();
        if (State.isLoggedIn()) {
            try {
                await API.validateSession(State.user.token);
            } catch (error) {
                console.warn('会话验证失败，需要重新登录:', error);
                State.logout();
            }
        }
        if (State.isLoggedIn()) {
            ChatPage.updateUserInfo(State.user.username);
            HomePage.updateUserInfo(State.user.username, State.user.displayName);
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
                ChatPage.updateChatTitle(conv.title, conv.agentId);
            }
            ChatPage.setInputEnabled(true);
            ChatPage.updateSendButton();
            LoginPage.hide();
            HomePage.show();
            ChatPage.show();
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

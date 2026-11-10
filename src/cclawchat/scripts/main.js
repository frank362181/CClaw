/**
 * ============================================================
 * ./scripts/main.js — 主应用控制器 (ES Module)
 * ============================================================
 */

import { State } from './modules/state.js';
import { API } from './modules/api.js';
import { Auth } from './modules/auth.js';
import { StreamEngine } from './modules/stream.js';
import { LoginPage } from './modules/login.js';
import { ChatPage } from './modules/chat.js';
import { HomePage } from './modules/home.js';
import { mountComponent } from './modules/component.js';
import { CONFIG } from './modules/config.js';

const App = {
    async init() {
        try {
            document.title = CONFIG.APP_NAME;
            // 1. 挂载页面组件（视图来自 pages/*.html，Shadow DOM 隔离）
            const [loginComp, chatComp, homeComp] = await Promise.all([
                mountComponent({ tag: 'login-page', url: './pages/login.html' }),
                mountComponent({ tag: 'chat-page', url: './pages/chat.html' }),
                mountComponent({ tag: 'home-page', url: './pages/home.html' }),
            ]);

            // 2. 初始化组件（注入业务回调；组件内部不感知 App 细节）
            LoginPage.init(loginComp, {
                onSubmit: (username, password) => Auth.login(username, password),
            });

            ChatPage.init(chatComp, {
                onSend: () => this.sendMessage(),
                onInterrupt: () => this.interruptChat(),
                onNewChat: () => this.newConversation(),
                onClearChat: () => this.clearConversation(),
                // 历史会话抽屉：打开时拉取当前用户的会话主题（后端按时间倒序返回最近 15 条）
                onLoadSessions: () => API.getSessions(State.user?.token),
                onSelectConversation: (convId, title, agentId) => this.selectConversation(convId, title, agentId),
                onDeleteConversation: (convId) => this.deleteConversation(convId),
            });

            HomePage.init(homeComp, {
                // 二次确认后：执行退出，回到登录页
                onConfirmLogout: async () => {
                    await Auth.logout();
                },
            });

            // 3. 绑定全局事件（组件内部交互已由组件自身处理）
            this._bindEvents();

            // 4. 恢复会话状态，决定初始展示登录页还是主页
            const restored = await Auth.restore();
            if (!restored) {
                LoginPage.focusUsername();
            } else {
                // 刷新后停在"只有服务端记录"的会话上：补拉该会话轮次，避免只剩空状态
                const conv = State.getCurrentConversation();
                if (conv && conv.messages.length === 0) {
                    this.loadSessionTurns(conv.id);
                }
            }

            console.log('🚀 App initialized', {
                version: '1.0.0',
                user: State.user?.username || '未登录',
                conversations: State.conversations.length,
            });
        } catch (err) {
            console.error('🚨 App 初始化失败:', err);
        }
    },

    _bindEvents() {
        // 全局快捷键
        document.addEventListener('keydown', (e) => {
            if (e.ctrlKey && e.shiftKey && e.key === 'N') {
                e.preventDefault();
                if (State.isLoggedIn()) {
                    this.newConversation();
                }
            }
        });
    },

    /** 切换到指定会话（历史会话抽屉点击）；本地无记录时创建占位，并拉取该会话最近轮次 */
    selectConversation(convId, title, agentId) {
        if (!convId) return;
        if (State.isStreaming) StreamEngine.cancel();
        const isLocal = State.conversations.some(c => c.id === convId);
        if (isLocal) {
            if (!State.setCurrentConversation(convId)) return;
            // 历史会话带着自己的 Agent：选定后一并绑定，后续消息续用同一 Agent
            State.setConversationAgent(convId, agentId);
        } else {
            State.ensureConversation(convId, title, agentId);
        }
        const conv = State.getCurrentConversation();
        ChatPage.renderMessages(conv);
        ChatPage.updateChatTitle(conv?.title || '新对话', conv?.agentId);
        ChatPage.setInputEnabled(true);
        ChatPage.updateSendButton();
        ChatPage.closeHistory();

        // 本地无消息内容（占位会话）时，从后端拉取该会话最近 15 轮并展示
        if (conv && conv.messages.length === 0) {
            this.loadSessionTurns(conv.id);
        }
    },

    /** 拉取并渲染指定会话的历史轮次（失败时在消息区给出提示，不影响继续对话） */
    async loadSessionTurns(sessionId) {
        if (!sessionId) return;
        ChatPage.showTurnLoading();
        try {
            const turns = await API.getSessionTurns(State.user?.token, sessionId);
            // 期间用户可能已切换到其它会话：只渲染仍处于当前会话的结果
            if (State.currentConvId !== sessionId) return;
            ChatPage.renderTurns(turns);
        } catch (err) {
            if (State.currentConvId !== sessionId) return;
            console.warn('[Turns] 获取会话历史失败：', err);
            ChatPage.showTurnError(err?.message || '加载历史对话失败');
        }
    },

    /** 删除指定会话（历史会话抽屉删除按钮） */
    deleteConversation(convId) {
        const conv = State.conversations.find(c => c.id === convId);
        if (!conv) return;
        if (!confirm(`确定删除会话「${conv.title || '新对话'}」？删除后不可恢复。`)) return;

        if (State.isStreaming && State.currentConvId === convId) {
            StreamEngine.cancel();
        }
        State.deleteConversation(convId);

        const current = State.getCurrentConversation();
        ChatPage.renderMessages(current);
        ChatPage.updateChatTitle(current?.title || '新对话', current?.agentId);
        ChatPage.setInputEnabled(true);
        ChatPage.updateSendButton();
        ChatPage.removeSessionFromList(convId);
    },

    /** 清空当前对话 */
    clearConversation() {
        const conv = State.getCurrentConversation();
        if (!conv || conv.messages.length === 0) return;
        if (confirm('确定要清空当前对话的所有消息吗？')) {
            State.clearCurrentConversation();
            const updated = State.getCurrentConversation();
            ChatPage.renderMessages(updated);
            ChatPage.updateChatTitle(updated?.title || '新对话', updated?.agentId);
            ChatPage.focusInput();
        }
    },

    sendMessage() {
        const content = ChatPage.getInputValue();
        if (!content || State.isStreaming || ChatPage.isInputDisabled()) return;

        let conv = State.getCurrentConversation();
        if (!conv) {
            const newConv = State.createConversation('新对话');
            ChatPage.updateChatTitle(newConv.title, newConv.agentId);
            conv = State.getCurrentConversation();
            if (!conv) return;
        }

        this._doSendMessage(conv, content);
    },

    _doSendMessage(conv, content) {
        ChatPage.setInputEnabled(false);
        State.addMessage(conv.id, 'user', content);
        ChatPage.renderMessages(conv);
        ChatPage.clearInput();

        StreamEngine.start(
            conv.id,
            content,
            () => {
                // onComplete
                const updatedConv = State.getCurrentConversation();
                if (updatedConv) {
                    ChatPage.renderMessages(updatedConv);
                    ChatPage.updateChatTitle(updatedConv.title, updatedConv.agentId);
                }
                ChatPage.setInputEnabled(true);
                ChatPage.updateSendButton();
                ChatPage.focusInput();
            },
            (err) => {
                ChatPage.setInputEnabled(true);
                ChatPage.updateSendButton();
                ChatPage.focusInput();
                console.error('Stream error:', err);
            }
        );
    },

    /** 中断当前会话进行中的流式聊天（用户点击中断按钮，按钮已变为中断态） */
    interruptChat() {
        if (!State.isStreaming) return;
        const conv = State.getCurrentConversation();
        if (!conv) return;
        StreamEngine.interrupt(conv.id);
    },

    newConversation() {
        if (State.isStreaming) {
            StreamEngine.cancel();
        }
        const conv = State.createConversation('新对话');
        ChatPage.renderMessages(conv);
        ChatPage.updateChatTitle(conv.title, conv.agentId);
        ChatPage.setInputEnabled(true);
        ChatPage.updateSendButton();
        ChatPage.focusInput();
    },

};

// 启动应用
document.addEventListener('DOMContentLoaded', () => {
    App.init().catch(error => {
        console.error("应用初始化失败:", error);
    });
});

/**
 * ============================================================
 * ./scripts/main.js — 主应用控制器 (ES Module)
 * ============================================================
 */

import { State } from './modules/state.js';
import { Auth } from './modules/auth.js';
import { StreamEngine } from './modules/stream.js';
import { LoginPage } from './modules/login.js';
import { ChatPage } from './modules/chat.js';
import { mountComponent } from './modules/component.js';

const App = {
    async init() {
        try {
            // 1. 挂载页面组件（视图来自 pages/*.html，Shadow DOM 隔离）
            const [loginComp, chatComp] = await Promise.all([
                mountComponent({ tag: 'login-page', url: 'pages/login.html' }),
                mountComponent({ tag: 'chat-page', url: 'pages/chat.html' }),
            ]);

            // 2. 初始化组件（注入业务回调；组件内部不感知 App 细节）
            LoginPage.init(loginComp, {
                onSubmit: (username, password) => Auth.login(username, password),
            });
            
            ChatPage.init(chatComp, {
                onSend: () => this.sendMessage(),
                onNewChat: () => this.newConversation(),
                onClearChat: () => this.clearConversation(),
                // 点击退出：先弹出二次确认弹窗（不阻塞、不受 confirm() 拦截影响）
                onLogout: () => ChatPage.openLogoutConfirm(),
                // 二次确认后：执行退出，回到登录页
                onConfirmLogout: () => Auth.logout(),
            });

            // 3. 绑定全局事件（组件内部交互已由组件自身处理）
            this._bindEvents();

            // 4. 恢复会话状态，决定初始展示登录页还是聊天页
            const restored = Auth.restore();
            if (!restored) {
                LoginPage.focusUsername();
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
        // 窗口尺寸变化：桌面端收起侧边栏
        let resizeTimer;
        window.addEventListener('resize', () => {
            clearTimeout(resizeTimer);
            resizeTimer = setTimeout(() => {
                if (window.innerWidth > 768) {
                    ChatPage.closeSidebar();
                }
            }, 200);
        });

        // 自定义事件：切换对话（由 ChatPage 会话列表广播）
        document.addEventListener('switch-conversation', (e) => {
            this.switchConversation(e.detail.convId);
        });
        document.addEventListener('delete-conversation', (e) => {
            this.deleteConversation(e.detail.convId);
        });

        // 全局快捷键
        document.addEventListener('keydown', (e) => {
            if (e.ctrlKey && e.shiftKey && e.key === 'N') {
                e.preventDefault();
                if (State.isLoggedIn()) {
                    this.newConversation();
                }
            }
            if (e.key === 'Escape' && window.innerWidth <= 768) {
                ChatPage.closeSidebar();
            }
        });
    },

    clearConversation() {
        const conv = State.getCurrentConversation();
        if (!conv || conv.messages.length === 0) return;
        if (confirm('确定要清空当前对话的所有消息吗？')) {
            State.clearCurrentConversation();
            const updated = State.getCurrentConversation();
            ChatPage.renderMessages(updated);
            ChatPage.updateChatTitle(updated?.title || '新对话');
            ChatPage.renderConversations(State.conversations, State.currentConvId);
            ChatPage.focusInput();
        }
    },

    sendMessage() {
        const content = ChatPage.getInputValue();
        if (!content || State.isStreaming || ChatPage.isInputDisabled()) return;

        let conv = State.getCurrentConversation();
        if (!conv) {
            const newConv = State.createConversation('新对话');
            ChatPage.renderConversations(State.conversations, State.currentConvId);
            ChatPage.updateChatTitle(newConv.title);
            conv = State.getCurrentConversation();
            if (!conv) return;
        }

        this._doSendMessage(conv, content);
    },

    _doSendMessage(conv, content) {
        ChatPage.setInputEnabled(false);
        State.isStreaming = true;

        State.addMessage(conv.id, 'user', content);
        ChatPage.renderMessages(conv);
        ChatPage.clearInput();
        ChatPage.renderConversations(State.conversations, State.currentConvId);

        StreamEngine.start(
            conv.id,
            content,
            () => {
                // onComplete
                State.isStreaming = false;
                const updatedConv = State.getCurrentConversation();
                if (updatedConv) {
                    ChatPage.renderMessages(updatedConv);
                    ChatPage.updateChatTitle(updatedConv.title);
                    ChatPage.renderConversations(State.conversations, State.currentConvId);
                }
                ChatPage.setInputEnabled(true);
                ChatPage.updateSendButton();
                ChatPage.focusInput();
            },
            (err) => {
                State.isStreaming = false;
                ChatPage.setInputEnabled(true);
                ChatPage.updateSendButton();
                ChatPage.focusInput();
                console.error('Stream error:', err);
            }
        );
    },

    newConversation() {
        if (State.isStreaming) {
            StreamEngine.cancel();
        }
        const conv = State.createConversation('新对话');
        ChatPage.renderConversations(State.conversations, State.currentConvId);
        ChatPage.renderMessages(conv);
        ChatPage.updateChatTitle(conv.title);
        ChatPage.setInputEnabled(true);
        ChatPage.updateSendButton();
        ChatPage.focusInput();
        if (window.innerWidth <= 768) {
            ChatPage.closeSidebar();
        }
    },

    switchConversation(convId) {
        if (State.isStreaming) {
            StreamEngine.cancel();
        }
        State.currentConvId = convId;
        State.save();
        const conv = State.getCurrentConversation();
        if (conv) {
            ChatPage.renderMessages(conv);
            ChatPage.updateChatTitle(conv.title);
            ChatPage.renderConversations(State.conversations, State.currentConvId);
            ChatPage.setInputEnabled(true);
            ChatPage.updateSendButton();
            ChatPage.focusInput();
        }
    },

    deleteConversation(convId) {
        if (State.isStreaming) {
            StreamEngine.cancel();
        }
        State.deleteConversation(convId);
        ChatPage.renderConversations(State.conversations, State.currentConvId);
        const conv = State.getCurrentConversation();
        if (conv) {
            ChatPage.renderMessages(conv);
            ChatPage.updateChatTitle(conv.title);
            ChatPage.setInputEnabled(true);
        } else {
            const newConv = State.createConversation('新对话');
            ChatPage.renderMessages(newConv);
            ChatPage.updateChatTitle(newConv.title);
            ChatPage.renderConversations(State.conversations, State.currentConvId);
            ChatPage.setInputEnabled(true);
        }
        ChatPage.updateSendButton();
        ChatPage.focusInput();
    },
};

// 启动应用
document.addEventListener('DOMContentLoaded', () => {
    App.init();
});
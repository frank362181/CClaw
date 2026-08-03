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
import { HomePage } from './modules/home.js';
import { mountComponent } from './modules/component.js';

const App = {
    async init() {
        try {
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
                onNewChat: () => this.newConversation(),
                onClearChat: () => this.clearConversation(),
                onHideChat: () => this.toggleChatDrawer(false),
            });

            HomePage.init(homeComp, {
                // 点击右上角聊天气泡：以抽屉方式打开 chat.html
                onToggleChat: () => this.toggleChatDrawer(),
                // 二次确认后：执行退出，回到登录页
                onConfirmLogout: () => Auth.logout(),
            });

            // 3. 绑定全局事件（组件内部交互已由组件自身处理）
            this._bindEvents();

            // 4. 恢复会话状态，决定初始展示登录页还是主页
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
        // 窗口尺寸变化：桌面端收起主侧边栏与聊天抽屉
        let resizeTimer;
        window.addEventListener('resize', () => {
            clearTimeout(resizeTimer);
            resizeTimer = setTimeout(() => {
                if (window.innerWidth > 768) {
                    HomePage.closeSidebar();
                }
            }, 200);
        });

        // 聊天抽屉：点击遮罩 / 关闭按钮收起
        document.getElementById('drawerOverlay').addEventListener('click', () => {
            if (this.isChatDrawerOpen()) {
                this.toggleChatDrawer(false);
            }
        });

        // 全局快捷键
        document.addEventListener('keydown', (e) => {
            if (e.ctrlKey && e.shiftKey && e.key === 'N') {
                e.preventDefault();
                if (State.isLoggedIn()) {
                    this.newConversation();
                }
            }
            if (e.key === 'Escape') {
                if (this.isChatDrawerOpen()) {
                    this.toggleChatDrawer(false);
                } else if (window.innerWidth <= 768) {
                    HomePage.closeSidebar();
                }
            }
        });
    },

    /* ================= 聊天抽屉（chat.html 以抽屉方式展示） ================= */

    isChatDrawerOpen() {
        const drawer = document.getElementById('chatDrawer');
        return drawer ? drawer.classList.contains('open') : false;
    },

    /**
     * 打开 / 收起聊天抽屉
     * @param {boolean|undefined} open 未传则切换
     */
    toggleChatDrawer(open) {
        const drawer = document.getElementById('chatDrawer');
        if (!drawer) return;

        const isOpen = drawer.classList.contains('open');
        const target = open !== undefined ? open : !isOpen;

        if (target) {
            // 打开：展示聊天组件并渲染会话状态
            drawer.classList.add('open');
            ChatPage.show();
            const conv = State.getCurrentConversation();
            if (conv) {
                ChatPage.renderMessages(conv);
                ChatPage.updateChatTitle(conv.title);
            }
            ChatPage.setInputEnabled(true);
            ChatPage.updateSendButton();
            ChatPage.focusInput();
        } else {
            // 收起：隐藏聊天组件
            drawer.classList.remove('open');
            ChatPage.hide();
        }
    },

    /** 清空当前对话 */
    clearConversation() {
        const conv = State.getCurrentConversation();
        if (!conv || conv.messages.length === 0) return;
        if (confirm('确定要清空当前对话的所有消息吗？')) {
            State.clearCurrentConversation();
            const updated = State.getCurrentConversation();
            ChatPage.renderMessages(updated);
            ChatPage.updateChatTitle(updated?.title || '新对话');
            ChatPage.focusInput();
        }
    },

    sendMessage() {
        const content = ChatPage.getInputValue();
        if (!content || State.isStreaming || ChatPage.isInputDisabled()) return;

        let conv = State.getCurrentConversation();
        if (!conv) {
            const newConv = State.createConversation('新对话');
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
        ChatPage.renderMessages(conv);
        ChatPage.updateChatTitle(conv.title);
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
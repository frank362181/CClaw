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
                onHideChat: () => this.toggleChatDrawer(false),
            });

            HomePage.init(homeComp, {
                // 点击右上角聊天气泡：以抽屉方式打开 chat.html
                onToggleChat: () => this.toggleChatDrawer(),
                // 二次确认后：执行退出，回到登录页
                onConfirmLogout: async () => {
                    this.toggleChatDrawer(false);
                    await Auth.logout();
                },
            });

            // 3. 绑定全局事件（组件内部交互已由组件自身处理）
            this._bindEvents();

            // 4. 恢复会话状态，决定初始展示登录页还是主页
            const restored = await Auth.restore();
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

        // 聊天抽屉：宽度可拖拽调节
        this._initDrawerResize();
    },

    /* ================= 聊天抽屉（chat.html 以抽屉方式展示） ================= */

    /**
     * 聊天抽屉宽度拖拽调节（桌面端）。
     * - 拖动抽屉左缘的调节条即可改宽度（带最小/最大钳制）。
     * - 消息列表为 flex 自适应，宽度会跟随抽屉同步放大/缩小。
     * - 记住用户上次的宽度（localStorage）；移动端强制全宽。
     */
    _initDrawerResize() {
        const resizer = document.getElementById('drawerResizer');
        const panel = document.querySelector('#chatDrawer .drawer-panel');
        if (!resizer || !panel) return;

        const rootStyle = getComputedStyle(document.documentElement);
        const parsePx = (v) => parseInt(v, 10) || 0;
        const MIN_W = parsePx(rootStyle.getPropertyValue('--chat-drawer-min-width')) || 320;
        const MAX_W = parsePx(rootStyle.getPropertyValue('--chat-drawer-max-width')) || 960;
        const STORE_KEY = 'cclaw.chatDrawerWidth';

        /* 恢复上次宽度（仅桌面端；移动端由 CSS 全宽接管） */
        const restoreWidth = () => {
            if (window.innerWidth <= 768) {
                panel.style.width = '';
                return;
            }
            const saved = parseInt(localStorage.getItem(STORE_KEY), 10);
            if (Number.isFinite(saved) && saved >= MIN_W && saved <= MAX_W) {
                panel.style.width = `${saved}px`;
            }
        };

        let dragging = false;
        let startX = 0;
        let startWidth = 0;

        const clampWidth = (w) => {
            const maxAllowed = Math.min(MAX_W, window.innerWidth - 16);
            return Math.max(MIN_W, Math.min(maxAllowed, w));
        };

        const onMove = (e) => {
            if (!dragging) return;
            // 面板锚定右侧：向左拖动（clientX 变小）则加宽
            const w = clampWidth(startWidth + (startX - e.clientX));
            panel.style.width = `${w}px`;
        };

        const onUp = () => {
            if (!dragging) return;
            dragging = false;
            resizer.classList.remove('active');
            document.body.classList.remove('drawer-resizing');
            resizer.removeEventListener('pointermove', onMove);
            resizer.removeEventListener('pointerup', onUp);
            resizer.removeEventListener('pointercancel', onUp);
            const w = parseInt(panel.style.width, 10);
            if (Number.isFinite(w) && w >= MIN_W) {
                try { localStorage.setItem(STORE_KEY, String(w)); } catch (err) { /* 忽略存储失败 */ }
            }
        };

        resizer.addEventListener('pointerdown', (e) => {
            if (window.innerWidth <= 768) return; // 移动端不启用拖拽
            dragging = true;
            startX = e.clientX;
            startWidth = panel.getBoundingClientRect().width;
            resizer.classList.add('active');
            document.body.classList.add('drawer-resizing');
            try { resizer.setPointerCapture(e.pointerId); } catch (err) { /* 忽略 */ }
            resizer.addEventListener('pointermove', onMove);
            resizer.addEventListener('pointerup', onUp);
            resizer.addEventListener('pointercancel', onUp);
            e.preventDefault();
        });

        // 视口跨断点：进入移动端恢复全宽，回到桌面端恢复上次宽度
        window.addEventListener('resize', () => {
            if (window.innerWidth <= 768) {
                panel.style.width = '';
            } else {
                restoreWidth();
            }
        });

        restoreWidth();
    },

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

        // 抽屉打开时隐藏右下角 AI 浮动气泡，避免遮挡发送按钮
        HomePage.setChatBubbleVisible(!target);

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
                    ChatPage.updateChatTitle(updatedConv.title);
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

/**
 * ============================================================
 * 主页组件控制器 (HomePage)
 * ============================================================
 * 登录成功后显示的主页面：
 *   - 左侧边栏：目录树菜单（dashboard / 资产台账）
 *   - 右侧核心内容显示区域（按菜单动态渲染）
 *   - 右上角聊天气泡：点击后由外部以抽屉方式打开 chat.html
 *
 * 高内聚：仅负责主页视图的渲染与交互。
 * 低耦合：不直接依赖 App / ChatPage，通过注入回调与外部协作；
 *         打开聊天抽屉、退出登录等跨组件动作交由外部处理。
 */

import { getInitials } from '../utils/helper.js?v=1.0.4';

/* ---------- 菜单定义（目录树） ---------- */
const MENUS = {
    dashboard: { id: 'dashboard', label: 'dashboard', title: '数据总览' },
    asset: { id: 'asset', label: '资产台账', title: '资产台账' },
    settings: { id: 'settings', label: '系统设置', title: '系统设置' },
};

/* ---------- 内容 Mock 数据（预留后端接入位置） ---------- */
const DASH_STATS = [
    { label: '资产总数', value: '1,286', icon: '📦', delta: '+3.2%', up: true },
    { label: '在用设备', value: '1,054', icon: '🖥️', delta: '+1.1%', up: true },
    { label: '待处理告警', value: '12', icon: '⚠️', delta: '-18%', up: false },
    { label: '今日巡检', value: '96', icon: '✅', delta: '+24', up: true },
];

const CATEGORY_DATA = [
    { label: 'IT设备', value: 42 },
    { label: '办公设备', value: 26 },
    { label: '网络设备', value: 15 },
    { label: '安防设备', value: 10 },
    { label: '电力设备', value: 7 },
];

const RECENT_ACTIVITY = [
    { time: '10:32', text: '张伟 更新了资产「服务器集群 A1」的状态' },
    { time: '09:18', text: '系统完成每日资产巡检，共发现 2 项异常' },
    { time: '08:45', text: '李娜 新增资产「台式工作站 X3」' },
    { time: '昨天 17:20', text: '王强 提交了「激光打印机 HP」的维修工单' },
    { time: '昨天 15:05', text: '资产台账已完成本周数据同步' },
];

const ASSET_ROWS = [
    { id: 'ZC-2026-0001', name: '服务器集群 A1', category: 'IT设备', owner: '张伟', status: '在用', updated: '2026-07-28' },
    { id: 'ZC-2026-0002', name: '台式工作站 X3', category: 'IT设备', owner: '李娜', status: '在用', updated: '2026-07-26' },
    { id: 'ZC-2026-0003', name: '激光打印机 HP', category: '办公设备', owner: '王强', status: '维修中', updated: '2026-07-24' },
    { id: 'ZC-2026-0004', name: '会议室投影仪', category: '办公设备', owner: '赵敏', status: '在用', updated: '2026-07-20' },
    { id: 'ZC-2026-0005', name: '数据库服务器 S1', category: 'IT设备', owner: '张伟', status: '在用', updated: '2026-07-18' },
    { id: 'ZC-2026-0006', name: '网络交换机 48口', category: '网络设备', owner: '陈杰', status: '闲置', updated: '2026-07-15' },
    { id: 'ZC-2026-0007', name: '门禁考勤机', category: '安防设备', owner: '刘洋', status: '在用', updated: '2026-07-10' },
    { id: 'ZC-2026-0008', name: '空调机组 3P', category: '办公设备', owner: '王强', status: '在用', updated: '2026-07-06' },
    { id: 'ZC-2026-0009', name: '视频监控主机', category: '安防设备', owner: '刘洋', status: '在用', updated: '2026-07-02' },
    { id: 'ZC-2026-0010', name: 'UPS 不间断电源', category: '电力设备', owner: '陈杰', status: '维修中', updated: '2026-06-29' },
];

/* 资产状态徽标样式映射 */
const STATUS_BADGE = {
    '在用': 'inuse',
    '维修中': 'repair',
    '闲置': 'idle',
    '已报废': 'scrapped',
};

export const HomePage = {
    comp: null,
    els: {},
    handlers: {},
    username: '',
    currentMenu: 'dashboard',

    /**
     * 初始化组件
     * @param {object} comp 组件实例（mountComponent 返回）
     * @param {{ onToggleChat?: Function, onConfirmLogout?: Function }} handlers
     */
    init(comp, handlers = {}) {
        this.comp = comp;
        this.handlers = handlers;
        this.els = {
            homePage: comp.query('#homePage'),
            homeSidebar: comp.query('#homeSidebar'),
            homeSidebarOverlay: comp.query('#homeSidebarOverlay'),
            menuTree: comp.query('#menuTree'),
            contentTitle: comp.query('#contentTitle'),
            contentView: comp.query('#contentView'),
            chatBubble: comp.query('#chatBubble'),
            btnHomeLogout: comp.query('#btnHomeLogout'),
            btnToggleHomeSidebar: comp.query('#btnToggleHomeSidebar'),
            homeAvatar: comp.query('#homeAvatar'),
            homeUserName: comp.query('#homeUserName'),
            // 退出确认弹窗
            logoutConfirm: comp.query('#homeLogoutConfirm'),
            btnCancelLogout: comp.query('#btnCancelLogout'),
            btnConfirmLogout: comp.query('#btnConfirmLogout'),
        };

        // 目录树：点击菜单切换内容
        this.els.menuTree.querySelectorAll('.tree-item').forEach(item => {
            item.addEventListener('click', () => this.selectMenu(item.dataset.menu));
        });

        // 聊天气泡：通知外部以抽屉方式打开聊天窗口（低耦合，交由 App 处理）
        this.els.chatBubble.addEventListener('click', () => this.handlers.onToggleChat?.());

        // 移动端主侧边栏折叠
        this.els.btnToggleHomeSidebar.addEventListener('click', () => this.toggleSidebar());
        this.els.homeSidebarOverlay.addEventListener('click', () => this.closeSidebar());

        // 退出登录：先弹出二次确认（是否真正退出由外部决定）
        this.els.btnHomeLogout.addEventListener('click', () => this.openLogoutConfirm());
        this.els.btnCancelLogout.addEventListener('click', () => this.closeLogoutConfirm());
        this.els.btnConfirmLogout.addEventListener('click', () => this.handlers.onConfirmLogout?.());
        this.els.logoutConfirm.addEventListener('click', (e) => {
            if (e.target === this.els.logoutConfirm) this.closeLogoutConfirm();
        });

        // 默认展示 dashboard
        this.selectMenu('dashboard');
    },

    show() {
        this.closeLogoutConfirm();
        // 与 LoginPage 一样，同时显示组件宿主，避免视图虽显示但宿主被隐藏。
        this.comp.el.style.display = 'block';
        this.els.homePage.style.display = 'flex';
        requestAnimationFrame(() => {
            this.els.homePage.classList.add('active');
        });
    },

    hide() {
        this.els.homePage.classList.remove('active');
        this.els.homePage.style.display = 'none';
        // 仅隐藏 Shadow DOM 内部元素不会释放 <home-page> 宿主占据的整屏高度。
        this.comp.el.style.display = 'none';
        this.closeSidebar();
    },

    /**
     * 选择菜单并渲染对应内容
     * @param {string} menuId
     */
    selectMenu(menuId) {
        const menu = MENUS[menuId] || MENUS.dashboard;
        this.currentMenu = menu.id;

        // 高亮目录树菜单项
        this.els.menuTree.querySelectorAll('.tree-item').forEach(item => {
            item.classList.toggle('active', item.dataset.menu === menu.id);
        });

        // 更新面包屑标题
        this.els.contentTitle.textContent = menu.label;

        // 渲染核心内容区
        this.els.contentView.innerHTML = menu.id === 'asset'
            ? this.renderAsset()
            : menu.id === 'settings'
                ? this.renderSettings()
                : this.renderDashboard();

        // 移动端选中菜单后收起主侧边栏
        if (window.innerWidth <= 768) {
            this.closeSidebar();
        }
    },

    /* ========== dashboard 内容 ========== */
    renderDashboard() {
        const statsHtml = DASH_STATS.map(s => `
            <div class="stat-card">
                <div class="stat-icon">${s.icon}</div>
                <div class="stat-info">
                    <div class="stat-value">${s.value}</div>
                    <div class="stat-label">${s.label}</div>
                </div>
                <div class="stat-delta ${s.up ? 'up' : 'down'}">${s.delta}</div>
            </div>
        `).join('');

        const maxCat = Math.max(...CATEGORY_DATA.map(c => c.value));
        const barsHtml = CATEGORY_DATA.map(c => `
            <div class="chart-bar-row">
                <span class="chart-bar-label">${c.label}</span>
                <div class="chart-bar-track">
                    <div class="chart-bar-fill" style="width:${Math.round(c.value / maxCat * 100)}%"></div>
                </div>
                <span class="chart-bar-value">${c.value}%</span>
            </div>
        `).join('');

        const activityHtml = RECENT_ACTIVITY.map(a => `
            <li class="activity-item">
                <span class="activity-time">${a.time}</span>
                <span class="activity-text">${a.text}</span>
            </li>
        `).join('');

        return `
            <div class="stat-grid">
                ${statsHtml}
            </div>

            <div class="dash-panels">
                <div class="panel-card">
                    <div class="panel-title">资产分类分布</div>
                    <div class="chart-bars">
                        ${barsHtml}
                    </div>
                </div>
                <div class="panel-card">
                    <div class="panel-title">最近动态</div>
                    <ul class="activity-list">
                        ${activityHtml}
                    </ul>
                </div>
            </div>
        `;
    },

    /* ========== 资产台账内容 ========== */
    renderAsset() {
        const rowsHtml = ASSET_ROWS.map(row => `
            <tr>
                <td class="asset-id">${row.id}</td>
                <td class="asset-name">${row.name}</td>
                <td>${row.category}</td>
                <td>${row.owner}</td>
                <td><span class="status-badge ${STATUS_BADGE[row.status] || ''}">${row.status}</span></td>
                <td class="asset-updated">${row.updated}</td>
            </tr>
        `).join('');

        return `
            <div class="asset-toolbar">
                <div class="asset-title-block">
                    <h2>资产台账</h2>
                    <p>管理企业所有固定资产信息，支持按分类检索与维护。</p>
                </div>
                <div class="asset-actions">
                    <span class="asset-count">共 ${ASSET_ROWS.length} 条记录</span>
                    <button type="button" class="btn-asset-add" id="btnAssetAdd">＋ 新增资产</button>
                </div>
            </div>

            <div class="asset-table-wrap">
                <table class="asset-table">
                    <thead>
                        <tr>
                            <th>资产编号</th>
                            <th>资产名称</th>
                            <th>分类</th>
                            <th>负责人</th>
                            <th>状态</th>
                            <th>更新时间</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${rowsHtml}
                    </tbody>
                </table>
            </div>
        `;
    },

    /* ========== 系统设置内容 ========== */
    renderSettings() {
        return `
            <div class="asset-toolbar">
                <div class="asset-title-block">
                    <h2>系统设置</h2>
                    <p>配置系统基础信息、外观与通知偏好。</p>
                </div>
            </div>

            <div class="settings-grid">
                <div class="panel-card">
                    <div class="panel-title">基础信息</div>
                    <div class="settings-rows">
                        <div class="setting-row">
                            <span class="setting-label">系统名称</span>
                            <input type="text" class="setting-input" value="企业智能平台" />
                        </div>
                        <div class="setting-row">
                            <span class="setting-label">当前版本</span>
                            <span class="setting-value">v1.0.7</span>
                        </div>
                        <div class="setting-row">
                            <span class="setting-label">系统语言</span>
                            <select class="setting-input">
                                <option selected>简体中文</option>
                                <option>English</option>
                            </select>
                        </div>
                    </div>
                </div>

                <div class="panel-card">
                    <div class="panel-title">外观与通知</div>
                    <div class="settings-rows">
                        <div class="setting-row">
                            <span class="setting-label">深色模式</span>
                            <label class="switch">
                                <input type="checkbox" />
                                <span class="switch-slider"></span>
                            </label>
                        </div>
                        <div class="setting-row">
                            <span class="setting-label">消息通知</span>
                            <label class="switch">
                                <input type="checkbox" checked />
                                <span class="switch-slider"></span>
                            </label>
                        </div>
                        <div class="setting-row">
                            <span class="setting-label">自动保存会话</span>
                            <label class="switch">
                                <input type="checkbox" checked />
                                <span class="switch-slider"></span>
                            </label>
                        </div>
                    </div>
                </div>
            </div>
        `;
    },

    /* ========== 用户信息 / 侧边栏 ========== */
    updateUserInfo(username) {
        this.username = username || '';
        const display = username || '用户';
        this.els.homeUserName.textContent = display;
        this.els.homeAvatar.textContent = getInitials(display);
        // 用户名变化后重渲染当前菜单内容（如 dashboard 欢迎语）
        if (this.els.contentView) {
            this.selectMenu(this.currentMenu);
        }
    },

    toggleSidebar(open) {
        const isOpen = this.els.homeSidebar.classList.contains('open');
        const target = open !== undefined ? open : !isOpen;
        this.els.homeSidebar.classList.toggle('open', target);
        this.els.homeSidebarOverlay.classList.toggle('active', target);
    },

    closeSidebar() {
        this.toggleSidebar(false);
    },

    openLogoutConfirm() {
        this.els.logoutConfirm.classList.add('show');
    },

    closeLogoutConfirm() {
        this.els.logoutConfirm.classList.remove('show');
    },

    reset() {
        this.closeLogoutConfirm();
        this.closeSidebar();
        this.updateUserInfo('');
        this.selectMenu('dashboard');
    },
};

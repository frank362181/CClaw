/**
 * ============================================================
 * 主页组件控制器 (HomePage)
 * ============================================================
 * 登录成功后显示的主页面：
 *   - 左侧边栏：目录树菜单（dashboard / 我的项目 / 系统设置）
 *   - 右侧核心内容显示区域（按菜单动态渲染）
 *   - 右上角聊天气泡：点击后由外部以抽屉方式打开 chat.html
 *
 * 高内聚：仅负责主页视图的渲染与交互。
 * 低耦合：不直接依赖 App / ChatPage，通过注入回调与外部协作；
 *         打开聊天抽屉、退出登录等跨组件动作交由外部处理。
 */

import { getInitials, genId } from '../utils/helper.js';
import { State } from './state.js';

/* ---------- 菜单定义（目录树） ---------- */
const MENUS = {
    dashboard: { id: 'dashboard', label: 'dashboard', title: '数据总览' },
    npa: { id: 'npa', label: '我的项目', title: '我的项目' },
    settings: { id: 'settings', label: '系统设置', title: '系统设置' },
};

/* ---------- 系统设置：Agents / Skills / MCP / Storages / Knowledge 图卡数据（参考 OpenClaw 设置结构，预留后端接入位置） ---------- */
const AGENTS = [
    { id: 'assistant', name: '通用助手', icon: '🤖', desc: '日常问答与通用任务处理', model: 'cclaw-gpt-4o', status: '运行中', default: true },
    { id: 'npa-manager', name: '不良资产管理', icon: '🏦', desc: '不良资产处置、分析与进度跟踪', model: 'cclaw-gpt-4o', status: '运行中', default: false },
    { id: 'data-analyst', name: '数据分析', icon: '📊', desc: '数据统计、洞察与可视化', model: 'cclaw-gpt-4o-mini', status: '运行中', default: false },
    { id: 'report-writer', name: '报告撰写', icon: '📝', desc: '自动生成结构化业务报告', model: 'cclaw-gpt-4o-mini', status: '已暂停', default: false },
    { id: 'risk-watch', name: '风险预警', icon: '⚠️', desc: '实时监控风险并生成预警', model: 'cclaw-gpt-4o', status: '运行中', default: false },
];

const SKILLS = [
    { id: 'code-search', name: '代码搜索', icon: '🔍', desc: '在工作区中检索代码与文档', enabled: true },
    { id: 'web-fetch', name: '网页抓取', icon: '🌐', desc: '抓取并解析网页内容', enabled: true },
    { id: 'data-viz', name: '数据可视化', icon: '📈', desc: '将数据渲染为图表', enabled: true },
    { id: 'file-parse', name: '文件解析', icon: '📄', desc: '解析 JSON / CSV / 文本文件', enabled: true },
    { id: 'memory', name: '记忆管理', icon: '🧠', desc: '跨会话持久化知识与偏好', enabled: true },
    { id: 'scheduler', name: '定时任务', icon: '⏰', desc: '按计划自动执行任务', enabled: false },
];

const MCP_SERVERS = [
    { id: 'fs', name: 'filesystem', icon: '🗂️', transport: 'stdio', enabled: true },
    { id: 'db', name: 'database', icon: '🗄️', transport: 'stdio', enabled: true },
    { id: 'web', name: 'fetch', icon: '🌐', transport: 'SSE', enabled: false },
];

const STORAGES = [
    { id: 'session', name: '会话存储', icon: '💾', type: 'memory', enabled: true },
    { id: 'redis', name: 'Redis 缓存', icon: '📦', type: 'redis', enabled: true },
];

const KNOWLEDGE = [
    { id: 'assets', name: '资产知识库', icon: '📚', entries: 1286, enabled: true },
    { id: 'npa', name: '不良资产知识库', icon: '🗃️', entries: 342, enabled: true },
];

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
    { time: '昨天 15:05', text: '系统已完成本周数据同步' },
];

/* ========== 我的项目（NPA）：数据与本地存储 ========== */
const NPA_STORAGE_PREFIX = 'cclaw_npa_data:';

const NPA_SEED = [
    { name: '某地产集团不良资产包', defaultCount: 156, totalAmount: 32000, recoveredAmount: 8600, progress: 27, createdDate: '2026-03-12', households: [
        { name: '恒信置业有限公司', assetAmount: 6800, defaultAmount: 4300, firstDefaultDate: '2025-11-03', recoveryScore: 45 },
        { name: '龙腾建设集团有限公司', assetAmount: 5200, defaultAmount: 3100, firstDefaultDate: '2026-01-15', recoveryScore: 30 },
        { name: '中盛商贸有限公司', assetAmount: 4100, defaultAmount: 2600, firstDefaultDate: '2025-08-22', recoveryScore: 55 },
    ] },
    { name: '制造业应收账款包', defaultCount: 84, totalAmount: 12500, recoveredAmount: 5200, progress: 42, createdDate: '2026-04-02', households: [
        { name: '瑞丰机械制造有限公司', assetAmount: 3200, defaultAmount: 2100, firstDefaultDate: '2026-02-10', recoveryScore: 70 },
        { name: '天成纺织有限公司', assetAmount: 2800, defaultAmount: 1700, firstDefaultDate: '2026-03-05', recoveryScore: 62 },
    ] },
    { name: '城商行不良贷款批量转让', defaultCount: 210, totalAmount: 58000, recoveredAmount: 0, progress: 5, createdDate: '2026-06-18', households: [
        { name: '华宇建材有限公司', assetAmount: 9800, defaultAmount: 6200, firstDefaultDate: '2025-06-18', recoveryScore: 35 },
        { name: '金桥物流有限公司', assetAmount: 7500, defaultAmount: 4800, firstDefaultDate: '2025-09-30', recoveryScore: 42 },
        { name: '远大电子科技有限公司', assetAmount: 6600, defaultAmount: 3900, firstDefaultDate: '2025-12-12', recoveryScore: 58 },
        { name: '蓝海餐饮管理有限公司', assetAmount: 5400, defaultAmount: 3300, firstDefaultDate: '2026-01-20', recoveryScore: 66 },
    ] },
    { name: '零售类小微企业不良包', defaultCount: 45, totalAmount: 6800, recoveredAmount: 6800, progress: 100, createdDate: '2026-01-08', households: [
        { name: '晨光便利店', assetAmount: 120, defaultAmount: 80, firstDefaultDate: '2025-10-08', recoveryScore: 88 },
        { name: '鑫源汽修部', assetAmount: 95, defaultAmount: 60, firstDefaultDate: '2025-12-15', recoveryScore: 80 },
    ] },
    { name: '基础设施类不良债权包', defaultCount: 96, totalAmount: 27600, recoveredAmount: 4200, progress: 15, createdDate: '2026-07-01', households: [
        { name: '安泰工程承包有限公司', assetAmount: 7300, defaultAmount: 4400, firstDefaultDate: '2026-03-26', recoveryScore: 40 },
        { name: '宏远市政工程有限公司', assetAmount: 5900, defaultAmount: 3600, firstDefaultDate: '2026-05-09', recoveryScore: 52 },
    ] },
];

/* 户明细示例名称池（项目缺少明细数据时生成示例用，预留后端接入位置） */
const NPA_HOUSEHOLD_NAME_POOL = [
    '恒信置业有限公司', '龙腾建设集团有限公司', '中盛商贸有限公司', '华宇建材有限公司',
    '金桥物流有限公司', '远大电子科技有限公司', '蓝海餐饮管理有限公司', '瑞丰机械制造有限公司',
    '天成纺织有限公司', '安泰工程承包有限公司',
];

/* 获取项目户明细：优先使用项目自带明细，缺失时按项目生成示例明细 */
function npaHouseholds(project) {
    if (Array.isArray(project.households) && project.households.length) return project.households;
    const count = Math.max(1, Math.min(Number(project.defaultCount) || 3, 6));
    const total = Number(project.totalAmount) || 1000;
    const seed = String(project.id || 'npa').split('').reduce((s, c) => s + c.charCodeAt(0), 0);
    const list = [];
    for (let i = 0; i < count; i++) {
        const assetAmount = Math.round((total / count) * (0.35 + ((seed + i * 7) % 50) / 100) * 100) / 100;
        const defaultAmount = Math.round(assetAmount * (0.55 + ((seed + i * 3) % 30) / 100) * 100) / 100;
        const year = 2025 + (i % 2);
        const month = String(1 + ((seed + i * 5) % 12)).padStart(2, '0');
        const day = String(1 + ((seed + i * 11) % 28)).padStart(2, '0');
        const defaultRate = assetAmount > 0 ? defaultAmount / assetAmount : 0.6;
        list.push({
            name: NPA_HOUSEHOLD_NAME_POOL[(seed + i) % NPA_HOUSEHOLD_NAME_POOL.length],
            assetAmount,
            defaultAmount,
            firstDefaultDate: `${year}-${month}-${day}`,
            // 回收评分：违约占比越低，回收可能性越高
            recoveryScore: Math.max(5, Math.min(95, Math.round((1 - defaultRate) * 100))),
        });
    }
    return list;
}

/* 不良资产项目本地存储（预留后端接入位置；当前按用户持久化到 localStorage） */
const npaStore = {
    _key(username) {
        if (!username) return null;
        return NPA_STORAGE_PREFIX + encodeURIComponent(username.toLowerCase());
    },
    load(username) {
        const key = this._key(username);
        if (!key) return [];
        let projects = null;
        try {
            const raw = localStorage.getItem(key);
            projects = raw ? JSON.parse(raw) : null;
        } catch { projects = null; }
        if (!Array.isArray(projects) || projects.length === 0) {
            projects = NPA_SEED.map(s => ({ ...s, id: genId() }));
            this.save(username, projects);
        }
        return projects;
    },
    save(username, projects) {
        const key = this._key(username);
        if (!key) return;
        try { localStorage.setItem(key, JSON.stringify(projects)); } catch (e) { console.warn('NPA 项目保存失败:', e); }
    },
};

function npaStatusText(progress) {
    if (progress >= 100) return '已完成';
    if (progress > 0) return '处置中';
    return '未启动';
}

function npaStatusClass(progress) {
    if (progress >= 100) return 'done';
    if (progress > 0) return 'doing';
    return 'todo';
}

/* 分析进度直线颜色：进度 >= 50% 用绿色，否则用红色 */
function npaProgressClass(progress) {
    return Number(progress) >= 50 ? 'good' : 'bad';
}

/* 回收评分等级与悬停提示：>=80 高，>=50 中，<50 低 */
function npaScoreClass(score) {
    if (score >= 80) return 'high';
    if (score >= 50) return 'mid';
    return 'low';
}

function npaRecoveryTip(score) {
    if (score >= 80) return '回收可能性：高';
    if (score >= 50) return '回收可能性：中等';
    return '回收可能性：较低';
}

function formatAmount(value) {
    return Number(value || 0).toLocaleString('zh-CN');
}

export const HomePage = {
    comp: null,
    els: {},
    handlers: {},
    username: '',
    currentMenu: 'dashboard',
    skillsFilter: 'all',
    npaProjects: [],
    pendingDeleteId: null,
    currentHousehold: null,
    currentFileText: '',

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
            // 我的项目：新建项目弹窗
            npaCreateModal: comp.query('#npaCreateModal'),
            btnNpaCancel: comp.query('#btnNpaCancel'),
            btnNpaConfirm: comp.query('#btnNpaConfirm'),
            npaName: comp.query('#npaName'),
            npaFile: comp.query('#npaFile'),
            npaFormError: comp.query('#npaFormError'),
            // 我的项目：删除确认弹窗
            npaDeleteConfirm: comp.query('#npaDeleteConfirm'),
            btnNpaDeleteCancel: comp.query('#btnNpaDeleteCancel'),
            btnNpaDeleteConfirm: comp.query('#btnNpaDeleteConfirm'),
            npaDeleteConfirmText: comp.query('#npaDeleteConfirmText'),
            // 我的项目：户分析与格式文件弹窗
            npaFileModal: comp.query('#npaFileModal'),
            btnNpaFileClose: comp.query('#btnNpaFileClose'),
            btnNpaFileDownload: comp.query('#btnNpaFileDownload'),
            npaFileView: comp.query('#npaFileView'),
            // 用户菜单（点击登录名弹出）
            homeUserInfo: comp.query('#homeUserInfo'),
            userMenu: comp.query('#userMenu'),
            btnChangePassword: comp.query('#btnChangePassword'),
            btnEditProfile: comp.query('#btnEditProfile'),
            // 修改密码弹窗
            changePwdModal: comp.query('#changePwdModal'),
            btnChangePwdCancel: comp.query('#btnChangePwdCancel'),
            btnChangePwdConfirm: comp.query('#btnChangePwdConfirm'),
            oldPassword: comp.query('#oldPassword'),
            newPassword: comp.query('#newPassword'),
            confirmPassword: comp.query('#confirmPassword'),
            changePwdError: comp.query('#changePwdError'),
            // 修改个人信息弹窗
            editProfileModal: comp.query('#editProfileModal'),
            btnEditProfileCancel: comp.query('#btnEditProfileCancel'),
            btnEditProfileConfirm: comp.query('#btnEditProfileConfirm'),
            profileDisplayName: comp.query('#profileDisplayName'),
            profileEmail: comp.query('#profileEmail'),
            editProfileError: comp.query('#editProfileError'),
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

        // 我的项目：动态内容通过事件委托绑定（内容随菜单切换重建，避免重复绑定）
        this.els.contentView.addEventListener('click', (e) => {
            if (e.target.closest('#btnNpaAdd')) { this.openNpaCreate(); return; }
            const expandBtn = e.target.closest('[data-npa-expand]');
            if (expandBtn) { this.toggleNpaExpand(expandBtn.dataset.npaExpand); return; }
            const fileBtn = e.target.closest('[data-npa-household]');
            if (fileBtn) { this.openNpaHouseholdFile(fileBtn.dataset.npaHousehold); return; }
            const skillFilterBtn = e.target.closest('[data-skills-filter]');
            if (skillFilterBtn) { this.setSkillsFilter(skillFilterBtn.dataset.skillsFilter); return; }
            const delBtn = e.target.closest('[data-npa-delete]');
            if (delBtn) { this.openNpaDelete(delBtn.dataset.npaDelete); return; }
        });
        // 系统设置：Skills 搜索框输入过滤
        this.els.contentView.addEventListener('input', (e) => {
            if (e.target.id === 'skillsSearch') this._applySkillFilters();
        });
        this.els.btnNpaCancel.addEventListener('click', () => this.closeNpaCreate());
        this.els.btnNpaConfirm.addEventListener('click', () => this.submitNpaCreate());
        this.els.npaCreateModal.addEventListener('click', (e) => {
            if (e.target === this.els.npaCreateModal) this.closeNpaCreate();
        });

        // 我的项目：删除确认弹窗
        this.els.btnNpaDeleteCancel.addEventListener('click', () => this.closeNpaDelete());
        this.els.btnNpaDeleteConfirm.addEventListener('click', () => this.confirmNpaDelete());
        this.els.npaDeleteConfirm.addEventListener('click', (e) => {
            if (e.target === this.els.npaDeleteConfirm) this.closeNpaDelete();
        });

        // 我的项目：户分析与格式文件弹窗
        this.els.btnNpaFileClose.addEventListener('click', () => this.closeNpaHouseholdFile());
        this.els.btnNpaFileDownload.addEventListener('click', () => this.downloadNpaHouseholdFile());
        this.els.npaFileModal.addEventListener('click', (e) => {
            if (e.target === this.els.npaFileModal) this.closeNpaHouseholdFile();
        });

        // 用户菜单：点击登录名弹出 修改密码 / 修改个人信息
        this.els.homeUserInfo.addEventListener('click', (e) => {
            e.stopPropagation();
            this.toggleUserMenu();
        });
        this.els.btnChangePassword.addEventListener('click', () => {
            this.closeUserMenu();
            this.openChangePwdModal();
        });
        this.els.btnEditProfile.addEventListener('click', () => {
            this.closeUserMenu();
            this.openEditProfileModal();
        });
        this.els.homePage.addEventListener('click', (e) => {
            if (!e.target.closest('.home-user-wrap')) this.closeUserMenu();
        });

        // 修改密码弹窗
        this.els.btnChangePwdCancel.addEventListener('click', () => this.closeChangePwdModal());
        this.els.btnChangePwdConfirm.addEventListener('click', () => this.submitChangePwd());
        this.els.changePwdModal.addEventListener('click', (e) => {
            if (e.target === this.els.changePwdModal) this.closeChangePwdModal();
        });

        // 修改个人信息弹窗
        this.els.btnEditProfileCancel.addEventListener('click', () => this.closeEditProfileModal());
        this.els.btnEditProfileConfirm.addEventListener('click', () => this.submitEditProfile());
        this.els.editProfileModal.addEventListener('click', (e) => {
            if (e.target === this.els.editProfileModal) this.closeEditProfileModal();
        });

        // 默认展示 dashboard
        this.selectMenu('dashboard');
    },

    show() {
        this.closeLogoutConfirm();
        this.closeNpaDelete();
        this.closeNpaHouseholdFile();
        this.closeUserMenu();
        this.closeChangePwdModal();
        this.closeEditProfileModal();
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
        this.els.contentView.innerHTML = menu.id === 'settings'
            ? this.renderSettings()
            : menu.id === 'npa'
                ? this.renderNpa()
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

    /* ========== 系统设置内容：Agents / Skills / MCP / Storages / Knowledge 图卡 ========== */
    renderSettings() {
        const agentsHtml = AGENTS.map(a => `
            <div class="agent-card" data-agent="${a.id}">
                <span class="card-check">✓</span>
                <div class="card-icon">${a.icon}</div>
                <div class="card-name">${a.name}${a.default ? '<span class="card-badge">默认</span>' : ''}</div>
                <div class="card-desc">${a.desc}</div>
                <div class="card-meta">模型：${a.model}</div>
                <div class="card-status">
                    <span class="card-status-dot ${a.status === '运行中' ? 'on' : 'off'}"></span>${a.status}
                </div>
            </div>
        `).join('');

        const skillsHtml = SKILLS.map(s => `
            <div class="skill-card" data-skill="${s.id}" data-skill-name="${s.name.toLowerCase()}" data-skill-status="${s.enabled ? 'enabled' : 'disabled'}">
                <span class="card-check">✓</span>
                <div class="card-icon">${s.icon}</div>
                <div class="card-name">${s.name}</div>
                <div class="card-desc">${s.desc}</div>
                <div class="card-status">
                    <span class="card-status-dot ${s.enabled ? 'on' : 'off'}"></span>${s.enabled ? '已启用' : '未启用'}
                </div>
            </div>
        `).join('');

        const mcpHtml = MCP_SERVERS.map(m => `
            <div class="mini-card mcp-card" data-mcp="${m.id}">
                <span class="card-check">✓</span>
                <div class="card-icon">${m.icon}</div>
                <div class="card-name">${m.name}</div>
                <div class="card-desc">传输：${m.transport}</div>
                <div class="card-status">
                    <span class="card-status-dot ${m.enabled ? 'on' : 'off'}"></span>${m.enabled ? '已连接' : '未连接'}
                </div>
            </div>
        `).join('');

        const storageHtml = STORAGES.map(s => `
            <div class="mini-card storage-card" data-storage="${s.id}">
                <span class="card-check">✓</span>
                <div class="card-icon">${s.icon}</div>
                <div class="card-name">${s.name}</div>
                <div class="card-desc">类型：${s.type}</div>
                <div class="card-status">
                    <span class="card-status-dot ${s.enabled ? 'on' : 'off'}"></span>${s.enabled ? '可用' : '不可用'}
                </div>
            </div>
        `).join('');

        const knowledgeHtml = KNOWLEDGE.map(k => `
            <div class="mini-card knowledge-card" data-knowledge="${k.id}">
                <span class="card-check">✓</span>
                <div class="card-icon">${k.icon}</div>
                <div class="card-name">${k.name}</div>
                <div class="card-desc">条目：${formatAmount(k.entries)}</div>
                <div class="card-status">
                    <span class="card-status-dot ${k.enabled ? 'on' : 'off'}"></span>${k.enabled ? '已索引' : '未索引'}
                </div>
            </div>
        `).join('');

        return `
            <div class="content-toolbar">
                <div class="content-title-block">
                    <h2>系统设置</h2>
                </div>
            </div>

            <div class="settings-section">
                <div class="settings-section-head">
                    <div class="settings-section-title">Agents（智能体）<span class="settings-section-count">共 ${AGENTS.length} 个</span></div>
                </div>
                <div class="agent-grid">
                    ${agentsHtml}
                </div>
            </div>

            <div class="settings-section">
                <div class="settings-section-head">
                    <div class="settings-section-title">Skills（技能）<span class="settings-section-count">共 ${SKILLS.length} 个</span><span class="skills-filter" role="tablist">
                        <button type="button" class="skills-filter-btn active" data-skills-filter="all">全部</button>
                        <button type="button" class="skills-filter-btn" data-skills-filter="enabled">已启用</button>
                        <button type="button" class="skills-filter-btn" data-skills-filter="disabled">未启用</button>
                    </span><input type="text" id="skillsSearch" class="setting-input skills-search" placeholder="搜索技能" /></div>
                </div>
                <div class="skill-grid">
                    ${skillsHtml}
                </div>
            </div>

            <div class="settings-section">
                <div class="settings-section-head">
                    <div class="settings-section-title">MCP Servers（MCP 服务器）<span class="settings-section-count">共 ${MCP_SERVERS.length} 个</span></div>
                </div>
                <div class="mini-grid">
                    ${mcpHtml}
                </div>
            </div>

            <div class="settings-section">
                <div class="settings-section-head">
                    <div class="settings-section-title">Storages（存储）<span class="settings-section-count">共 ${STORAGES.length} 个</span></div>
                </div>
                <div class="mini-grid">
                    ${storageHtml}
                </div>
            </div>

            <div class="settings-section">
                <div class="settings-section-head">
                    <div class="settings-section-title">Knowledge（知识库）<span class="settings-section-count">共 ${KNOWLEDGE.length} 个</span></div>
                </div>
                <div class="mini-grid">
                    ${knowledgeHtml}
                </div>
            </div>
        `;
    },

    /* ========== 我的项目（NPA）内容 ========== */
    _loadNpa() {
        this.npaProjects = npaStore.load(this.username);
    },

    renderNpa() {
        this._loadNpa();
        const projects = this.npaProjects;

        const totalAmount = projects.reduce((s, p) => s + Number(p.totalAmount || 0), 0);
        const totalDefault = projects.reduce((s, p) => s + Number(p.defaultCount || 0), 0);
        const avgProgress = projects.length
            ? Math.round(projects.reduce((s, p) => s + Number(p.progress || 0), 0) / projects.length)
            : 0;

        const stats = [
            { label: '处置项目', value: projects.length, icon: '🏦', delta: '', up: true },
            { label: '资产总额（万元）', value: formatAmount(totalAmount), icon: '💰', delta: '', up: true },
            { label: '违约户数', value: formatAmount(totalDefault), icon: '📑', delta: '', up: true },
            { label: '平均分析进度', value: avgProgress + '%', icon: '📈', delta: '', up: true },
        ];
        const statsHtml = stats.map(s => `
            <div class="stat-card">
                <div class="stat-icon">${s.icon}</div>
                <div class="stat-info">
                    <div class="stat-value">${s.value}</div>
                    <div class="stat-label">${s.label}</div>
                </div>
                <div class="stat-delta ${s.up ? 'up' : 'down'}">${s.delta}</div>
            </div>
        `).join('');

        const rowsHtml = projects.length
            ? projects.map(p => {
                const statusClass = npaStatusClass(p.progress);
                const statusText = npaStatusText(p.progress);
                const households = npaHouseholds(p);
                const subRowsHtml = households.map((h, idx) => `
                    <tr>
                        <td class="npa-sub-name">${h.name}</td>
                        <td class="npa-num">${formatAmount(h.assetAmount)}</td>
                        <td class="npa-amount">${formatAmount(h.defaultAmount)}</td>
                        <td class="npa-date">${h.firstDefaultDate}</td>
                        <td class="npa-score-cell">
                            <span class="npa-score ${npaScoreClass(h.recoveryScore)}" data-tip="${npaRecoveryTip(h.recoveryScore)}（评分 ${h.recoveryScore}）">${h.recoveryScore}</span>
                        </td>
                        <td class="npa-file-cell">
                            <button type="button" class="btn-npa-file" data-npa-household="${p.id}::${idx}" title="查看分析及格式文件">📄 查看</button>
                        </td>
                    </tr>
                `).join('');
                return `
                    <tr>
                        <td class="npa-expand-cell">
                            <button type="button" class="btn-npa-expand" data-npa-expand="${p.id}" title="展开户明细">▶</button>
                        </td>
                        <td class="npa-name">${p.name}</td>
                        <td class="npa-num">${formatAmount(p.defaultCount)}</td>
                        <td class="npa-amount">${formatAmount(p.totalAmount)}</td>
                        <td class="npa-recovered">${formatAmount(p.recoveredAmount)}</td>
                        <td class="npa-date">${p.createdDate}</td>
                        <td>
                            <div class="npa-progress">
                                <div class="npa-progress-track">
                                    <div class="npa-progress-fill ${npaProgressClass(p.progress)}" style="width:${Math.min(100, Math.max(0, Number(p.progress) || 0))}%"></div>
                                </div>
                                <span class="npa-progress-text">${p.progress}%</span>
                            </div>
                        </td>
                        <td><span class="status-badge ${statusClass}">${statusText}</span></td>
                        <td class="npa-ops">
                            <button type="button" class="btn-npa-delete" data-npa-delete="${p.id}" title="删除项目">🗑</button>
                        </td>
                    </tr>
                    <tr class="npa-detail-row" data-npa-detail="${p.id}" hidden>
                        <td colspan="9" class="npa-detail-cell">
                            <div class="npa-detail-wrap">
                                <div class="npa-detail-title">户明细（${households.length} 户）</div>
                                <table class="npa-sub-table">
                                    <thead>
                                        <tr>
                                            <th>户名称</th>
                                            <th>资产总额（万元）</th>
                                            <th>违约金额（万元）</th>
                                            <th>首次违约时间</th>
                                            <th>回收评分</th>
                                            <th>格式文件</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        ${subRowsHtml}
                                    </tbody>
                                </table>
                            </div>
                        </td>
                    </tr>
                `;
            }).join('')
            : `<tr><td colspan="9" class="npa-empty">暂无项目，点击右上角「新建项目」创建</td></tr>`;

        return `
            <div class="stat-grid">
                ${statsHtml}
            </div>

            <div class="content-toolbar">
                <div class="content-title-block">
                    <h2>我的项目</h2>
                    <p>以项目维度管理资产项目，跟踪违约户数、资产规模与分析进度。</p>
                </div>
                <div class="toolbar-actions">
                    <span class="toolbar-count">共 ${projects.length} 个项目</span>
                    <button type="button" class="btn-primary-add" id="btnNpaAdd">＋ 新建项目</button>
                </div>
            </div>

            <div class="table-wrap">
                <table class="data-table npa-table">
                    <thead>
                        <tr>
                            <th class="npa-expand-th"></th>
                            <th>项目名称</th>
                            <th>违约户数</th>
                            <th>资产总额（万元）</th>
                            <th>已处置金额（万元）</th>
                            <th>创建日期</th>
                            <th>分析进度</th>
                            <th>状态</th>
                            <th>操作</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${rowsHtml}
                    </tbody>
                </table>
            </div>
        `;
    },

    openNpaCreate() {
        this.els.npaName.value = '';
        this.els.npaFile.value = '';
        this.hideNpaFormError();
        this.els.npaCreateModal.classList.add('show');
        this.els.npaName.focus();
    },

    closeNpaCreate() {
        this.els.npaCreateModal.classList.remove('show');
    },

    async submitNpaCreate() {
        const name = this.els.npaName.value.trim();
        const file = this.els.npaFile.files?.[0] || null;

        if (!name) { this.showNpaFormError('请填写项目名称'); return; }

        // 由项目文件自动分析资产总额与分析进度（预留后端分析接入位置）
        const analyzed = await this._analyzeNpaFile(file);

        this.npaProjects.unshift({
            id: genId(),
            name,
            defaultCount: analyzed.defaultCount,
            totalAmount: analyzed.totalAmount,
            recoveredAmount: analyzed.recoveredAmount,
            progress: analyzed.progress,
            createdDate: new Date().toISOString().slice(0, 10), // 提交时即为当前日期
        });
        npaStore.save(this.username, this.npaProjects);
        this.closeNpaCreate();
        this.selectMenu('npa');
    },

    /**
     * 分析项目文件，提取资产指标。
     * 当前为前端轻量解析（支持 JSON：defaultCount/totalAmount/recoveredAmount/progress），
     * 无法解析时按 0 处理；后续可替换为上传到后端由真实算法分析。
     * @param {File|null} file
     * @returns {Promise<{defaultCount:number,totalAmount:number,recoveredAmount:number,progress:number}>}
     */
    async _analyzeNpaFile(file) {
        const stats = { defaultCount: 0, totalAmount: 0, recoveredAmount: 0, progress: 0 };
        if (!file) return stats;
        try {
            const text = await file.text();
            const data = JSON.parse(text);
            const num = (v) => Number(v);
            if (Number.isFinite(num(data.defaultCount)) && num(data.defaultCount) >= 0) stats.defaultCount = num(data.defaultCount);
            if (Number.isFinite(num(data.totalAmount)) && num(data.totalAmount) >= 0) stats.totalAmount = num(data.totalAmount);
            if (Number.isFinite(num(data.recoveredAmount)) && num(data.recoveredAmount) >= 0) stats.recoveredAmount = num(data.recoveredAmount);
            if (Number.isFinite(num(data.progress))) stats.progress = Math.min(100, Math.max(0, num(data.progress)));
        } catch {
            // 文件无法解析：保留 0，交由后续系统分析
        }
        return stats;
    },

    /* 展开/收起项目户明细 */
    toggleNpaExpand(id) {
        const detailRow = this.els.contentView.querySelector(`.npa-detail-row[data-npa-detail="${id}"]`);
        if (!detailRow) return;
        const btn = this.els.contentView.querySelector(`.btn-npa-expand[data-npa-expand="${id}"]`);
        const isHidden = detailRow.hidden;
        detailRow.hidden = !isHidden;
        if (btn) {
            btn.textContent = isHidden ? '▼' : '▶';
            btn.classList.toggle('expanded', isHidden);
        }
    },

    /* 打开户格式文件弹窗：展示分析与整理好的格式文件 */
    openNpaHouseholdFile(ref) {
        const [projectId, idx] = String(ref || '').split('::');
        const project = this.npaProjects.find(p => p.id === projectId);
        if (!project) return;
        const households = npaHouseholds(project);
        const h = households[Number(idx)];
        if (!h) return;
        this.currentHousehold = { project, household: h };
        this.currentFileText = this._buildHouseholdFileText(project, h);

        this.els.npaFileView.innerHTML = `
            <div class="npa-file-analysis">
                <div class="npa-file-label">分析</div>
                <p>该户「${h.name}」资产总额 ${formatAmount(Number(h.assetAmount) || 0)} 万元，其中违约金额 ${formatAmount(Number(h.defaultAmount) || 0)} 万元，违约率约 ${this._householdDefaultRate(h)}%，首次违约时间为 ${h.firstDefaultDate}，当前风险等级为「${this._householdRiskLevel(h)}」。</p>
            </div>
            <div class="npa-file-block">
                <div class="npa-file-label">格式文件</div>
                <pre class="npa-file-pre">${this.currentFileText}</pre>
            </div>
        `;
        this.els.npaFileModal.classList.add('show');
    },

    closeNpaHouseholdFile() {
        this.els.npaFileModal.classList.remove('show');
        this.currentHousehold = null;
        this.currentFileText = '';
    },

    downloadNpaHouseholdFile() {
        if (!this.currentHousehold || !this.currentFileText) return;
        const name = this.currentHousehold.household.name || '户信息档案';
        const blob = new Blob([this.currentFileText], { type: 'text/plain;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${name}.txt`;
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
    },

    /* 户违约率（%） */
    _householdDefaultRate(h) {
        const asset = Number(h.assetAmount) || 0;
        const def = Number(h.defaultAmount) || 0;
        return asset > 0 ? Math.round(def / asset * 100) : 0;
    },

    /* 户风险等级 */
    _householdRiskLevel(h) {
        const rate = this._householdDefaultRate(h);
        return rate >= 80 ? '高' : rate >= 50 ? '中' : '低';
    },

    /* 生成整理好的格式文件内容 */
    _buildHouseholdFileText(project, h) {
        return [
            '【户信息档案】',
            '==============================',
            `户名称      ：${h.name}`,
            `所属项目    ：${project.name}`,
            `资产总额    ：${formatAmount(Number(h.assetAmount) || 0)} 万元`,
            `违约金额    ：${formatAmount(Number(h.defaultAmount) || 0)} 万元`,
            `违约率      ：${this._householdDefaultRate(h)}%`,
            `首次违约时间：${h.firstDefaultDate}`,
            `风险等级    ：${this._householdRiskLevel(h)}`,
            `状态        ：违约`,
        ].join('\n');
    },

    /* 打开删除确认弹窗（风格与退出确认一致） */
    openNpaDelete(id) {
        const project = this.npaProjects.find(p => p.id === id);
        if (!project) return;
        this.pendingDeleteId = id;
        this.els.npaDeleteConfirmText.textContent = `确定删除项目「${project.name}」吗？删除后不可恢复。`;
        this.els.npaDeleteConfirm.classList.add('show');
    },

    closeNpaDelete() {
        this.pendingDeleteId = null;
        this.els.npaDeleteConfirm.classList.remove('show');
    },

    confirmNpaDelete() {
        if (!this.pendingDeleteId) return;
        this.npaProjects = this.npaProjects.filter(p => p.id !== this.pendingDeleteId);
        npaStore.save(this.username, this.npaProjects);
        this.closeNpaDelete();
        this.selectMenu('npa');
    },

    showNpaFormError(msg) {
        this.els.npaFormError.textContent = msg;
        this.els.npaFormError.style.display = 'block';
    },

    hideNpaFormError() {
        this.els.npaFormError.textContent = '';
        this.els.npaFormError.style.display = 'none';
    },

    /* ========== 用户信息 / 侧边栏 ========== */
    updateUserInfo(username, displayName) {
        this.username = username || '';
        const display = displayName || username || '用户';
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

    /* 控制 AI 助手浮动气泡显示/隐藏（聊天抽屉打开时隐藏，避免遮挡发送按钮） */
    setChatBubbleVisible(visible) {
        if (this.els.chatBubble) {
            this.els.chatBubble.style.display = visible ? '' : 'none';
        }
    },

    /* ========== 用户菜单：修改密码 / 修改个人信息 ========== */
    toggleUserMenu() {
        const isOpen = this.els.userMenu.classList.contains('show');
        this.els.userMenu.classList.toggle('show', !isOpen);
    },

    closeUserMenu() {
        this.els.userMenu.classList.remove('show');
    },

    openChangePwdModal() {
        this.els.oldPassword.value = '';
        this.els.newPassword.value = '';
        this.els.confirmPassword.value = '';
        this.hideFormError(this.els.changePwdError);
        this.els.changePwdModal.classList.add('show');
        this.els.oldPassword.focus();
    },

    closeChangePwdModal() {
        this.els.changePwdModal.classList.remove('show');
    },

    submitChangePwd() {
        const oldPwd = this.els.oldPassword.value.trim();
        const newPwd = this.els.newPassword.value.trim();
        const confirmPwd = this.els.confirmPassword.value.trim();
        if (!oldPwd) { this.showFormError(this.els.changePwdError, '请输入原密码'); return; }
        if (!newPwd) { this.showFormError(this.els.changePwdError, '请输入新密码'); return; }
        if (newPwd.length < 6) { this.showFormError(this.els.changePwdError, '新密码至少 6 位'); return; }
        if (newPwd !== confirmPwd) { this.showFormError(this.els.changePwdError, '两次输入的新密码不一致'); return; }
        // 预留后端修改密码接口
        this.closeChangePwdModal();
    },

    openEditProfileModal() {
        this.els.profileDisplayName.value = this.username;
        this.els.profileEmail.value = '';
        this.hideFormError(this.els.editProfileError);
        this.els.editProfileModal.classList.add('show');
        this.els.profileDisplayName.focus();
    },

    closeEditProfileModal() {
        this.els.editProfileModal.classList.remove('show');
    },

    submitEditProfile() {
        const displayName = this.els.profileDisplayName.value.trim();
        if (!displayName) { this.showFormError(this.els.editProfileError, '请输入显示名称'); return; }
        // 更新本地用户信息并持久化（预留后端接口）
        this.updateUserInfo(this.username, displayName);
        if (State.user) {
            State.user.displayName = displayName;
            State.saveSession();
        }
        this.closeEditProfileModal();
    },

    showFormError(el, msg) {
        el.textContent = msg;
        el.style.display = 'block';
    },

    hideFormError(el) {
        el.textContent = '';
        el.style.display = 'none';
    },

    /* ========== 系统设置：Skills 筛选 ========== */
    setSkillsFilter(filter) {
        this.skillsFilter = filter;
        const btns = this.els.contentView.querySelectorAll('[data-skills-filter]');
        btns.forEach(b => b.classList.toggle('active', b.dataset.skillsFilter === filter));
        this._applySkillFilters();
    },

    _applySkillFilters() {
        const query = (this.els.contentView.querySelector('#skillsSearch')?.value || '').trim().toLowerCase();
        const cards = this.els.contentView.querySelectorAll('.skill-card');
        cards.forEach(card => {
            let show = true;
            const status = card.dataset.skillStatus;
            if (this.skillsFilter === 'enabled' && status !== 'enabled') show = false;
            if (this.skillsFilter === 'disabled' && status !== 'disabled') show = false;
            const name = (card.dataset.skillName || '').toLowerCase();
            if (query && !name.includes(query)) show = false;
            card.style.display = show ? '' : 'none';
        });
    },

    openLogoutConfirm() {
        this.els.logoutConfirm.classList.add('show');
    },

    closeLogoutConfirm() {
        this.els.logoutConfirm.classList.remove('show');
    },

    reset() {
        this.closeLogoutConfirm();
        this.closeNpaDelete();
        this.closeNpaHouseholdFile();
        this.closeUserMenu();
        this.closeChangePwdModal();
        this.closeEditProfileModal();
        this.closeSidebar();
        this.npaProjects = [];
        this.pendingDeleteId = null;
        this.currentHousehold = null;
        this.currentFileText = '';
        this.updateUserInfo('');
        this.selectMenu('dashboard');
    },
};

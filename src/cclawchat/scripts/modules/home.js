/**
 * ============================================================
 * 主页组件控制器 (HomePage)
 * ============================================================
 * 登录成功后显示的主页面：
 *   - 顶部内容头：品牌标识 + 面包屑 + 用户菜单（系统设置 / 修改密码 / 修改个人信息）+ 退出登录
 *   - 核心内容显示区域（按菜单动态渲染，登录后直接进入「我的项目」）
 *   - 右上角聊天气泡：点击后由外部以抽屉方式打开 chat.html
 *
 * 高内聚：仅负责主页视图的渲染与交互。
 * 低耦合：不直接依赖 App / ChatPage，通过注入回调与外部协作；
 *         打开聊天抽屉、退出登录等跨组件动作交由外部处理。
 */

import { getInitials, genId, escapeHtml, formatBytes } from '../utils/helper.js';
import { State } from './state.js';
import { API } from './api.js';
import { CONFIG } from './config.js';

/* ---------- 内容页定义 ----------
   npa：默认落地页；settings：右上角用户菜单入口（点击后替换主内容区显示） */
const MENUS = {
    npa: { id: 'npa', label: '我的项目', title: '我的项目' },
    settings: { id: 'settings', label: '系统设置', title: '系统设置' },
};

/* 登录后的默认落地页：我的项目 */
const DEFAULT_MENU = 'npa';

/* ---------- 系统设置：Agents / Skills 展示数据（MCP / Storages / Knowledge 暂无后端数据） ---------- */
/* Agents 本地示例数据：后端不可用或 Mock 模式下降级使用（真实数据来自 GET /api/agents） */
const AGENTS_FALLBACK = [
    { id: 'assistant', name: '通用助手', icon: '🤖', desc: '日常问答与通用任务处理', model: 'cclaw-gpt-4o', status: '运行中', default: true },
    { id: 'npa-manager', name: '不良资产管理', icon: '🏦', desc: '不良资产处置、分析与进度跟踪', model: 'cclaw-gpt-4o', status: '运行中', default: false },
    { id: 'data-analyst', name: '数据分析', icon: '📊', desc: '数据统计、洞察与可视化', model: 'cclaw-gpt-4o-mini', status: '运行中', default: false },
    { id: 'report-writer', name: '报告撰写', icon: '📝', desc: '自动生成结构化业务报告', model: 'cclaw-gpt-4o-mini', status: '已暂停', default: false },
    { id: 'risk-watch', name: '风险预警', icon: '⚠️', desc: '实时监控风险并生成预警', model: 'cclaw-gpt-4o', status: '运行中', default: false },
];

/* 智能体详情 Tab 页签定义 */
const AGENT_TABS = [
    { id: 'basic', label: '基本信息' },
    { id: 'workspace', label: '工作空间' },
];

/* 智能体详情编辑表单字段（显式排除 owner_id / agent_id / d_version 等只读或后端维护字段）
   tab: basic=基本信息页签（身份/模型/形象 + 运行配置）；workspace=工作空间页签（启动文件 + 技能，见 _renderWorkspaceFiles） */
const AGENT_FORM_FIELDS = [
    // 基本信息页签
    { key: 'AgentName', label: '名称', type: 'text', required: true, maxlength: 64, tab: 'basic', section: '基本信息' },
    { key: 'Description', label: '描述', type: 'text', maxlength: 128, tab: 'basic', section: '基本信息' },
    { key: 'IsDefault', label: '默认 Agent', type: 'switch', tab: 'basic', section: '基本信息' },
    { key: 'Disabled', label: '已禁用', type: 'switch', tab: 'basic', section: '基本信息' },
    { key: 'PrimaryModel', label: '主模型', type: 'text', required: true, maxlength: 32, tab: 'basic', section: '模型配置' },
    { key: 'Fallbacks', label: '兜底模型', type: 'text', maxlength: 128, tab: 'basic', section: '模型配置' },
    { key: 'ImageName', label: '形象名称', type: 'text', maxlength: 32, tab: 'basic', section: '形象配置' },
    { key: 'ImageEmoji', label: '形象表情', type: 'emoji', tab: 'basic', section: '形象配置' },
    { key: 'ImageTheme', label: '形象主题', type: 'text', maxlength: 64, tab: 'basic', section: '形象配置' },
    { key: 'CompactionMode', label: '压缩模式', type: 'select', options: [{ value: 0, label: '整体摘要' }, { value: 1, label: '分块摘要' }], tab: 'basic', section: '上下文与压缩' },
    // reverse_token_floor 是**系统提示预留**（启动文档 + 折叠进 system 的历史摘要的额度），
    // 约束的是"历史可以占多少"，不是"历史至少要保留多少" —— 旧标签把方向写反了（当成历史下限）。
    { key: 'ReverseTokenFloor', label: '系统提示预留 Token', type: 'number', min: 1, tab: 'basic', section: '上下文与压缩' },
    { key: 'MaxConcurrents', label: '最大并发数', type: 'number', min: 1, max: 10, tab: 'basic', section: '并发与限制' },
    { key: 'SubAgents', label: '子 Agent 上限', type: 'number', min: 1, max: 8, step: 1, tab: 'basic', section: '并发与限制' },
    { key: 'ReasoningMode', label: '推理展示方式', type: 'select', options: [{ value: 0, label: '不展示' }, { value: 1, label: '生成后附带' }, { value: 2, label: '实时流' }], tab: 'basic', section: '推理与工具' },
    { key: 'ToolProgress', label: '工具执行进度', type: 'select', options: [{ value: 0, label: '简洁' }, { value: 1, label: '附具体命令' }], tab: 'basic', section: '推理与工具' },
];

/* 形象表情选择列表（表情选择器） */
const EMOJI_PICKER_OPTIONS = [
    '😀', '😃', '😄', '😁', '😆', '😅', '😂', '🤣', '😊', '😇', '🙂', '😉',
    '😍', '🥰', '😘', '😜', '🤪', '😎', '🤓', '🥳', '😏', '😌', '😴', '🤔',
    '🤗', '🤩', '😭', '😢', '😡', '😱', '😷', '🤒', '🥺', '😳', '🤯', '😇',
    '🤖', '👾', '👽', '🤝', '👍', '👎', '👏', '🙌', '👌', '🤙', '💪', '✌️',
    '🐱', '🐶', '🦊', '🐼', '🐨', '🐯', '🦁', '🐮', '🐷', '🐸', '🐵', '🦄',
    '🌈', '⭐', '🔥', '💡', '💎', '💰', '🏆', '🎯', '🎨', '🚀', '🏦', '⚖️',
];

/* 详情表单必填校验提示 */
const AGENT_FORM_REQUIRED = {
    AgentName: '名称',
    PrimaryModel: '主模型',
};

const SKILLS = [
    { id: 'code-search', name: '代码搜索', icon: '🔍', desc: '在工作区中检索代码与文档', enabled: true },
    { id: 'web-fetch', name: '网页抓取', icon: '🌐', desc: '抓取并解析网页内容', enabled: true },
    { id: 'data-viz', name: '数据可视化', icon: '📈', desc: '将数据渲染为图表', enabled: true },
    { id: 'file-parse', name: '文件解析', icon: '📄', desc: '解析 JSON / CSV / 文本文件', enabled: true },
    { id: 'memory', name: '记忆管理', icon: '🧠', desc: '跨会话持久化知识与偏好', enabled: true },
    { id: 'scheduler', name: '定时任务', icon: '⏰', desc: '按计划自动执行任务', enabled: false },
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

/* 宽松布尔转换：兼容 true/false、1/0、'1'/'true' 等后端常见取值 */
function toBool(value) {
    if (typeof value === 'boolean') return value;
    if (typeof value === 'number') return value !== 0;
    if (typeof value === 'string') {
        const t = value.trim().toLowerCase();
        return t === '1' || t === 'true' || t === 'yes';
    }
    return false;
}

/* 后端 TAgent → 前端展示卡片模型（防御性归一化，兼容大小写字段） */
function normalizeAgent(agent) {
    if (!agent || typeof agent !== 'object') return null;
    const id = String(agent.AgentId || agent.agentId || genId());
    const disabled = Boolean(agent.Disabled ?? agent.disabled);
    return {
        id,
        name: String(agent.AgentName || agent.ImageName || agent.agentName || '未命名智能体'),
        icon: String(agent.ImageEmoji || agent.imageEmoji || '🤖'),
        desc: String(agent.Description || agent.description || agent.Workspace || agent.workspace || ''),
        model: String(agent.PrimaryModel || agent.primaryModel || '—'),
        status: disabled ? '已暂停' : '运行中',
        // 默认 Agent 标记：兼容后端字段 is_default / IsDefault
        default: toBool(agent.is_default ?? agent.IsDefault),
        // 保留后端原始对象，供详情页展示完整字段
        raw: agent,
    };
}

/* SKILL 作用域文案：与后端 SkillScope 常量一致（0 系统 / 1 个人 / 2 项目 / 3 插件） */
const SKILL_SCOPE_LABELS = ['系统', '个人', '项目', '插件'];

function skillScopeLabel(scope) {
    return SKILL_SCOPE_LABELS[Number(scope)] || '未知';
}

/* 后端 Skill 作用域 → 卡片图标（后端无图标字段，按作用域映射兜底，保持 Mock 卡片图标位风格） */
const SKILL_SCOPE_ICONS = ['🛠️', '👤', '📁', '🧩'];

function skillScopeIcon(scope) {
    return SKILL_SCOPE_ICONS[Number(scope)] || '🧩';
}

/* 后端 TSkill → 前端展示模型（防御性归一化，兼容大小写字段与本地示例字段） */
function normalizeSkill(skill) {
    if (!skill || typeof skill !== 'object') return null;
    const scope = Number(skill.skillScope ?? skill.SkillScope ?? 0);
    // 兼容后端字段 Disabled/disabled 与本地示例字段 enabled
    const disabled = Boolean(skill.Disabled ?? skill.disabled ?? (skill.enabled !== undefined && !skill.enabled));
    const dir = String(skill.skillDir || skill.SkillDir || '');
    return {
        id: String(skill.skillId || skill.SkillId || skill.id || genId()),
        name: String(skill.skillName || skill.SkillName || skill.name || '未命名技能'),
        // 卡片展示字段：优先取显式提供（Mock 示例），否则按作用域映射图标、用目录/作用域作描述兜底
        icon: String(skill.icon || skillScopeIcon(scope)),
        desc: String(skill.desc || dir || `${skillScopeLabel(scope)}技能`),
        dir,
        version: Number(skill.dVersion ?? skill.DVersion ?? 0) || 0,
        scope,
        scopeLabel: skillScopeLabel(scope),
        enabled: !disabled,
        status: disabled ? '未启用' : '已启用',
        raw: skill,
    };
}

export const HomePage = {
    comp: null,
    els: {},
    handlers: {},
    username: '',
    currentMenu: DEFAULT_MENU,
    menuHistory: [],       // 页面来源栈（仅记录当前页的上一个页面，用于「返回」）
    skillsFilter: 'all',
    agents: null,          // 智能体列表（null=尚未加载，由后端接口填充）
    agentsLoading: false,  // 防止重复并发加载
    currentAgentDetail: null, // 当前智能体详情（null=不在详情页）
    currentAgentTab: 'basic', // 详情页当前激活页签（basic / workspace）
    startupFilesCache: null,  // 当前智能体启动文件清单缓存 { agentId, list }（离开详情页/切换 agent 时失效）
    skills: null,             // 技能列表（null=尚未加载，由后端 GetSkills 填充；公司级，非按 agent）
    _skillsLoadPromise: null, // 进行中的技能列表加载 Promise（去重 + 供等待）
    npaProjects: [],
    pendingDeleteId: null,
    currentHousehold: null,
    currentFileText: '',

    /**
     * 初始化组件
     * @param {object} comp 组件实例（mountComponent 返回）
     * @param {{ onConfirmLogout?: Function }} handlers
     */
    init(comp, handlers = {}) {
        this.comp = comp;
        this.handlers = handlers;
        this.els = {
            homePage: comp.query('#homePage'),
            homeBrand: comp.query('#homeBrand'),
            contentTitle: comp.query('#contentTitle'),
            contentBreadcrumb: comp.query('.content-breadcrumb'),
            crumbHome: comp.query('#crumbHome'),
            contentView: comp.query('#contentView'),
            btnHomeLogout: comp.query('#btnHomeLogout'),
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
            btnSystemSettings: comp.query('#btnSystemSettings'),
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

        // 品牌标识 / 面包屑：品牌与「首页」一键回到我的项目；详情页点当前页签返回系统设置
        this.els.homeBrand.addEventListener('click', (e) => {
            e.preventDefault();
            this.backToDefaultMenu();
        });
        this.els.contentBreadcrumb.addEventListener('click', (e) => {
            if (e.target.closest('#crumbHome')) { this.backToDefaultMenu(); return; }
            if (this.currentAgentDetail) { this.closeAgentDetail(); return; }
            this.backToDefaultMenu();
        });

        // 退出登录：先弹出二次确认（是否真正退出由外部决定）
        this.els.btnHomeLogout.addEventListener('click', () => this.openLogoutConfirm());
        this.els.btnCancelLogout.addEventListener('click', () => this.closeLogoutConfirm());
        this.els.btnConfirmLogout.addEventListener('click', () => this.handlers.onConfirmLogout?.());
        this.els.logoutConfirm.addEventListener('click', (e) => {
            if (e.target === this.els.logoutConfirm) this.closeLogoutConfirm();
        });

        // 动态内容通过事件委托绑定（内容随菜单切换重建，避免重复绑定）
        this.els.contentView.addEventListener('click', (e) => {
            // 表情选择：点击弹层与打开按钮之外先关闭所有表情弹层
            const emojiPopup = e.target.closest('[data-emoji-popup]');
            const emojiPicker = e.target.closest('[data-emoji-picker]');
            if (!emojiPopup && !emojiPicker) this._closeEmojiPopups();
            if (emojiPicker) { this.toggleEmojiPicker(emojiPicker.dataset.emojiPicker); return; }
            const emojiItem = e.target.closest('[data-emoji-item]');
            if (emojiItem) { this.pickEmoji(emojiItem.dataset.emojiItem); return; }

            // 系统设置：智能体卡片 → 详情二级页；详情页返回/保存按钮
            const agentCard = e.target.closest('[data-agent]');
            if (agentCard) { this.openAgentDetail(agentCard.dataset.agent); return; }
            if (e.target.closest('#btnBackToSettings')) { this.closeAgentDetail(); return; }
            // 智能体详情：一键返回我的项目
            if (e.target.closest('#btnBackToHomeFromDetail')) { this.backToDefaultMenu(); return; }
            // 系统设置工具栏：返回进入设置前的页面
            if (e.target.closest('#btnBackFromSettings')) { this.goBack(); return; }
            if (e.target.closest('#btnSaveAgent')) { this.submitAgentEdit(); return; }
            const agentTab = e.target.closest('[data-agent-tab]');
            if (agentTab) { this.switchAgentTab(agentTab.dataset.agentTab); return; }
            const agentCollapse = e.target.closest('[data-agent-collapse]');
            if (agentCollapse) { this.toggleAgentSection(agentCollapse.dataset.agentCollapse, agentCollapse); return; }
            const startupFileBtn = e.target.closest('[data-startup-file]');
            if (startupFileBtn) { this.openStartupFile(startupFileBtn.dataset.startupFile); return; }

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

        // 用户菜单：点击登录名弹出 系统设置 / 修改密码 / 修改个人信息
        this.els.homeUserInfo.addEventListener('click', (e) => {
            e.stopPropagation();
            this.toggleUserMenu();
        });
        this.els.btnSystemSettings.addEventListener('click', () => {
            this.closeUserMenu();
            this.selectMenu('settings');
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

        // 默认展示首个菜单（我的项目）
        this.selectMenu(DEFAULT_MENU);
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
    },

    /**
     * 选择菜单并渲染对应内容
     * @param {string} menuId
     * @param {{ trackHistory?: boolean }} [options] trackHistory=false 时不记录来源（供「返回」自身使用）
     */
    selectMenu(menuId, options = {}) {
        const menu = MENUS[menuId] || MENUS[DEFAULT_MENU];
        const previous = this.currentMenu;
        this.currentMenu = menu.id;
        // 记录来源页：进入非默认页时记录上一页；回到默认页时清空，避免来回切换堆积无效历史
        if (options.trackHistory !== false && menu.id !== previous) {
            this.menuHistory = menu.id === DEFAULT_MENU ? [] : [previous];
        }
        // 切换菜单时退出智能体详情二级页
        this.currentAgentDetail = null;
        this.startupFilesCache = null; // 离开详情页释放启动文件缓存（防跨 agent 残留）
        // 面包屑：不在默认页时「首页」可点击一键返回我的项目
        this._syncBreadcrumb();

        // 高亮用户菜单中的系统设置入口
        this.els.btnSystemSettings.classList.toggle('active', menu.id === 'settings');

        // 更新面包屑标题
        this.els.contentTitle.textContent = menu.label;

        // 渲染核心内容区
        if (menu.id === 'settings') {
            this.els.contentView.innerHTML = this.renderSettings();
            // 智能体列表来自后端，异步加载完成后原地刷新该区块
            this._loadSettingsAgents();
            // 技能列表来自后端 GetSkills（agent 详情页工作空间页签使用），异步加载一次并缓存
            this._loadSettingsSkills();
        } else {
            this.els.contentView.innerHTML = this.renderNpa();
        }
    },

    /* 返回默认页（我的项目）；已在默认页时不做无谓重渲染 */
    backToDefaultMenu() {
        if (this.currentMenu === DEFAULT_MENU && !this.currentAgentDetail) return;
        this.selectMenu(DEFAULT_MENU);
    },

    /* 返回进入当前页之前的页面；无来源记录时回到默认页（我的项目） */
    goBack() {
        const target = this.menuHistory.pop() || DEFAULT_MENU;
        this.selectMenu(target, { trackHistory: false });
    },

    /* 同步面包屑可点击态：非默认页时「首页」可回我的项目；详情页内当前页签可回系统设置 */
    _syncBreadcrumb() {
        this.els.crumbHome.classList.toggle('crumb-link', this.currentMenu !== DEFAULT_MENU);
        this.els.contentTitle.classList.toggle('crumb-link', !!this.currentAgentDetail);
    },

    /* ========== 系统设置内容：Agents / Skills / MCP / Storages / Knowledge 设置区块 ========== */
    renderSettings() {
        // 智能体列表：后端优先，this.agents 由 _loadSettingsAgents() 填充
        const agentsHtml = this._renderAgentCards();

        // 技能列表：后端优先（GetSkills，按当前登录用户 owner 过滤），this.skills 由 _loadSettingsSkills() 填充
        const skillsHtml = this._renderSkillCards();

        return `
            <div class="content-toolbar">
                <div class="content-title-block">
                    <h2>系统设置</h2>
                </div>
                <div class="toolbar-actions">
                    <button type="button" class="btn-modal-cancel btn-back-page" id="btnBackFromSettings" title="返回上一页">← 返回</button>
                </div>
            </div>

            <div class="settings-section">
                <div class="settings-section-head">
                    <div class="settings-section-title">Agents（智能体）<span class="settings-section-count" id="agentCount">${this.agents === null ? '' : `共 ${this.agents.length} 个`}</span></div>
                </div>
                <div class="agent-grid" id="agentGrid">
                    ${agentsHtml}
                </div>
            </div>

            <div class="settings-section">
                <div class="settings-section-head">
                    <div class="settings-section-title">Skills（技能）<span class="settings-section-count" id="skillCount">${this.skills === null ? '' : `共 ${this.skills.length} 个`}</span><span class="skills-filter" role="tablist">
                        <button type="button" class="skills-filter-btn active" data-skills-filter="all">全部</button>
                        <button type="button" class="skills-filter-btn" data-skills-filter="enabled">已启用</button>
                        <button type="button" class="skills-filter-btn" data-skills-filter="disabled">未启用</button>
                    </span><input type="text" id="skillsSearch" class="setting-input skills-search" placeholder="搜索技能" /></div>
                </div>
                <div class="skill-grid" id="skillGrid">
                    ${skillsHtml}
                </div>
            </div>

            <div class="settings-section">
                <div class="settings-section-head">
                    <div class="settings-section-title">MCP Servers（MCP 服务器）<span class="settings-section-count">共 0 个</span></div>
                </div>
                <div class="mini-grid">
                    <div class="settings-state">无对应内容</div>
                </div>
            </div>

            <div class="settings-section">
                <div class="settings-section-head">
                    <div class="settings-section-title">Storages（存储）<span class="settings-section-count">共 0 个</span></div>
                </div>
                <div class="mini-grid">
                    <div class="settings-state">无对应内容</div>
                </div>
            </div>

            <div class="settings-section">
                <div class="settings-section-head">
                    <div class="settings-section-title">Knowledge（知识库）<span class="settings-section-count">共 0 个</span></div>
                </div>
                <div class="mini-grid">
                    <div class="settings-state">无对应内容</div>
                </div>
            </div>
        `;
    },

    /* ========== 系统设置：Agents 数据加载（后端优先） ========== */

    /* 渲染智能体卡片区（含加载 / 空状态） */
    _renderAgentCards() {
        if (this.agents === null) {
            return '<div class="settings-state">正在加载智能体列表…</div>';
        }
        if (this.agents.length === 0) {
            return '<div class="settings-state">暂无智能体</div>';
        }
        return this.agents.map(a => this._agentCardHtml(a)).join('');
    },

    /* 单个智能体卡片 HTML（对后端字段做 HTML 转义，防 XSS） */
    _agentCardHtml(a) {
        return `
            <div class="agent-card" data-agent="${escapeHtml(a.id)}">
                <span class="card-check">✓</span>
                <div class="card-icon">${escapeHtml(a.icon)}</div>
                <div class="card-name">${escapeHtml(a.name)}${a.default ? '<span class="card-badge">默认</span>' : ''}</div>
                <div class="card-desc">${escapeHtml(a.desc)}</div>
                <div class="card-meta">模型：${escapeHtml(a.model)}</div>
                <div class="card-status">
                    <span class="card-status-dot ${a.status === '运行中' ? 'on' : 'off'}"></span>${escapeHtml(a.status)}
                </div>
            </div>
        `;
    },

    /* 渲染技能卡片区（含加载 / 空状态；数据来自后端 GetSkills，按当前登录用户 owner 过滤） */
    _renderSkillCards() {
        if (this.skills === null) {
            return '<div class="settings-state">正在加载技能列表…</div>';
        }
        if (this.skills.length === 0) {
            return '<div class="settings-state">暂无技能</div>';
        }
        return this.skills.map(s => this._skillCardHtml(s)).join('');
    },

    /* 单个技能卡片 HTML（对后端字段做 HTML 转义，防 XSS；结构与 Mock 示例卡片保持一致） */
    _skillCardHtml(s) {
        return `
            <div class="skill-card" data-skill="${escapeHtml(s.id)}" data-skill-name="${escapeHtml(s.name.toLowerCase())}" data-skill-status="${s.enabled ? 'enabled' : 'disabled'}">
                <span class="card-check">✓</span>
                <div class="card-icon">${escapeHtml(s.icon)}</div>
                <div class="card-name">${escapeHtml(s.name)}</div>
                <div class="card-desc">${escapeHtml(s.desc)}</div>
                <div class="card-status">
                    <span class="card-status-dot ${s.enabled ? 'on' : 'off'}"></span>${escapeHtml(s.status)}
                </div>
            </div>
        `;
    },

    /**
     * 加载当前登录用户的智能体列表（后端 GET /api/agents）。
     * - 以当前登录用户 token 调后端，后端据此识别 owner，前端无需传用户标识。
     * - Mock 模式直接使用本地示例数据；后端异常时降级示例数据保证页面可用。
     * - 结果缓存在 this.agents，仅首次进入设置页触发，避免重复请求。
     */
    async _loadSettingsAgents() {
        if (this.agents !== null || this.agentsLoading) return;
        this.agentsLoading = true;
        try {
            if (CONFIG.ENABLE_MOCK) {
                this.agents = AGENTS_FALLBACK;
            } else {
                try {
                    const list = await API.getAgents(State.user?.token);
                    // 后端返回空列表时展示空状态（不混入示例数据）
                    this.agents = (Array.isArray(list) ? list : []).map(normalizeAgent).filter(Boolean);
                } catch (error) {
                    console.warn('[Agents] 获取智能体列表失败，使用本地示例数据：', error);
                    this.agents = AGENTS_FALLBACK;
                }
            }
        } finally {
            this.agentsLoading = false;
            this._syncAgentEmoji(); // 加载完成后同步默认 Agent 表情到 State
        }
        this._refreshAgentsSection();
    },

    /**
     * 依据当前智能体列表,将"默认 Agent 的表情"同步到 State.agentEmoji,
     * 供聊天 UI(如思考过程图标)展示与 Agent 对应的表情。
     * 优先取"默认且运行中"的 Agent;无默认时取第一个运行中;列表为空时保持原值。
     */
    _syncAgentEmoji() {
        const list = Array.isArray(this.agents) ? this.agents : [];
        const running = list.filter(a => a.status !== '已暂停');
        const def = running.find(a => a.default) || running[0] || list[0];
        if (def && def.icon) {
            State.agentEmoji = def.icon;
        }
    },

    /* 智能体加载完成后，原地刷新已渲染的设置页区块（未在设置页则跳过） */
    _refreshAgentsSection() {
        const grid = this.els.contentView?.querySelector('#agentGrid');
        const count = this.els.contentView?.querySelector('#agentCount');
        if (grid) {
            grid.innerHTML = this._renderAgentCards();
            if (count) count.textContent = `共 ${this.agents.length} 个`;
        }
    },

    /* 技能加载完成后，原地刷新已渲染的设置页区块（未在设置页则跳过），并保留筛选/搜索状态 */
    _refreshSkillsSection() {
        const grid = this.els.contentView?.querySelector('#skillGrid');
        const count = this.els.contentView?.querySelector('#skillCount');
        if (grid) {
            grid.innerHTML = this._renderSkillCards();
            if (count) count.textContent = `共 ${this.skills.length} 个`;
            // 重渲染后重新应用筛选/搜索（若用户已设置）
            this._applySkillFilters();
        }
    },

    /**
     * 加载当前登录用户的技能列表（后端 GET /api/skills，即 GetSkills）。
     * - 以当前登录用户 token 调后端，后端据此识别 owner（comCode），前端无需传用户标识。
     * - 与 _loadSettingsAgents 策略一致：Mock 用本地示例数据、后端异常时降级、结果缓存在 this.skills。
     * - 技能为「公司级」数据（非按 agent），故全局缓存一次，agent 详情页工作空间页签复用，避免重复请求。
     * @returns {Promise<Array<object>>} 归一化后的技能列表
     */
    async _loadSettingsSkills() {
        if (this.skills !== null) return this.skills;
        // 已有进行中的加载则复用其 Promise，避免重复请求
        if (this._skillsLoadPromise) return this._skillsLoadPromise;
        this._skillsLoadPromise = (async () => {
            try {
                if (CONFIG.ENABLE_MOCK) {
                    this.skills = SKILLS.map(normalizeSkill);
                } else {
                    try {
                        const list = await API.getSkills(State.user?.token);
                        this.skills = (Array.isArray(list) ? list : []).map(normalizeSkill).filter(Boolean);
                    } catch (error) {
                        console.warn('[Skills] 获取技能列表失败，使用本地示例数据：', error);
                        this.skills = SKILLS.map(normalizeSkill);
                    }
                }
            } finally {
                this._skillsLoadPromise = null;
            }
            // 数据就绪后原地刷新已渲染的设置页技能区块（未在设置页则跳过）
            this._refreshSkillsSection();
            return this.skills;
        })();
        return this._skillsLoadPromise;
    },

    /* ========== 系统设置：智能体详情二级页 ========== */

    /* 打开智能体详情（替换当前内容区，可从详情页返回系统设置） */
    openAgentDetail(agentId) {
        const agent = (this.agents || []).find(a => String(a.id) === String(agentId));
        if (!agent) return;
        this.currentAgentDetail = agent;
        this.currentAgentTab = 'basic'; // 每次进入详情默认展示第一个页签
        // 面包屑：详情页内「首页」回我的项目、当前页签回系统设置
        this._syncBreadcrumb();
        this.els.contentView.innerHTML = this.renderAgentDetail(agent);
    },

    /* 关闭详情页：返回系统设置（列表已缓存，直接重渲染，无需重新请求） */
    closeAgentDetail() {
        this.currentAgentDetail = null;
        this.selectMenu('settings');
    },

    /* 渲染智能体详情页：顶部概览 + 可编辑分组表单（除 owner_id 外字段均可改） */
    renderAgentDetail(agent) {
        const raw = (agent && typeof agent.raw === 'object') ? agent.raw : this._agentCardAsRaw(agent);
        // 形象表情用该 agent 实际展示的 emoji：raw 为空时回退到卡片默认（避免字段留空）
        const formRaw = { ...raw, ImageEmoji: String(raw.ImageEmoji || agent.icon) };

        return `
            <div class="agent-detail">
                <!-- 操作状态提示（顶部醒目横幅，成功自动消失） -->
                <div class="agent-form-status" id="agentFormStatus"></div>

                <!-- 合并后的概览 banner：名称 + 描述(版本号徽标) + 返回/保存 按钮 -->
                <div class="agent-detail-head">
                    <span class="agent-detail-icon">${escapeHtml(agent.icon)}</span>
                    <div class="agent-detail-brief">
                        <div class="agent-detail-name">
                            ${escapeHtml(agent.name)}${agent.default ? '<span class="card-badge">默认</span>' : ''}
                            <span class="agent-status-dot ${agent.status === '运行中' ? 'on' : 'off'}" title="${escapeHtml(agent.status)}"></span>
                        </div>
                        <div class="agent-detail-desc">
                            ${escapeHtml(agent.desc)}
                            <span class="agent-detail-version" title="版本号">版本 ${escapeHtml(this._agentFieldText(raw.dVersion))}</span>
                        </div>
                    </div>
                    <div class="agent-detail-actions">
                        <button type="button" class="btn-modal-cancel" id="btnBackToHomeFromDetail" title="返回我的项目">← 返回我的项目</button>
                        <button type="button" class="btn-modal-cancel" id="btnBackToSettings">← 返回系统设置</button>
                        <button type="button" class="btn-primary-add" id="btnSaveAgent">保存修改</button>
                    </div>
                </div>

                <div class="agent-form-card">
                    <div class="agent-tabs" role="tablist">
                        ${AGENT_TABS.map(t => `
                            <button type="button" class="agent-tab ${t.id === 'basic' ? 'active' : ''}" role="tab" data-agent-tab="${t.id}">${t.label}</button>
                        `).join('')}
                    </div>
                    ${AGENT_TABS.map(t => `
                        <div class="agent-tab-panel" role="tabpanel" data-agent-panel="${t.id}" ${t.id === 'basic' ? '' : 'hidden'}>
                            ${t.id === 'workspace'
                                ? this._renderWorkspaceFiles(formRaw.AgentId)
                                : this._agentFormSections(formRaw, t.id)}
                        </div>
                    `).join('')}
                </div>
            </div>
        `;
    },

    /* 分组生成表单区块（AGENT_FORM_FIELDS 按 tab 过滤、再按 section 聚合） */
    _agentFormSections(raw, tabId) {
        const groups = [];
        AGENT_FORM_FIELDS
            .filter(field => field.tab === tabId)
            .forEach(field => {
                const group = groups.find(g => g.title === field.section);
                const item = this._agentFormItemHtml(field, raw);
                if (group) group.items.push(item);
                else groups.push({ title: field.section, items: [item] });
            });
        return groups.map(group => `
            <div class="agent-form-section">
                <div class="agent-form-section-title">${group.title}</div>
                <div class="agent-form-grid">
                    ${group.items.join('')}
                </div>
            </div>
        `).join('');
    },

    /* 工作空间页签：启动文件 + 技能 可收缩区块（数据经后端 API 异步加载，仅元数据不含正文） */
    _renderWorkspaceFiles(agentId) {
        return `
            <div class="agent-form-section">
                ${this._agentSectionHead('启动文件', 'startupFilesPanel')}
                ${agentId
                    ? `<div class="startup-files-panel" id="startupFilesPanel" data-agent-id="${escapeHtml(agentId)}">
                        <div class="startup-files-loading">加载中…</div>
                    </div>`
                    : '<div class="startup-files-error">暂无可用的智能体，无法加载启动文件</div>'}
            </div>
            <div class="agent-form-section">
                ${this._agentSectionHead('技能', 'skillsPanel')}
                ${this._renderSkillsPanel(agentId)}
            </div>`;
    },

    /* 可收缩区块头：标题 + 右侧展开/收缩图标按钮（bodyId 为被折叠的内容区元素 id） */
    _agentSectionHead(title, bodyId) {
        return `
            <div class="agent-form-section-title">
                ${title}
                <button type="button" class="agent-section-toggle"
                        data-agent-collapse="${bodyId}"
                        aria-expanded="true" title="收缩">
                    <svg class="agent-section-toggle-icon" viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" focusable="false">
                        <path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                    </svg>
                </button>
            </div>`;
    },

    /* 工作空间页签：技能列表面板（数据经后端 GetSkills 异步加载，仅元数据不含正文） */
    _renderSkillsPanel(agentId) {
        return agentId
            ? `<div class="skill-files-panel" id="skillsPanel">
                <div class="skill-files-loading">加载中…</div>
            </div>`
            : '<div class="skill-files-error">暂无可用的智能体，无法加载技能列表</div>';
    },

    /* 渲染启动文件清单（文件名 + 必需/可选 + 缺失/大小；值经 HTML 转义防 XSS） */
    _renderStartupFilesList(list) {
        if (!Array.isArray(list) || list.length === 0) {
            return '<div class="startup-files-empty">暂无启动文件</div>';
        }
        return `
            <div class="startup-files-list">
                ${list.map(doc => {
                    const meta = doc.required
                        ? '<span class="tag-required">必需</span>'
                        : '<span class="tag-optional">可选</span>';
                    const extra = doc.missing
                        ? '<span class="tag-missing">缺失</span>'
                        : `<span class="tag-size">${formatBytes(doc.byteSize)}</span>`;
                    const title = doc.missing ? '文件缺失' : `查看 ${doc.fileName}`;
                    return `
                        <button type="button" class="startup-file-item${doc.missing ? ' missing' : ''}"
                                data-startup-file="${escapeHtml(doc.fileName)}"
                                title="${escapeHtml(title)}">
                            <span class="startup-file-item-name">${escapeHtml(doc.fileName)}</span>
                            <span class="startup-file-item-meta">${meta}${extra}</span>
                        </button>`;
                }).join('')}
            </div>`;
    },

    /* 加载当前智能体启动文件清单：有缓存直接渲染，否则请求一次并缓存（避免重复请求） */
    async loadStartupFiles() {
        const panel = this.els.contentView?.querySelector('#startupFilesPanel');
        const agentId = panel?.dataset.agentId;
        if (!panel || !agentId) return;

        if (this.startupFilesCache && this.startupFilesCache.agentId === agentId) {
            panel.innerHTML = this._renderStartupFilesList(this.startupFilesCache.list);
            return;
        }

        panel.innerHTML = '<div class="startup-files-loading">加载中…</div>';
        try {
            const list = await API.getStartupFiles(State.user?.token, agentId);
            this.startupFilesCache = { agentId, list };
            panel.innerHTML = this._renderStartupFilesList(list);
        } catch (error) {
            panel.innerHTML = `<div class="startup-files-error">${escapeHtml(error.message || '加载启动文件失败')}</div>`;
        }
    },

    /* 点击启动文件：加载并展示其内容（仅点击时请求正文，列表本身不含正文，节省带宽） */
    async openStartupFile(fileName) {
        const panel = this.els.contentView?.querySelector('#startupFilesPanel');
        if (!panel || !fileName) return;
        const agentId = panel.dataset.agentId;

        // 高亮选中项
        panel.querySelectorAll('[data-startup-file]').forEach(el => {
            el.classList.toggle('active', el.dataset.startupFile === fileName);
        });

        // 复用 / 创建内容预览区（位于列表下方）
        let viewer = panel.querySelector('#startupFileViewer');
        if (!viewer) {
            viewer = document.createElement('div');
            viewer.id = 'startupFileViewer';
            viewer.className = 'startup-file-viewer';
            panel.appendChild(viewer);
        }
        viewer.innerHTML = '<div class="startup-files-loading">加载中…</div>';

        try {
            const file = await API.getStartupFile(State.user?.token, agentId, fileName);
            viewer.innerHTML = this._renderStartupFileContent(file);
        } catch (error) {
            viewer.innerHTML = `<div class="startup-files-error">${escapeHtml(error.message || '加载文件内容失败')}</div>`;
        }
    },

    /* 渲染单个启动文件内容（正文经 HTML 转义放入只读文本框，防 XSS） */
    _renderStartupFileContent(file) {
        if (!file) return '<div class="startup-files-error">文件不存在</div>';
        const missing = file.missing;
        return `
            <div class="startup-file-viewer-head">
                <span class="startup-file-viewer-name">${escapeHtml(file.fileName)}</span>
                <span class="startup-file-viewer-meta">
                    ${file.required ? '必需' : '可选'}
                    ${missing ? ' · 缺失' : ` · ${formatBytes(file.byteSize)}`}
                </span>
            </div>
            ${missing
                ? '<div class="startup-files-error">该文件尚未创建</div>'
                : `<textarea class="startup-file-viewer-body" readonly spellcheck="false">${escapeHtml(file.content)}</textarea>`}
        `;
    },

    /* 渲染技能清单（技能名 + 版本/作用域/启用状态；值经 HTML 转义防 XSS） */
    _renderSkillsList(list) {
        if (!Array.isArray(list) || list.length === 0) {
            return '<div class="skill-files-empty">暂无技能</div>';
        }
        return `
            <div class="skill-files-list">
                ${list.map(s => `
                    <div class="skill-file-item" title="${escapeHtml(s.name)}${s.dir ? `（${escapeHtml(s.dir)}）` : ''}">
                        <span class="skill-file-item-name">${escapeHtml(s.name)}</span>
                        <span class="skill-file-item-meta">
                            <span class="tag-version">版本 ${escapeHtml(s.version)}</span>
                            <span class="tag-optional">${escapeHtml(s.scopeLabel)}</span>
                            <span class="${s.enabled ? 'tag-required' : 'tag-missing'}">${s.enabled ? '已启用' : '未启用'}</span>
                        </span>
                    </div>`).join('')}
            </div>`;
    },

    /* 加载技能清单：复用全局技能缓存 this.skills（公司级，非按 agent），避免重复请求 */
    async loadSkills() {
        const panel = this.els.contentView?.querySelector('#skillsPanel');
        if (!panel) return;

        if (this.skills === null) {
            panel.innerHTML = '<div class="skill-files-loading">加载中…</div>';
            await this._loadSettingsSkills();
        }
        panel.innerHTML = this._renderSkillsList(this.skills || []);
    },

    /* 切换工作空间页签下文件区块的展开/收缩（启动文件 / 技能） */
    toggleAgentSection(bodyId, btn) {
        const body = this.els.contentView?.querySelector(`#${bodyId}`);
        if (!body || !btn) return;
        const collapsed = !body.hidden; // 当前展开 → 收缩
        body.hidden = collapsed;
        btn.classList.toggle('collapsed', collapsed);
        btn.setAttribute('aria-expanded', String(!collapsed));
        btn.title = collapsed ? '展开' : '收缩';
    },

    /* 切换详情页签（基本信息 / 工作空间） */
    switchAgentTab(tabId) {
        this.currentAgentTab = tabId;
        this.els.contentView.querySelectorAll('.agent-tab').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.agentTab === tabId);
        });
        this.els.contentView.querySelectorAll('.agent-tab-panel').forEach(panel => {
            panel.hidden = panel.dataset.agentPanel !== tabId;
        });
        // 工作空间页签：懒加载启动文件 / 技能清单（有缓存则直接渲染，不重复请求）
        if (tabId === 'workspace') {
            this.loadStartupFiles();
            this.loadSkills();
        }
    },

    /* 单个表单项 HTML（text / number / select / switch / emoji，值经 HTML 转义防 XSS） */
    _agentFormItemHtml(field, raw) {
        const value = raw?.[field.key];
        const id = 'f_' + field.key;
        const required = field.required ? ' <span class="req">*</span>' : '';

        let control;
        if (field.type === 'switch') {
            control = `
                <label class="switch">
                    <input type="checkbox" id="${id}" data-key="${field.key}" ${toBool(value) ? 'checked' : ''} />
                    <span class="switch-slider"></span>
                </label>`;
        } else if (field.type === 'emoji') {
            // 高亮该 agent 原本的形象表情（当前输入框的值）
            const currentEmoji = String(value ?? '');
            const emojiItems = EMOJI_PICKER_OPTIONS.map((e, i) =>
                `<button type="button" class="agent-emoji-item${currentEmoji === e ? ' selected' : ''}" data-emoji-item="${i}" title="${e}">${e}</button>`
            ).join('');
            control = `
                <div class="agent-emoji-control">
                    <input type="text" id="${id}" class="setting-input agent-form-input" data-key="${field.key}" value="${escapeHtml(String(value ?? ''))}" maxlength="32" placeholder="点击 ⋯ 选择表情" />
                    <button type="button" class="btn-emoji-picker" data-emoji-picker="${field.key}" title="选择表情">⋯</button>
                    <div class="agent-emoji-popup" data-emoji-popup="${field.key}" hidden>
                        ${emojiItems}
                    </div>
                </div>`;
        } else if (field.type === 'select') {
            const options = (field.options || []).map(o =>
                `<option value="${Number(o.value)}"${Number(value) === Number(o.value) ? ' selected' : ''}>${escapeHtml(o.label)}</option>`
            ).join('');
            control = `<select id="${id}" class="setting-input agent-form-input" data-key="${field.key}">${options}</select>`;
        } else if (field.type === 'number') {
            const attrs = [
                field.min !== undefined ? `min="${field.min}"` : '',
                field.max !== undefined ? `max="${field.max}"` : '',
                field.step ? `step="${field.step}"` : '',
            ].filter(Boolean).join(' ');
            control = `<input type="number" id="${id}" class="setting-input agent-form-input" data-key="${field.key}" value="${Number(value) || 0}" ${attrs} />`;
        } else {
            control = `<input type="text" id="${id}" class="setting-input agent-form-input" data-key="${field.key}" value="${escapeHtml(String(value ?? ''))}" maxlength="${field.maxlength || ''}" />`;
        }

        return `
            <div class="agent-form-item ${field.type === 'switch' ? 'agent-form-item-switch' : ''}">
                <label class="agent-form-label" for="${id}">${field.label}${required}</label>
                ${control}
            </div>`;
    },

    /* 收集表单值：select/number 转数字、switch 转布尔、文本去首尾空格 */
    _collectAgentForm() {
        const values = {};
        this.els.contentView.querySelectorAll('[data-key]').forEach(el => {
            const key = el.dataset.key;
            if (el.type === 'checkbox') values[key] = el.checked;
            else if (el.type === 'select-one' || el.type === 'number') values[key] = Number(el.value) || 0;
            else values[key] = el.value.trim();
        });
        return values;
    },

    /* 保存修改：校验必填 → 调后端更新接口 → 成功后更新本地缓存并刷新详情页 */
    async submitAgentEdit() {
        const raw = this.currentAgentDetail?.raw;
        if (!raw) return;

        const values = this._collectAgentForm();
        for (const [key, label] of Object.entries(AGENT_FORM_REQUIRED)) {
            if (!String(values[key] ?? '').trim()) {
                this._setAgentFormStatus('error', `「${label}」不能为空`);
                // 若必填字段位于未激活页签，自动切到该页签方便定位
                const field = AGENT_FORM_FIELDS.find(f => f.key === key);
                if (field && field.tab !== this.currentAgentTab) this.switchAgentTab(field.tab);
                return;
            }
        }

        // 数值范围校验（min / max）
        for (const field of AGENT_FORM_FIELDS) {
            if (field.type !== 'number' || (field.min === undefined && field.max === undefined)) continue;
            const num = Number(values[field.key]);
            if (!Number.isFinite(num)) continue;
            const tooLow = field.min !== undefined && num < field.min;
            const tooHigh = field.max !== undefined && num > field.max;
            if (!tooLow && !tooHigh) continue;
            const range = field.min !== undefined && field.max !== undefined
                ? `须在 ${field.min}~${field.max} 之间`
                : field.min !== undefined
                    ? `不能小于 ${field.min}`
                    : `不能大于 ${field.max}`;
            this._setAgentFormStatus('error', `「${field.label}」${range}`);
            if (field.tab !== this.currentAgentTab) this.switchAgentTab(field.tab);
            return;
        }

        // 乐观锁：提交「当前服务端版本号」（后端校验相等后自增），不能 +1
        const baseVersion = Number(raw.dVersion || 0);
        const updatedRaw = { ...raw, ...values, dVersion: baseVersion };
        this._setAgentFormStatus('saving', '保存中…');
        try {
            await API.updateAgent(State.user?.token, updatedRaw);
        } catch (error) {
            this._setAgentFormStatus('error', error.message || '保存失败，请重试');
            return;
        }

        // 保存成功后服务端版本号 +1，本地同步，供下次编辑使用
        const card = normalizeAgent({ ...updatedRaw, dVersion: baseVersion + 1 });
        const idx = (this.agents || []).findIndex(a => String(a.id) === String(card.id));
        if (idx >= 0) this.agents[idx] = card;
        this.currentAgentDetail = card;
        this.els.contentView.innerHTML = this.renderAgentDetail(card);
        // 保持当前所在页签，再提示成功
        this.switchAgentTab(this.currentAgentTab);
        this._setAgentFormStatus('success', '保存成功');
    },

    /* ========== 系统设置：形象表情选择 ========== */

    /* 切换表情选择弹层（打开/关闭） */
    toggleEmojiPicker(fieldKey) {
        const popup = this.els.contentView?.querySelector(`[data-emoji-popup="${fieldKey}"]`);
        if (!popup) return;
        const willOpen = popup.hidden;
        this._closeEmojiPopups();
        if (willOpen) popup.hidden = false;
    },

    /* 选中表情：写入对应输入框并关闭弹层 */
    pickEmoji(index) {
        const emoji = EMOJI_PICKER_OPTIONS[Number(index)];
        if (!emoji) return;
        const popup = this.els.contentView?.querySelector('[data-emoji-popup]:not([hidden])');
        if (!popup) return;
        const input = popup.closest('.agent-emoji-control')?.querySelector('input[data-key]');
        if (input) input.value = emoji;
        popup.hidden = true;
    },

    /* 关闭所有表情弹层 */
    _closeEmojiPopups() {
        this.els.contentView?.querySelectorAll('[data-emoji-popup]:not([hidden])').forEach(p => {
            p.hidden = true;
        });
    },

    /* 表单操作状态提示（顶部醒目横幅：success/error/saving；成功自动消失） */
    _setAgentFormStatus(type, msg) {
        const el = this.els.contentView?.querySelector('#agentFormStatus');
        if (!el) return;
        const icon = type === 'success' ? '✓' : type === 'error' ? '⚠' : '…';
        el.textContent = (icon ? `${icon} ` : '') + msg;
        el.className = 'agent-form-status' + (type ? ` ${type}` : '');
        clearTimeout(this._agentStatusTimer);
        if (type === 'success') {
            this._agentStatusTimer = setTimeout(() => {
                el.textContent = '';
                el.className = 'agent-form-status';
            }, 2500);
        }
    },

    /* 详情字段值格式化：空值占位、布尔转中文 */
    _agentFieldText(value) {
        if (value === null || value === undefined || value === '') return '—';
        if (typeof value === 'boolean') return value ? '是' : '否';
        return String(value);
    },

    /* 无后端原始对象时（如本地示例数据）由卡片模型派生展示字段 */
    _agentCardAsRaw(agent) {
        return {
            AgentId: agent.id,
            AgentName: agent.name,
            Description: agent.desc,
            PrimaryModel: agent.model,
            ImageEmoji: agent.icon,
            IsDefault: agent.default,
            Disabled: agent.status === '已暂停',
        };
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
        // 用户名变化后重渲染当前菜单内容
        if (this.els.contentView) {
            this.selectMenu(this.currentMenu);
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
        this.npaProjects = [];
        this.pendingDeleteId = null;
        this.currentHousehold = null;
        this.currentFileText = '';
        // 清空智能体缓存：切换登录用户后按新用户重新拉取（避免跨用户数据残留）
        this.agents = null;
        this.agentsLoading = false;
        this.currentAgentDetail = null;
        this.startupFilesCache = null; // 切换登录用户后按新用户重新拉取（避免跨用户残留）
        this.skills = null;            // 技能列表（公司级）切换用户后重新拉取（避免跨用户残留）
        this._skillsLoadPromise = null;
        this.menuHistory = [];         // 清空页面来源栈，避免跨用户残留
        this.updateUserInfo('');
        this.selectMenu(DEFAULT_MENU);
    },
};

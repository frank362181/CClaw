/**
 * ============================================================
 * 工具函数
 * ============================================================
 */

/** 生成短 ID */
export function genId() {
    if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
    return Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 11);
}

/* 会话业务主键长度上限（与后端 CompactIdLen=40、DDL VARCHAR(40) 对齐） */
const SESSION_ID_MAX_LEN = 40;

/**
 * 规整会话 ID（对齐后端 cclawservice/common.NormalizeCompactId）：
 * 去空白、去连字符、仅保留字母/数字/下划线。
 */
export function normalizeSessionId(id) {
    return String(id ?? '').trim().replace(/-/g, '').replace(/[^0-9a-zA-Z_]/g, '').slice(0, SESSION_ID_MAX_LEN);
}

/**
 * 生成与后端落库格式一致的会话 ID（"单位编码 + UUID 去连字符"）。
 * 后端会把客户端会话 ID 规整为 comCode 前缀形式再落库，若前端直接用裸 UUID，
 * 本地会话 ID 与 GetSessions 返回的 SessionId 将永远不相等（高亮失效、重复建会话）。
 * @param {string} comCode 单位编码（登录返回）
 */
export function newSessionId(comCode) {
    const prefix = normalizeSessionId(comCode);
    const body = normalizeSessionId(genId());
    return (prefix + body).slice(0, SESSION_ID_MAX_LEN);
}

/** 格式化时间 (时:分) */
export function formatTime(ts) {
    const d = new Date(ts);
    const h = String(d.getHours()).padStart(2, '0');
    const m = String(d.getMinutes()).padStart(2, '0');
    return h + ':' + m;
}

/** 格式化日期（用于列表） */
export function formatDate(ts) {
    const d = new Date(ts);
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const target = new Date(d.getFullYear(), d.getMonth(), d.getDate());
    const diffDays = Math.floor((today - target) / (1000 * 60 * 60 * 24));
    if (diffDays === 0) return '今天';
    if (diffDays === 1) return '昨天';
    if (diffDays < 7) return diffDays + '天前';
    return d.getMonth() + 1 + '/' + d.getDate();
}

/** 格式化字节数（用于文件大小展示） */
export function formatBytes(bytes) {
    const n = Number(bytes) || 0;
    if (n < 1024) return n + ' B';
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
    return (n / (1024 * 1024)).toFixed(1) + ' MB';
}

/** 截断文本 */
export function truncate(text, maxLen = 28) {
    if (!text) return '新对话';
    return text.length > maxLen ? text.slice(0, maxLen) + '…' : text;
}

/** 防抖 */
export function debounce(fn, delay = 300) {
    let timer;
    return (...args) => {
        clearTimeout(timer);
        timer = setTimeout(() => fn(...args), delay);
    };
}

/** 安全解析 JSON */
export function safeJSON(str, fallback = null) {
    try { return JSON.parse(str); } catch { return fallback; }
}

/** HTML 转义（渲染后端数据时必须使用，防止 XSS 注入） */
export function escapeHtml(value) {
    return String(value ?? '').replace(/[&<>"']/g, (ch) => ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#39;',
    }[ch]));
}

/** 获取用户首字母 */
export function getInitials(name) {
    if (!name) return 'U';
    return name.charAt(0).toUpperCase();
}

export function getRandomNonce() {
    return String(Math.floor(Math.random() * Number.MAX_SAFE_INTEGER) + 1);
} 

/** 随机延迟 (用于流式模拟) */
export function randomDelay(min = 15, max = 45) {
    return Math.floor(Math.random() * (max - min + 1)) + min;
}

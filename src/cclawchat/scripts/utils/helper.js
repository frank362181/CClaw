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

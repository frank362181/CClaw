/**
 * ============================================================
 * 轻量级组件加载器 (Web Components)
 * ============================================================
 * 设计原则：
 *  - 高内聚：每个组件 = 视图 (pages/*.html) + 内部样式（通过 Shadow DOM
 *    隔离），对外只暴露少量公共方法，内部 DOM 结构外部不可见。
 *  - 低耦合：组件之间互不感知彼此实现，只通过公开 API 协作。
 *
 * 用法：
 *   const login = await mountComponent({ tag: 'login-page', url: 'pages/login.html' });
 *   login.query('#username');      // 在组件作用域内查找元素
 */

// 组件全局样式入口（通过 Shadow DOM @import 引入，实现样式封装）
const STYLE_ENTRY = './styles/main.css';

const registry = new Map();

/**
 * 挂载一个页面组件
 * @param {{ tag: string, url: string }} options
 * @returns {Promise<{ el: HTMLElement, root: ShadowRoot, query: Function, queryAll: Function }>}
 */
export async function mountComponent({ tag, url }) {
    // 同一组件只加载一次（组件注册表）
    if (registry.has(tag)) {
        return registry.get(tag);
    }

    const promise = (async () => {
        // 1. 拉取组件视图模板
        const res = await fetch(url);
        if (!res.ok) {
            throw new Error(`组件加载失败 (${res.status}): ${url}`);
        }
        const html = await res.text();

        // 2. 复用 index.html 中已声明的组件宿主；若不存在则新建并挂载到 #app
        let el = document.querySelector(tag);
        if (!el) {
            el = document.createElement(tag);
            document.getElementById('app').appendChild(el);
        }

        // 3. 创建 Shadow DOM，实现组件 DOM 与样式隔离
        const root = el.attachShadow({ mode: 'open' });

        // 4. 将全局样式引入组件内部，保证组件样式自包含、不受外界干扰
        const style = document.createElement('style');
        style.textContent = `@import url("${STYLE_ENTRY}");`;
        root.appendChild(style);

        // 5. 注入组件视图模板
        const tpl = document.createElement('template');
        tpl.innerHTML = html;
        root.appendChild(tpl.content);

        // 6. 返回组件实例（对外暴露的作用域内查询能力）
        return {
            el,
            root,
            query: (selector) => root.querySelector(selector),
            queryAll: (selector) => Array.from(root.querySelectorAll(selector)),
        };
    })();

    registry.set(tag, promise);
    return promise;
}

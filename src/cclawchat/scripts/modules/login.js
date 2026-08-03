/**
 * ============================================================
 * 登录页组件控制器 (LoginPage)
 * ============================================================
 * 仅负责登录页视图的展示、校验与交互，不关心其他页面。
 * 不依赖 ChatPage / App，仅通过 onSubmit 回调与外部协作。
 */

export const LoginPage = {
    comp: null,
    els: {},

    /**
     * 初始化组件
     * @param {object} comp 组件实例（mountComponent 返回）
     * @param {{ onSubmit?: (username: string, password: string) => Promise<boolean> }} handlers
     */
    init(comp, handlers = {}) {
        this.comp = comp;
        this.els = {
            page: comp.query('#loginPage'),
            form: comp.query('#loginForm'),
            error: comp.query('#loginError'),
            btn: comp.query('#btnLogin'),
            username: comp.query('#username'),
            password: comp.query('#password'),
        };

        // 表单提交：校验 -> 回调外部业务 -> 反馈
        this.els.form.addEventListener('submit', async (e) => {
            e.preventDefault();
            this.hideError();

            const username = this.getUsername();
            const password = this.getPassword();
            if (!username || !password) {
                this.showError('请输入用户名和密码');
                return;
            }

            this.setLoading(true);
            const success = await (await handlers.onSubmit?.(username, password) ?? true);
            this.setLoading(false);

            if (!success) {
                this.clearPassword();
                this.focusPassword();
            }
        });
    },

    show() {
        // 宿主也必须参与显示/隐藏；否则登录页虽然隐藏，仍会占满一屏高度，
        // 将后续主页顶到首屏之外。
        this.comp.el.style.display = 'block';
        this.els.page.style.display = 'flex';
    },

    hide() {
        this.els.page.style.display = 'none';
        this.comp.el.style.display = 'none';
    },

    setLoading(loading) {
        this.els.btn.disabled = loading;
        this.els.btn.classList.toggle('loading', loading);
    },

    showError(msg) {
        this.els.error.textContent = '⚠️ ' + (msg || '用户名或密码错误，请重试。');
        this.els.error.classList.add('show');
    },

    hideError() {
        this.els.error.classList.remove('show');
    },

    getUsername() {
        return this.els.username.value.trim();
    },

    getPassword() {
        return this.els.password.value.trim();
    },

    clearPassword() {
        this.els.password.value = '';
    },

    focusUsername() {
        this.els.username.focus();
    },

    focusPassword() {
        this.els.password.focus();
    },
};

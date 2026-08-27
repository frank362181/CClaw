import test from 'node:test';
import assert from 'node:assert/strict';

class MemoryStorage {
    constructor() { this.data = new Map(); }
    getItem(key) { return this.data.has(key) ? this.data.get(key) : null; }
    setItem(key, value) { this.data.set(key, String(value)); }
    removeItem(key) { this.data.delete(key); }
    clear() { this.data.clear(); }
}

globalThis.localStorage = new MemoryStorage();
globalThis.sessionStorage = new MemoryStorage();

const { State } = await import('../scripts/modules/state.js');
const { CONFIG } = await import('../scripts/modules/config.js');

function reset() {
    localStorage.clear();
    sessionStorage.clear();
    State.user = null;
    State.conversations = [];
    State.currentConvId = null;
    State.isStreaming = false;
}

test('conversation data is isolated by username', () => {
    reset();
    State.login('alice', 'token-a');
    State.addMessage(State.currentConvId, 'user', 'Alice private message');
    State.logout();
    State.login('bob', 'token-b');
    assert.equal(State.getCurrentConversation().messages.length, 0);
    State.logout();
    State.login('alice', 'token-a2');
    assert.equal(State.getCurrentConversation().messages[0].content, 'Alice private message');
});

test('session is kept out of localStorage', () => {
    reset();
    State.login('alice', 'secret-token');
    assert.equal(localStorage.getItem(CONFIG.USER_KEY), null);
    assert.match(sessionStorage.getItem(CONFIG.USER_KEY), /secret-token/);
});

test('conversation enforces message limit', () => {
    reset();
    State.login('alice', 'token');
    for (let index = 0; index < CONFIG.MAX_MESSAGES_PER_CONV + 5; index += 1) {
        State.addMessage(State.currentConvId, 'user', String(index), undefined, { persist: false });
    }
    assert.equal(State.getCurrentConversation().messages.length, CONFIG.MAX_MESSAGES_PER_CONV);
});

test('updateMessageStatus stores transient status without persisting to storage', () => {
    reset();
    State.login('alice', 'token');
    const msg = State.addMessage(State.currentConvId, 'assistant', '');
    State.updateMessageStatus(State.currentConvId, msg.id, '正在思考…');
    const found = () => State.getCurrentConversation().messages.find(m => m.id === msg.id);
    assert.equal(found().statusText, '正在思考…');
    State.updateMessageStatus(State.currentConvId, msg.id, '');
    assert.equal(found().statusText, '');
    // 该字段仅存于内存:重新加载会话(从 localStorage)后不残留
    State.loadConversations();
    assert.equal(found().statusText, undefined);
});

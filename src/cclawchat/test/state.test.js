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

test('markMessageInterrupted marks message and persists across reload', () => {
    reset();
    State.login('alice', 'token');
    const msg = State.addMessage(State.currentConvId, 'assistant', '部分内容');
    const found = () => State.getCurrentConversation().messages.find(m => m.id === msg.id);
    // 默认未标记
    assert.equal(found().interrupted, undefined);
    // 标记后生效并持久化:重新加载会话(从 localStorage)后仍保留
    assert.equal(State.markMessageInterrupted(State.currentConvId, msg.id), true);
    assert.equal(found().interrupted, true);
    State.loadConversations();
    assert.equal(found().interrupted, true);
    // 不存在的消息返回 false,不产生副作用
    assert.equal(State.markMessageInterrupted(State.currentConvId, 'no-such-id'), false);
});

test('setCurrentConversation switches active conversation and persists', () => {
    reset();
    State.login('alice', 'token');
    const first = State.currentConvId;
    const second = State.createConversation('第二会话');
    assert.equal(State.currentConvId, second.id);

    assert.equal(State.setCurrentConversation(first), true);
    assert.equal(State.currentConvId, first);
    State.loadConversations();
    assert.equal(State.currentConvId, first);

    assert.equal(State.setCurrentConversation('missing-id'), false);
    assert.equal(State.currentConvId, first);
});

test('deleteConversation removes conversation and falls back to another one', () => {
    reset();
    State.login('alice', 'token');
    const first = State.currentConvId;
    const second = State.createConversation('第二会话');

    // 删除非当前会话：当前会话保持不变
    assert.equal(State.deleteConversation(first), true);
    assert.equal(State.conversations.length, 1);
    assert.equal(State.currentConvId, second.id);

    // 删除当前会话且无其它会话：自动新建一条默认会话
    assert.equal(State.deleteConversation(second.id), true);
    assert.equal(State.conversations.length, 1);
    assert.notEqual(State.currentConvId, second.id);
    assert.ok(State.getCurrentConversation());

    // 不存在的会话返回 false
    assert.equal(State.deleteConversation('no-such-id'), false);
});

test('new conversations use server-compatible session ids', async () => {
    const { newSessionId, normalizeSessionId } = await import('../scripts/utils/helper.js');
    reset();
    State.login('alice', 'token', { comCode: '100001' });
    State.conversations = [];
    const conv = State.createConversation('新会话');
    assert.match(conv.id, /^100001[0-9a-f]{32}$/);
    assert.equal(conv.id.length, 38);
    // 与后端规则一致：再去一次前缀不应改变
    assert.equal(normalizeSessionId(conv.id), conv.id);
    assert.equal(newSessionId('100001').length <= 40, true);
});

test('ensureConversation adopts legacy uuid conversation for a server session id', () => {
    reset();
    State.login('alice', 'token', { comCode: '100001' });
    State.conversations = [];
    const legacy = { id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890', title: '旧会话', messages: [], createdAt: 1, updatedAt: 1 };
    State.conversations = [legacy];
    State.currentConvId = legacy.id;

    const serverId = '100001' + 'a1b2c3d4e5f67890abcdef1234567890';
    const adopted = State.ensureConversation(serverId, '旧会话');
    assert.equal(adopted.id, serverId);
    assert.equal(State.conversations.length, 1);
    assert.equal(State.currentConvId, serverId);
});

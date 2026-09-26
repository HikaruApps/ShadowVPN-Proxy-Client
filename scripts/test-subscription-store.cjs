const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function memoryStorage(initial = {}) {
  const values = new Map(Object.entries(initial));
  return {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: key => values.delete(key),
  };
}

const window = {};
vm.runInNewContext(fs.readFileSync('src/renderer/subscription-store.js', 'utf8'), { window, URL, localStorage: memoryStorage(), Date, Math, JSON });
const store = window.shadowVpnSubscriptions;
assert.equal(store.validURL('https://example.com/sub'), true);
assert.equal(store.validURL('http://example.com/sub'), false);
assert.equal(store.validURL('https://user@example.com/sub'), false);
assert.deepEqual(JSON.parse(JSON.stringify(store.cleanMetadata({ title: ' ShadowVPN\n', supportUrl: 'https://t.me/support', dnsDoh: 'dns.example' }))), {
  title: 'ShadowVPN', supportUrl: 'https://t.me/support', dnsDoh: 'https://dns.example/dns-query',
});

const legacy = memoryStorage({ [store.legacyStorageKey]: 'https://legacy.example/sub' });
let state = store.readState(legacy);
assert.equal(state.items.length, 1);
assert.equal(state.items[0].url, 'https://legacy.example/sub');
assert.equal(state.activeId, '');

const previousVersion = memoryStorage({
  [store.storageKey]: JSON.stringify({
    items: [{ id: 'sub-existing-1234', url: 'https://existing.example/sub' }],
    selectedId: 'sub-existing-1234',
  }),
});
assert.equal(store.readState(previousVersion).activeId, '');

let result = store.addItem(state, 'https://second.example/sub');
assert.equal(result.error, '');
state = result.state;
assert.equal(state.items.length, 2);
assert.equal(store.activeItems(state).length, 2);
state.activeId = state.items[1].id;
assert.deepEqual([...store.activeItems(state)].map(item => item.id), [state.items[1].id]);
state = store.updateMetadata(state, state.items[1].id, { title: 'Основная', supportUrl: 'javascript:alert(1)', dnsDoh: 'http://dns.example' });
assert.equal(state.items[1].title, 'Основная');
assert.equal(state.items[1].supportUrl, '');
assert.equal(state.items[1].dnsDoh, '');
assert.equal(store.addItem(state, 'https://second.example/sub').error, 'Эта подписка уже добавлена');
result = store.updateItem(state, state.items[1].id, 'https://updated.example/sub');
assert.equal(result.error, '');
state = result.state;
assert.equal(state.items[1].url, 'https://updated.example/sub');
state = store.removeItem(state, state.items[0].id);
assert.equal(state.items.length, 1);
assert.equal(state.selectedId, state.items[0].id);
assert.equal(state.activeId, state.items[0].id);

state = store.removeItem(state, state.items[0].id);
assert.equal(state.activeId, '');

result = store.addItem(state, 'https://updated.example/sub');
state = result.state;

const storage = memoryStorage();
state = store.writeState(state, storage);
assert.equal(JSON.parse(storage.getItem(store.storageKey)).items.length, 1);
assert.equal(storage.getItem(store.legacyStorageKey), 'https://updated.example/sub');
assert.equal(store.displayHost('https://updated.example/sub'), 'updated.example');
console.log('Subscription store: migration, validation, add, update, remove passed.');

const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const listeners = new Map(), calls = [];
const elements = { statusText: {}, subscriptionError: {} };
const window = { __TAURI__: {
  core: { invoke: async (name, args) => { calls.push([name, args]); if (name === 'vpn_import') throw 'Import failed'; return name === 'vpn_get_state' ? 'connected' : {}; } },
  event: { listen: async (name, fn) => { listeners.set(name, fn); return () => {}; } },
} };
vm.runInNewContext(fs.readFileSync('src/renderer/tauri-bridge.js', 'utf8'), { window, document: { getElementById: id => elements[id] } });
(async () => {
  let state; const off = window.vpnApi.onStateChange(value => { state = value; });
  assert.equal(await window.vpnApi.getState(), 'connected');
  listeners.get('vpn:state')({ payload: 'disconnecting' }); assert.equal(state, 'disconnecting');
  off(); listeners.get('vpn:state')({ payload: 'disconnected' }); assert.equal(state, 'disconnecting');
  assert.equal((await window.vpnApi.connect('abcdef')).ok, true);
  assert.equal(calls.at(-1)[0], 'vpn_connect'); assert.equal(calls.at(-1)[1].profileId, 'abcdef');
  assert.equal(calls.at(-1)[1].dns, 'cloudflare');
  assert.deepEqual([...calls.at(-1)[1].dnsServers], []);
  assert.equal(calls.at(-1)[1].fragmentation, false);
  assert.equal(calls.at(-1)[1].killSwitch, false);
  await window.vpnApi.connect('abcdef', 'quad9'); assert.equal(calls.at(-1)[1].dns, 'quad9');
  await window.vpnApi.connect('abcdef', 'custom', ['192.168.1.1']);
  assert.deepEqual([...calls.at(-1)[1].dnsServers], ['192.168.1.1']);
  await window.vpnApi.connect('abcdef', 'cloudflare', [], true);
  assert.equal(calls.at(-1)[1].fragmentation, true);
  await window.vpnApi.connect('abcdef', 'cloudflare', [], true, true);
  assert.equal(calls.at(-1)[1].killSwitch, true);
  assert.equal((await window.vpnApi.importSubscription('https://example.com')).error, 'Import failed');
  await window.vpnApi.disconnect(); assert.equal(calls.at(-1)[0], 'vpn_disconnect');
  await window.vpnApi.ping(); assert.equal(calls.at(-1)[0], 'vpn_ping');
  await window.vpnApi.publicIp(true); assert.equal(calls.at(-1)[0], 'vpn_public_ip'); assert.equal(calls.at(-1)[1].masked, true);
  await window.vpnApi.getLogs(); assert.equal(calls.at(-1)[0], 'vpn_get_logs');
  await window.vpnApi.clearLogs(); assert.equal(calls.at(-1)[0], 'vpn_clear_logs');
  let logLine; const stopLogs = window.vpnApi.onLog(value => { logLine = value; });
  listeners.get('vpn:log')({ payload: 'Xray diagnostic' }); assert.equal(logLine, 'Xray diagnostic'); stopLogs();
  listeners.get('vpn:error')({payload:'Shutdown pending'}); assert.equal(elements.statusText.textContent, 'Shutdown pending');
  console.log('Tauri bridge: state listener, unsubscribe, commands, errors passed.');
})().catch(e => { console.error(e); process.exit(1); });

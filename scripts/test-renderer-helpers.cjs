const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const window = {};
vm.runInNewContext(fs.readFileSync('src/renderer/server-display.js', 'utf8'), { window, Intl });

const { regionalFlagCode, serverFlagAndName, sortServerProfiles } = window.shadowVpnDisplay;
assert.equal(regionalFlagCode('🇩🇪'), 'de');
assert.equal(regionalFlagCode('🇪🇺'), 'eu');
assert.equal(regionalFlagCode('🏴‍☠️'), '');
assert.deepEqual(
  JSON.parse(JSON.stringify(serverFlagAndName('🇭🇰 Новые блокировки'))),
  { flag: '🇭🇰', flagCode: 'hk', name: 'Новые блокировки' },
);
assert.equal(serverFlagAndName('🏴‍☠️ Torrent Server').name, 'Torrent Server');
assert.equal(serverFlagAndName('Без флага').flagCode, '');
const unsortedProfiles = [{ id: 'slow', name: 'Япония' }, { id: 'auto', auto: true, name: 'Авто' }, { id: 'offline', name: 'Berlin' }, { id: 'fast', name: 'Амстердам' }];
const unchangedProfiles = sortServerProfiles(unsortedProfiles, new Map());
assert.deepEqual(unchangedProfiles.map(profile => profile.id), ['auto', 'slow', 'offline', 'fast']);
const alphabeticalProfiles = sortServerProfiles(unsortedProfiles, new Map(), 'alphabetical');
assert.deepEqual(alphabeticalProfiles.map(profile => profile.id), ['auto', 'fast', 'slow', 'offline']);
const sortedProfiles = sortServerProfiles(unsortedProfiles, new Map([
  ['slow', { available: true, latencyMs: 120 }],
  ['offline', { available: false }],
  ['fast', { available: true, latencyMs: 20 }],
]), 'latency');
assert.deepEqual(sortedProfiles.map(profile => profile.id), ['auto', 'fast', 'slow', 'offline']);
for (const code of ['de', 'eu', 'fi', 'hk']) {
  assert.equal(fs.existsSync(`src/renderer/assets/flags/${code}.svg`), true, `${code}.svg is missing`);
}
const tauriConfig = JSON.parse(fs.readFileSync('src-tauri/tauri.conf.json', 'utf8'));
const mainWindow = tauriConfig.app.windows[0];
assert.equal(mainWindow.resizable, true);
assert.equal(mainWindow.minWidth, 720);
assert.equal(mainWindow.minHeight, 460);
const styles = fs.readFileSync('src/renderer/styles.css', 'utf8');
assert.match(styles, /grid-template-columns:\s*clamp\(320px, 45vw, 410px\)/);
assert.match(styles, /\.server-card\s*\{[^}]*min-height:\s*56px/s);
assert.match(styles, /\.connected-location\s*\{/);
const markup = fs.readFileSync('src/renderer/index.html', 'utf8');
assert.match(markup, /id="connectedLocation"[^>]*hidden/);
assert.match(markup, /id="connectedLocationFlag"/);
assert.match(markup, /id="connectedLocationName"/);
assert.match(markup, /id="sortMenu"[^>]*hidden/);
assert.match(markup, /id="fragmentationToggle"/);
assert.match(markup, /id="killSwitchToggle"/);
const storageValues = new Map();
const localStorage = { getItem: key => storageValues.get(key) ?? null, setItem: (key, value) => storageValues.set(key, value) };
const settingsWindow = { localStorage };
vm.runInNewContext(fs.readFileSync('src/renderer/settings-store.js', 'utf8'), { window: settingsWindow, URL });
const settings = settingsWindow.shadowVpnSettings;
assert.equal(settings.readDNS(), 'cloudflare');
assert.equal(settings.writeDNS('quad9'), 'quad9');
assert.equal(settings.readDNS(), 'quad9');
assert.equal(settings.writeDNS('https://attacker.example'), 'cloudflare');
assert.equal(settings.dnsProviders.length, 4);
assert.equal(settings.writeCustomDNS('1.1.1.1, 2606:4700:4700::1111'), '1.1.1.1, 2606:4700:4700::1111');
assert.deepEqual([...settings.parseCustomDNS(settings.readCustomDNS()).servers], ['1.1.1.1', '2606:4700:4700::1111']);
assert.ok(settings.parseCustomDNS('0.0.0.0').error);
assert.ok(settings.parseCustomDNS('not-an-ip').error);
assert.equal(settings.readFragmentation(), false);
assert.equal(settings.writeFragmentation(true), true);
assert.equal(settings.readFragmentation(), true);
assert.equal(settings.readKillSwitch(), false);
assert.equal(settings.writeKillSwitch(true), true);
assert.equal(settings.readKillSwitch(), true);
const stopScript = fs.readFileSync('stop-shadowvpn.ps1', 'utf8');
assert.match(stopScript, /ShadowVPN Kill Switch/);
assert.match(stopScript, /Remove-NetRoute/);
console.log('Renderer helpers: flags, responsive layout, and DNS settings passed.');

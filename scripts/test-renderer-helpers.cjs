const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const window = {};
vm.runInNewContext(fs.readFileSync('src/renderer/server-display.js', 'utf8'), { window, Intl });
vm.runInNewContext(fs.readFileSync('src/renderer/traffic-display.js', 'utf8'), { window, Number, Math });

const { regionalFlagCode, serverFlagAndName, sortServerProfiles, serverCategoriesForProfile, filterServerProfiles } = window.shadowVpnDisplay;
const { normalizeTraffic, formatBytes, formatSpeed } = window.shadowVpnTraffic;
assert.deepEqual(
  JSON.parse(JSON.stringify(normalizeTraffic({ uploadBytes: 1024, downloadBytes: -1, uploadBps: '512', downloadBps: Infinity }))),
  { uploadBytes: 1024, downloadBytes: 0, uploadBps: 512, downloadBps: 0 },
);
assert.equal(formatBytes(0), '0 Б');
assert.equal(formatBytes(1536), '1,5 КБ');
assert.equal(formatBytes(12 * 1024 * 1024), '12,0 МБ');
assert.equal(formatSpeed(2048), '2,0 КБ/с');
assert.equal(formatSpeed(normalizeTraffic(null).downloadBps), '0 Б/с');
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
const categoryProfiles = [
  { id: 'auto', auto: true, name: 'Авто' },
  { id: 'hysteria', name: '🇫🇮 Hysteria2 Finland' },
  { id: 'ws', name: 'Germany | WS' },
  { id: 'torrent', name: '🏴‍☠️ Torrent Server' },
  { id: 'gemini', name: '🇺🇸 Gemini Residential WS' },
  { id: 'warp', name: '🇩🇪 CloudFlare WARP' },
  { id: 'news', name: 'News Server' },
  { id: 'no-tls', name: '🇳🇱 Amsterdam No TLS' },
  { id: 'no-tls-dash', name: 'France No-TLS' },
  { id: 'tls', name: 'Sweden TLS' },
];
assert.deepEqual([...serverCategoriesForProfile(categoryProfiles[0])], ['all']);
assert.deepEqual([...serverCategoriesForProfile(categoryProfiles[4])], ['all', 'fast', 'gemini']);
assert.deepEqual(filterServerProfiles(categoryProfiles, 'fast').map(profile => profile.id), ['hysteria', 'ws', 'gemini']);
assert.equal(serverCategoriesForProfile(categoryProfiles[6]).includes('fast'), false);
assert.deepEqual(filterServerProfiles(categoryProfiles, 'p2p').map(profile => profile.id), ['torrent']);
assert.deepEqual(filterServerProfiles(categoryProfiles, 'gemini').map(profile => profile.id), ['gemini']);
assert.deepEqual(filterServerProfiles(categoryProfiles, 'warp').map(profile => profile.id), ['warp']);
assert.deepEqual(filterServerProfiles(categoryProfiles, 'no-tls').map(profile => profile.id), ['no-tls', 'no-tls-dash']);
assert.equal(serverCategoriesForProfile(categoryProfiles[9]).includes('no-tls'), false);
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
assert.match(styles, /\.server-card\.auto-card\s*\{[^}]*height:\s*56px/s);
assert.match(styles, /\.server-categories\s*\{[^}]*min-height:\s*38px[^}]*flex-shrink:\s*0/s);
assert.match(styles, /\.server-list\s*\{[^}]*flex:\s*1 1 auto/s);
assert.match(styles, /\.connected-location\s*\{/);
assert.match(styles, /\.setting-card\s*\{[^}]*margin-top:\s*10px/s);
assert.match(styles, /\.settings-section-title \+ \.setting-card\s*\{[^}]*margin-top:\s*0/s);
const markup = fs.readFileSync('src/renderer/index.html', 'utf8');
assert.match(markup, /id="connectedLocation"[^>]*hidden/);
assert.match(markup, /id="connectedLocationFlag"/);
assert.match(markup, /id="connectedLocationName"/);
assert.match(markup, /id="trafficStats"[^>]*hidden/);
assert.match(markup, /id="downloadSpeed"/);
assert.match(markup, /id="downloadTotal"/);
assert.match(markup, /id="uploadSpeed"/);
assert.match(markup, /id="uploadTotal"/);
assert.match(markup, /src="traffic-display\.js"/);
assert.match(markup, /id="sortMenu"[^>]*hidden/);
assert.match(markup, /id="serverCategoryTabs"[^>]*role="tablist"/);
for (const category of ['all', 'fast', 'p2p', 'gemini', 'warp', 'no-tls']) assert.match(markup, new RegExp(`data-server-category="${category}"`));
assert.match(markup, /id="pingBtn"[\s\S]*?class="ping-gauge"/);
assert.match(markup, /class="ping-gauge-needle"/);
assert.match(markup, /class="ping-gauge-needle" d="M12 17V11"/);
assert.match(markup, /id="fragmentationToggle"/);
assert.match(markup, /id="killSwitchToggle"/);
assert.match(markup, /id="deviceHwid"/);
assert.match(markup, /id="copyHwidBtn"/);
assert.match(markup, /id="autoUpdateSelect"/);
assert.match(markup, /id="autoUpdateDescription"/);
assert.match(markup, /id="pingMethodSelect"/);
assert.match(markup, /id="pingMethodDescription"/);
assert.match(markup, /id="routingModeSelect"/);
assert.match(markup, /id="routingDomainsInput"/);
assert.match(markup, /placeholder="example\.com&#10;geosite:youtube&#10;geoip:ru"/);
assert.match(markup, /id="routingGeoDataFields"[^>]*hidden/);
assert.match(markup, /id="geoIPURLInput"/);
assert.match(markup, /id="geoSiteURLInput"/);
assert.match(markup, /id="geoIPURLInput"[^>]*aria-describedby="geoDataError"/);
assert.match(markup, /id="geoSiteURLInput"[^>]*aria-describedby="geoDataError"/);
assert.match(markup, /id="geoDataError"/);
assert.match(markup, /Сеть[\s\S]*DNS внутри VPN[\s\S]*Маршрутизация[\s\S]*Режим маршрутизации/);
assert.match(markup, /Устройство[\s\S]*HWID устройства[\s\S]*Подписка[\s\S]*Обновление подписки[\s\S]*Сеть[\s\S]*DNS внутри VPN/);
assert.match(styles, /\.welcome\s*\{[^}]*overflow-y:\s*hidden/s);
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
assert.equal(settings.readAutoUpdate(), '60');
assert.equal(settings.writeAutoUpdate('15'), '15');
assert.equal(settings.readAutoUpdate(), '15');
assert.equal(settings.writeAutoUpdate('invalid'), '60');
assert.equal(settings.writeLastSubscriptionSync(123456), 123456);
assert.equal(settings.readLastSubscriptionSync(localStorage, 123456), 123456);
assert.equal(settings.readPingMethod(), 'tcp');
assert.equal(settings.writePingMethod('head'), 'head');
assert.equal(settings.readPingMethod(), 'head');
assert.equal(settings.writePingMethod('invalid'), 'tcp');
assert.equal(settings.readPingOnOpen(), false);
assert.equal(settings.writePingOnOpen(true), true);
assert.equal(settings.readPingOnOpen(), true);
assert.equal(settings.readRoutingMode(), 'full');
assert.equal(settings.writeRoutingMode('bypass'), 'bypass');
assert.equal(settings.readRoutingMode(), 'bypass');
assert.equal(settings.writeRoutingMode('invalid'), 'full');
assert.equal(settings.writeRoutingDomains('example.com\nfull:private.example.com'), 'example.com\nfull:private.example.com');
assert.equal(settings.readRoutingDomains(), 'example.com\nfull:private.example.com');
assert.equal(settings.routingModes.length, 3);
assert.equal(settings.writeGeoIPURL('https://example.com/geoip.dat'), 'https://example.com/geoip.dat');
assert.equal(settings.readGeoIPURL(), 'https://example.com/geoip.dat');
assert.equal(settings.writeGeoSiteURL('https://example.com/geosite.dat'), 'https://example.com/geosite.dat');
assert.equal(settings.readGeoSiteURL(), 'https://example.com/geosite.dat');
assert.equal(settings.validateGeoDataURL('http://example.com/geoip.dat', 'GeoIP').error.length > 0, true);
assert.equal(settings.validateGeoDataURL('https://user@example.com/geoip.dat', 'GeoIP').error.length > 0, true);
assert.equal(settings.validateGeoDataSources(['geoip:ru'], '', '').field, 'geoip');
assert.equal(settings.validateGeoDataSources(['geosite:youtube'], 'https://example.com/geoip.dat', '').field, 'geosite');
assert.equal(settings.validateGeoDataSources(['geoip:ru', 'geosite:youtube'], 'https://example.com/geoip.dat', 'https://example.com/geosite.dat').error, '');
const groupStorageValues = new Map();
const groupLocalStorage = { getItem: key => groupStorageValues.get(key) ?? null, setItem: (key, value) => groupStorageValues.set(key, value) };
const groupWindow = { localStorage: groupLocalStorage };
vm.runInNewContext(fs.readFileSync('src/renderer/group-store.js', 'utf8'), { window: groupWindow, localStorage: groupLocalStorage, Date, Math, Set, JSON });
const groups = groupWindow.shadowVpnGroups;
const group = groups.makeGroup([], ['111111111111111111111111', 'bad', '000000000000000000000000']);
assert.deepEqual([...group.profileIds], ['111111111111111111111111']);
let groupState = groups.writeState({ groups: [{ ...group, name: '  Моя   Auto  ' }], activeGroupId: group.id });
assert.equal(groupState.groups[0].name, 'Моя Auto');
assert.equal(groups.activeGroup(groupState).id, group.id);
assert.deepEqual([...groups.availableProfileIds(groupState.groups[0], [{ id: '111111111111111111111111' }, { id: '222222222222222222222222' }])], ['111111111111111111111111']);
assert.match(markup, /id="groupsMenuItem"/);
assert.match(markup, /id="groupsOverlay"[^>]*hidden/);
assert.match(markup, /src="group-store\.js"/);
assert.match(styles, /\.groups-panel\s*\{/);
assert.match(markup, /id="autoStartToggle"/);
assert.match(markup, /id="pingOnOpenToggle"/);
assert.match(markup, /class="connection-map"/);
assert.match(styles, /\.connection-map\s*\{/);
assert.match(styles, /\.traffic-stats\s*\{/);
assert.match(styles, /\.routing-geodata\s*\{/);
const stopScript = fs.readFileSync('stop-shadowvpn.ps1', 'utf8');
assert.match(stopScript, /ShadowVPN Kill Switch/);
assert.match(stopScript, /Remove-NetRoute/);
console.log('Renderer helpers: flags, responsive layout, and DNS settings passed.');

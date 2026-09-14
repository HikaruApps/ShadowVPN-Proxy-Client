// Small UI-facing API over Tauri IPC.
(() => {
  const { invoke } = window.__TAURI__.core;
  const { listen } = window.__TAURI__.event;
  const subscribers = new Set();
  const logSubscribers = new Set();
  const ready = listen('vpn:state', event => {
    for (const callback of subscribers) callback(event.payload);
  });
  listen('vpn:error', event => {
    document.getElementById('statusText').textContent = event.payload;
    document.getElementById('subscriptionError').textContent = event.payload;
  }).catch(() => {});
  listen('vpn:log', event => {
    for (const callback of logSubscribers) callback(event.payload);
  }).catch(() => {});
  async function call(command, args = {}) {
    try { await ready; return { ok: true, result: await invoke(command, args) }; }
    catch (error) { return { ok: false, error: String(error) }; }
  }
  window.vpnApi = Object.freeze({
    importSubscription: url => call('vpn_import', { url }),
    connect: (profileId, dns = 'cloudflare', dnsServers = [], fragmentation = false, killSwitch = false) => call('vpn_connect', { profileId, dns, dnsServers, fragmentation, killSwitch }),
    disconnect: () => call('vpn_disconnect'),
    ping: () => call('vpn_ping'),
    publicIp: (masked = false) => call('vpn_public_ip', { masked }),
    getState: async () => { await ready; return invoke('vpn_get_state'); },
    getLogs: () => call('vpn_get_logs'),
    clearLogs: () => call('vpn_clear_logs'),
    onStateChange: callback => { subscribers.add(callback); return () => subscribers.delete(callback); },
    onLog: callback => { logSubscribers.add(callback); return () => logSubscribers.delete(callback); },
  });
})();

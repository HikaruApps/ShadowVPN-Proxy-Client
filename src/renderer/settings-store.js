(() => {
  const dnsStorageKey = "shadowvpn.dnsProvider";
  const customDNSStorageKey = "shadowvpn.customDns";
  const fragmentationStorageKey = "shadowvpn.fragmentation";
  const killSwitchStorageKey = "shadowvpn.killSwitch";
  const autoUpdateStorageKey = "shadowvpn.subscriptionAutoUpdate";
  const lastSubscriptionSyncStorageKey = "shadowvpn.subscriptionLastSync";
  const pingMethodStorageKey = "shadowvpn.pingMethod";
  const pingOnOpenStorageKey = "shadowvpn.pingOnOpen";
  const routingModeStorageKey = "shadowvpn.routingMode";
  const routingDomainsStorageKey = "shadowvpn.routingDomains";
  const geoIPURLStorageKey = "shadowvpn.geoIpUrl";
  const geoSiteURLStorageKey = "shadowvpn.geoSiteUrl";
  const pingMethods = Object.freeze([
    Object.freeze({ id: "tcp", name: "TCP", detail: "быстрая проверка порта" }),
    Object.freeze({ id: "head", name: "HTTP HEAD", detail: "реальный запрос через сервер" }),
    Object.freeze({ id: "get", name: "HTTP GET", detail: "полная проверка через сервер" }),
  ]);
  const autoUpdateIntervals = Object.freeze([
    Object.freeze({ id: "15", name: "Каждые 15 минут", minutes: 15 }),
    Object.freeze({ id: "60", name: "Каждый час", minutes: 60 }),
    Object.freeze({ id: "360", name: "Каждые 6 часов", minutes: 360 }),
    Object.freeze({ id: "1440", name: "Раз в сутки", minutes: 1440 }),
    Object.freeze({ id: "off", name: "Выключено", minutes: 0 }),
  ]);
  const dnsProviders = Object.freeze([
    Object.freeze({ id: "cloudflare", name: "Cloudflare", detail: "1.1.1.1" }),
    Object.freeze({ id: "google", name: "Google", detail: "8.8.8.8" }),
    Object.freeze({ id: "quad9", name: "Quad9", detail: "с блокировкой угроз" }),
    Object.freeze({ id: "custom", name: "Свой DNS", detail: "IPv4 или IPv6" }),
  ]);

  function normalizeDNS(value) {
    return dnsProviders.some(provider => provider.id === value) ? value : "cloudflare";
  }

  function readDNS(storage = window.localStorage) {
    try {
      return normalizeDNS(storage.getItem(dnsStorageKey));
    } catch {
      return "cloudflare";
    }
  }

  function writeDNS(value, storage = window.localStorage) {
    const normalized = normalizeDNS(value);
    try {
      storage.setItem(dnsStorageKey, normalized);
    } catch {
      // The setting still applies for the current session if storage is unavailable.
    }
    return normalized;
  }

  function readCustomDNS(storage = window.localStorage) {
    try {
      return String(storage.getItem(customDNSStorageKey) || "").slice(0, 200);
    } catch {
      return "";
    }
  }

  function writeCustomDNS(value, storage = window.localStorage) {
    const normalized = String(value || "").slice(0, 200);
    try {
      storage.setItem(customDNSStorageKey, normalized);
    } catch {
      // Keep the input in memory if storage is unavailable.
    }
    return normalized;
  }

  function readFragmentation(storage = window.localStorage) {
    try {
      return storage.getItem(fragmentationStorageKey) === "true";
    } catch {
      return false;
    }
  }

  function writeFragmentation(value, storage = window.localStorage) {
    const enabled = Boolean(value);
    try {
      storage.setItem(fragmentationStorageKey, String(enabled));
    } catch {
      // Keep the value for this session if storage is unavailable.
    }
    return enabled;
  }

  function readKillSwitch(storage = window.localStorage) {
    try {
      return storage.getItem(killSwitchStorageKey) === "true";
    } catch {
      return false;
    }
  }

  function writeKillSwitch(value, storage = window.localStorage) {
    const enabled = Boolean(value);
    try {
      storage.setItem(killSwitchStorageKey, String(enabled));
    } catch {
      // Keep the value for this session if storage is unavailable.
    }
    return enabled;
  }

  function normalizeAutoUpdate(value) {
    return autoUpdateIntervals.some(item => item.id === value) ? value : "60";
  }

  function readAutoUpdate(storage = window.localStorage) {
    try { return normalizeAutoUpdate(storage.getItem(autoUpdateStorageKey)); }
    catch { return "60"; }
  }

  function writeAutoUpdate(value, storage = window.localStorage) {
    const normalized = normalizeAutoUpdate(value);
    try { storage.setItem(autoUpdateStorageKey, normalized); } catch { /* Keep the session value. */ }
    return normalized;
  }

  function readLastSubscriptionSync(storage = window.localStorage, now = Date.now()) {
    try {
      const value = Number(storage.getItem(lastSubscriptionSyncStorageKey));
      return Number.isFinite(value) && value > 0 && value <= now + 300000 ? value : 0;
    } catch { return 0; }
  }

  function writeLastSubscriptionSync(value = Date.now(), storage = window.localStorage) {
    const normalized = Number.isFinite(Number(value)) && Number(value) > 0 ? Math.floor(Number(value)) : Date.now();
    try { storage.setItem(lastSubscriptionSyncStorageKey, String(normalized)); } catch { /* Scheduling still works for this session. */ }
    return normalized;
  }

  function normalizePingMethod(value) {
    return pingMethods.some(item => item.id === value) ? value : "tcp";
  }

  function readPingMethod(storage = window.localStorage) {
    try { return normalizePingMethod(storage.getItem(pingMethodStorageKey)); }
    catch { return "tcp"; }
  }

  function writePingMethod(value, storage = window.localStorage) {
    const normalized = normalizePingMethod(value);
    try { storage.setItem(pingMethodStorageKey, normalized); } catch { /* Keep the session value. */ }
    return normalized;
  }

  function readPingOnOpen(storage = window.localStorage) {
    try { return storage.getItem(pingOnOpenStorageKey) === "true"; }
    catch { return false; }
  }

  function writePingOnOpen(value, storage = window.localStorage) {
    const enabled = Boolean(value);
    try { storage.setItem(pingOnOpenStorageKey, String(enabled)); } catch { /* Keep the session value. */ }
    return enabled;
  }

  const routingModes = Object.freeze([
    Object.freeze({ id: "full", name: "Весь трафик через VPN", detail: "Стандартный режим ShadowVPN" }),
    Object.freeze({ id: "bypass", name: "Выбранные домены напрямую", detail: "Остальное идёт через VPN" }),
    Object.freeze({ id: "proxy_only", name: "Только выбранные домены через VPN", detail: "Остальное подключается напрямую" }),
  ]);

  function normalizeRoutingMode(value) {
    return routingModes.some(item => item.id === value) ? value : "full";
  }

  function readRoutingMode(storage = window.localStorage) {
    try { return normalizeRoutingMode(storage.getItem(routingModeStorageKey)); }
    catch { return "full"; }
  }

  function writeRoutingMode(value, storage = window.localStorage) {
    const normalized = normalizeRoutingMode(value);
    try { storage.setItem(routingModeStorageKey, normalized); } catch { /* Keep the session value. */ }
    return normalized;
  }

  function readRoutingDomains(storage = window.localStorage) {
    try { return String(storage.getItem(routingDomainsStorageKey) || "").slice(0, 12000); }
    catch { return ""; }
  }

  function writeRoutingDomains(value, storage = window.localStorage) {
    const normalized = String(value || "").slice(0, 12000);
    try { storage.setItem(routingDomainsStorageKey, normalized); } catch { /* Keep the session value. */ }
    return normalized;
  }

  function readGeoDataURL(key, storage = window.localStorage) {
    try { return String(storage.getItem(key) || "").slice(0, 2048); }
    catch { return ""; }
  }

  function writeGeoDataURL(key, value, storage = window.localStorage) {
    const normalized = String(value || "").slice(0, 2048);
    try { storage.setItem(key, normalized); } catch { /* Keep the session value. */ }
    return normalized;
  }

  function readGeoIPURL(storage = window.localStorage) {
    return readGeoDataURL(geoIPURLStorageKey, storage);
  }

  function writeGeoIPURL(value, storage = window.localStorage) {
    return writeGeoDataURL(geoIPURLStorageKey, value, storage);
  }

  function readGeoSiteURL(storage = window.localStorage) {
    return readGeoDataURL(geoSiteURLStorageKey, storage);
  }

  function writeGeoSiteURL(value, storage = window.localStorage) {
    return writeGeoDataURL(geoSiteURLStorageKey, value, storage);
  }

  function validateGeoDataURL(value, title) {
    const normalized = String(value || "").trim();
    if (!normalized) return { value: "", error: "" };
    if (normalized.length > 2048 || /\s/.test(normalized)) {
      return { value: "", error: `${title}: ссылка слишком длинная или содержит пробелы` };
    }
    try {
      const parsed = new URL(normalized);
      if (parsed.protocol !== "https:" || !parsed.hostname || parsed.username || parsed.password || parsed.hash) {
        return { value: "", error: `${title}: нужна обычная HTTPS-ссылка без логина и фрагмента` };
      }
    } catch {
      return { value: "", error: `${title}: некорректная HTTPS-ссылка` };
    }
    return { value: normalized, error: "" };
  }

  function validateGeoDataSources(rules, geoIPURL, geoSiteURL) {
    const geoIP = validateGeoDataURL(geoIPURL, "GeoIP");
    if (geoIP.error) return { geoIPURL: "", geoSiteURL: "", error: geoIP.error, field: "geoip" };
    const geoSite = validateGeoDataURL(geoSiteURL, "GeoSite");
    if (geoSite.error) return { geoIPURL: "", geoSiteURL: "", error: geoSite.error, field: "geosite" };
    const values = Array.isArray(rules) ? rules : [];
    const needsGeoIP = values.some(rule => String(rule).trim().toLowerCase().startsWith("geoip:"));
    const needsGeoSite = values.some(rule => String(rule).trim().toLowerCase().startsWith("geosite:"));
    if (needsGeoIP && !geoIP.value) {
      return { geoIPURL: "", geoSiteURL: geoSite.value, error: "Для правил geoip: укажите ссылку на geoip.dat", field: "geoip" };
    }
    if (needsGeoSite && !geoSite.value) {
      return { geoIPURL: geoIP.value, geoSiteURL: "", error: "Для правил geosite: укажите ссылку на geosite.dat", field: "geosite" };
    }
    return { geoIPURL: geoIP.value, geoSiteURL: geoSite.value, error: "", field: "" };
  }

  function validIPv4(value) {
    const parts = value.split(".");
    if (parts.length !== 4 || !parts.every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255)) return false;
    const numbers = parts.map(Number);
    if (numbers.every(number => number === 0) || numbers.every(number => number === 255)) return false;
    return numbers[0] < 224 || numbers[0] > 239;
  }

  function validIPv6(value) {
    if (!value.includes(":") || !/^[0-9a-f:]+$/i.test(value) || value === "::" || /^ff/i.test(value)) return false;
    try {
      return new URL(`http://[${value}]/`).hostname.startsWith("[");
    } catch {
      return false;
    }
  }

  function parseCustomDNS(value) {
    const servers = [...new Set(String(value || "").split(/[\s,;]+/).map(item => item.trim()).filter(Boolean))];
    if (servers.length === 0) return { servers: [], error: "Укажите хотя бы один DNS IP-адрес" };
    if (servers.length > 4) return { servers: [], error: "Можно указать не больше четырёх DNS-адресов" };
    if (servers.some(server => !validIPv4(server) && !validIPv6(server))) {
      return { servers: [], error: "Введите корректные IPv4 или IPv6-адреса" };
    }
    return { servers, error: "" };
  }

  window.shadowVpnSettings = Object.freeze({
    dnsProviders,
    normalizeDNS,
    readDNS,
    writeDNS,
    readCustomDNS,
    writeCustomDNS,
    readFragmentation,
    writeFragmentation,
    readKillSwitch,
    writeKillSwitch,
    autoUpdateIntervals,
    normalizeAutoUpdate,
    readAutoUpdate,
    writeAutoUpdate,
    readLastSubscriptionSync,
    writeLastSubscriptionSync,
    pingMethods,
    normalizePingMethod,
    readPingMethod,
    writePingMethod,
    readPingOnOpen,
    writePingOnOpen,
    routingModes,
    normalizeRoutingMode,
    readRoutingMode,
    writeRoutingMode,
    readRoutingDomains,
    writeRoutingDomains,
    readGeoIPURL,
    writeGeoIPURL,
    readGeoSiteURL,
    writeGeoSiteURL,
    validateGeoDataURL,
    validateGeoDataSources,
    parseCustomDNS,
  });
})();

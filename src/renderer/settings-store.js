(() => {
  const dnsStorageKey = "shadowvpn.dnsProvider";
  const customDNSStorageKey = "shadowvpn.customDns";
  const fragmentationStorageKey = "shadowvpn.fragmentation";
  const killSwitchStorageKey = "shadowvpn.killSwitch";
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
    parseCustomDNS,
  });
})();

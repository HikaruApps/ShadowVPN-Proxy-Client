(() => {
  const storageKey = "shadowvpn.subscriptions.v1";
  const legacyStorageKey = "shadowvpn.subscriptionUrl";
  const maxSubscriptions = 16;

  function validURL(value) {
    try {
      const parsed = new URL(String(value || "").trim());
      return parsed.protocol === "https:" && Boolean(parsed.hostname) && !parsed.username && !parsed.password;
    } catch { return false; }
  }

  function cleanItem(value) {
    if (!value || typeof value !== "object") return null;
    const id = String(value.id || "");
    const url = String(value.url || "").trim();
    if (!/^sub-[0-9a-z-]{8,}$/i.test(id) || !validURL(url)) return null;
    return { id, url };
  }

  function normalizeState(value) {
    const seenURLs = new Set();
    const items = [];
    for (const raw of Array.isArray(value?.items) ? value.items : []) {
      const item = cleanItem(raw);
      if (!item || seenURLs.has(item.url)) continue;
      seenURLs.add(item.url);
      items.push(item);
      if (items.length >= maxSubscriptions) break;
    }
    const selectedId = items.some(item => item.id === value?.selectedId) ? value.selectedId : items[0]?.id || "";
    const activeId = items.some(item => item.id === value?.activeId) ? value.activeId : "";
    return { items, selectedId, activeId };
  }

  function makeId(items) {
    const used = new Set(items.map(item => item.id));
    let id;
    do { id = `sub-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`; } while (used.has(id));
    return id;
  }

  function readState(storage = localStorage) {
    try {
      const parsed = JSON.parse(storage.getItem(storageKey) || "null");
      if (parsed) return normalizeState(parsed);
    } catch { /* Fall back to the legacy single-subscription key. */ }
    try {
      const legacyURL = String(storage.getItem(legacyStorageKey) || "").trim();
      if (validURL(legacyURL)) {
        const item = { id: makeId([]), url: legacyURL };
        return { items: [item], selectedId: item.id, activeId: "" };
      }
    } catch { /* Return an empty state when storage is unavailable. */ }
    return { items: [], selectedId: "", activeId: "" };
  }

  function writeState(value, storage = localStorage) {
    const state = normalizeState(value);
    try {
      storage.setItem(storageKey, JSON.stringify(state));
      if (state.items[0]) storage.setItem(legacyStorageKey, state.items[0].url);
      else storage.removeItem(legacyStorageKey);
    } catch { /* Keep the state for this session. */ }
    return state;
  }

  function addItem(state, url) {
    const normalized = normalizeState(state);
    const cleanURL = String(url || "").trim();
    if (!validURL(cleanURL)) return { state: normalized, error: "Введите корректную HTTPS-ссылку" };
    if (normalized.items.some(item => item.url === cleanURL)) return { state: normalized, error: "Эта подписка уже добавлена" };
    if (normalized.items.length >= maxSubscriptions) return { state: normalized, error: `Можно добавить не больше ${maxSubscriptions} подписок` };
    const item = { id: makeId(normalized.items), url: cleanURL };
    return { state: { items: [...normalized.items, item], selectedId: item.id, activeId: normalized.activeId }, item, error: "" };
  }

  function updateItem(state, id, url) {
    const normalized = normalizeState(state);
    const cleanURL = String(url || "").trim();
    if (!validURL(cleanURL)) return { state: normalized, error: "Введите корректную HTTPS-ссылку" };
    if (normalized.items.some(item => item.id !== id && item.url === cleanURL)) return { state: normalized, error: "Эта подписка уже добавлена" };
    if (!normalized.items.some(item => item.id === id)) return { state: normalized, error: "Подписка не найдена" };
    return {
      state: { items: normalized.items.map(item => item.id === id ? { ...item, url: cleanURL } : item), selectedId: id, activeId: normalized.activeId },
      error: "",
    };
  }

  function removeItem(state, id) {
    const normalized = normalizeState(state);
    const items = normalized.items.filter(item => item.id !== id);
    return {
      items,
      selectedId: items.some(item => item.id === normalized.selectedId) ? normalized.selectedId : items[0]?.id || "",
      activeId: items.some(item => item.id === normalized.activeId) ? normalized.activeId : "",
    };
  }

  function activeItems(state) {
    const normalized = normalizeState(state);
    if (!normalized.activeId) return normalized.items;
    return normalized.items.filter(item => item.id === normalized.activeId);
  }

  function displayHost(value) {
    try { return new URL(String(value)).hostname; } catch { return "Подписка"; }
  }

  window.shadowVpnSubscriptions = Object.freeze({
    storageKey, legacyStorageKey, maxSubscriptions, validURL, readState, writeState,
    addItem, updateItem, removeItem, activeItems, displayHost,
  });
})();

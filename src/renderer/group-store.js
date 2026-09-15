(() => {
  const storageKey = "shadowvpn.serverGroups.v1";
  const profileIdPattern = /^[0-9a-f]{24}$/i;

  function cleanName(value) {
    return String(value || "").trim().replace(/\s+/g, " ").slice(0, 40) || "Новая группа";
  }

  function cleanProfileIds(values) {
    return [...new Set((Array.isArray(values) ? values : []).map(String).filter(value => profileIdPattern.test(value) && !/^0{24}$/.test(value)))];
  }

  function normalizeGroup(value) {
    if (!value || typeof value !== "object" || !/^group-[0-9a-z-]{8,}$/i.test(String(value.id))) return null;
    return { id: String(value.id), name: cleanName(value.name), profileIds: cleanProfileIds(value.profileIds) };
  }

  function readState() {
    try {
      const parsed = JSON.parse(localStorage.getItem(storageKey) || "{}");
      const groups = (Array.isArray(parsed.groups) ? parsed.groups : []).map(normalizeGroup).filter(Boolean);
      const activeGroupId = groups.some(group => group.id === parsed.activeGroupId) ? parsed.activeGroupId : "";
      return { groups, activeGroupId };
    } catch { return { groups: [], activeGroupId: "" }; }
  }

  function writeState(value) {
    const groups = (Array.isArray(value?.groups) ? value.groups : []).map(normalizeGroup).filter(Boolean).slice(0, 24);
    const activeGroupId = groups.some(group => group.id === value?.activeGroupId) ? value.activeGroupId : "";
    const state = { groups, activeGroupId };
    try { localStorage.setItem(storageKey, JSON.stringify(state)); } catch { /* Keep the session state in the renderer. */ }
    return state;
  }

  function makeGroup(existingGroups, profileIds) {
    const used = new Set(existingGroups.map(group => group.id));
    let id;
    do { id = `group-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`; } while (used.has(id));
    return { id, name: "Новая группа", profileIds: cleanProfileIds(profileIds) };
  }

  function activeGroup(state) {
    return state.groups.find(group => group.id === state.activeGroupId) || null;
  }

  function availableProfileIds(group, profiles) {
    if (!group) return [];
    const available = new Set((Array.isArray(profiles) ? profiles : []).filter(profile => !profile.auto).map(profile => profile.id));
    return group.profileIds.filter(id => available.has(id));
  }

  window.shadowVpnGroups = { readState, writeState, makeGroup, cleanName, cleanProfileIds, activeGroup, availableProfileIds };
})();

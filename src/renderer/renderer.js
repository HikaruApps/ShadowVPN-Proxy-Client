let serverProfiles = [];
let requestBusy = false;

const powerBtn = document.getElementById("powerBtn");
const statusText = document.getElementById("statusText");
const serverList = document.getElementById("serverList");
const pingBtn = document.getElementById("pingBtn");
const sortBtn = document.getElementById("sortBtn");
const sortMenu = document.getElementById("sortMenu");
const serversHeading = document.getElementById("serversHeading");
const serverCategoryTabs = document.getElementById("serverCategoryTabs");
const menuBtn = document.getElementById("menuBtn");
const appMenu = document.getElementById("appMenu");
const logsMenuItem = document.getElementById("logsMenuItem");
const syncSubscriptionBtn = document.getElementById("syncSubscriptionBtn");
const settingsMenuItem = document.getElementById("settingsMenuItem");
const groupsMenuItem = document.getElementById("groupsMenuItem");
const changeSubscriptionBtn = document.getElementById("changeSubscriptionBtn");
const settingsOverlay = document.getElementById("settingsOverlay");
const closeSettingsBtn = document.getElementById("closeSettingsBtn");
const dnsSelect = document.getElementById("dnsSelect");
const dnsDescription = document.getElementById("dnsDescription");
const customDnsFields = document.getElementById("customDnsFields");
const customDnsInput = document.getElementById("customDnsInput");
const customDnsError = document.getElementById("customDnsError");
const routingModeSelect = document.getElementById("routingModeSelect");
const routingModeDescription = document.getElementById("routingModeDescription");
const routingDomainsFields = document.getElementById("routingDomainsFields");
const routingDomainsInput = document.getElementById("routingDomainsInput");
const routingGeoDataFields = document.getElementById("routingGeoDataFields");
const geoIPURLInput = document.getElementById("geoIPURLInput");
const geoSiteURLInput = document.getElementById("geoSiteURLInput");
const geoDataError = document.getElementById("geoDataError");
const autoUpdateSelect = document.getElementById("autoUpdateSelect");
const autoUpdateDescription = document.getElementById("autoUpdateDescription");
const pingMethodSelect = document.getElementById("pingMethodSelect");
const pingMethodDescription = document.getElementById("pingMethodDescription");
const fragmentationToggle = document.getElementById("fragmentationToggle");
const killSwitchToggle = document.getElementById("killSwitchToggle");
const autoStartToggle = document.getElementById("autoStartToggle");
const autoStartDescription = document.getElementById("autoStartDescription");
const pingOnOpenToggle = document.getElementById("pingOnOpenToggle");
const deviceHwid = document.getElementById("deviceHwid");
const copyHwidBtn = document.getElementById("copyHwidBtn");
const logsOverlay = document.getElementById("logsOverlay");
const logsOutput = document.getElementById("logsOutput");
const logsCount = document.getElementById("logsCount");
const logsState = document.getElementById("logsState");
const logsLiveDot = document.getElementById("logsLiveDot");
const copyLogsBtn = document.getElementById("copyLogsBtn");
const clearLogsBtn = document.getElementById("clearLogsBtn");
const closeLogsBtn = document.getElementById("closeLogsBtn");
const connectedLocation = document.getElementById("connectedLocation");
const connectedLocationFlag = document.getElementById("connectedLocationFlag");
const connectedLocationName = document.getElementById("connectedLocationName");
const connectionDetails = document.getElementById("connectionDetails");
const ipLabel = document.getElementById("ipLabel");
const ipValue = document.getElementById("ipValue");
const connectionDivider = document.getElementById("connectionDivider");
const connectionDuration = document.getElementById("connectionDuration");
const connectionTime = document.getElementById("connectionTime");
const trafficStats = document.getElementById("trafficStats");
const downloadSpeed = document.getElementById("downloadSpeed");
const downloadTotal = document.getElementById("downloadTotal");
const uploadSpeed = document.getElementById("uploadSpeed");
const uploadTotal = document.getElementById("uploadTotal");
const groupsOverlay = document.getElementById("groupsOverlay");
const closeGroupsBtn = document.getElementById("closeGroupsBtn");
const createGroupBtn = document.getElementById("createGroupBtn");
const groupsList = document.getElementById("groupsList");
const groupEditor = document.getElementById("groupEditor");
const groupEmpty = document.getElementById("groupEmpty");
const groupFields = document.getElementById("groupFields");
const groupNameInput = document.getElementById("groupNameInput");
const activateGroupBtn = document.getElementById("activateGroupBtn");
const groupAutoDescription = document.getElementById("groupAutoDescription");
const groupHostCount = document.getElementById("groupHostCount");
const groupSelectAllBtn = document.getElementById("groupSelectAllBtn");
const groupClearBtn = document.getElementById("groupClearBtn");
const groupHosts = document.getElementById("groupHosts");
const deleteGroupBtn = document.getElementById("deleteGroupBtn");

let selectedGroupId = "auto";
const AUTO_PROFILE_ID = "000000000000000000000000";
const AUTO_NO_RU_PROFILE_ID = "000000000000000000000001";
let currentState = "disconnected";
const pingResults = new Map();
let directIp = "";
let directIpPromise = null;
let encryptionTimer = null;
let connectionTimer = null;
let connectedAt = 0;
let menuOpen = false;
let logsLoading = false;
let pendingLogLines = [];
let logLineCount = 0;
let sortMenuOpen = false;
const { serverFlagAndName, sortServerProfiles, filterServerProfiles } = window.shadowVpnDisplay;
const { normalizeTraffic, formatBytes, formatSpeed } = window.shadowVpnTraffic;
const { dnsProviders, readDNS, writeDNS, readCustomDNS, writeCustomDNS, readFragmentation, writeFragmentation, readKillSwitch, writeKillSwitch, autoUpdateIntervals, readAutoUpdate, writeAutoUpdate, readLastSubscriptionSync, writeLastSubscriptionSync, pingMethods, readPingMethod, writePingMethod, readPingOnOpen, writePingOnOpen, routingModes, readRoutingMode, writeRoutingMode, readRoutingDomains, writeRoutingDomains, readGeoIPURL, writeGeoIPURL, readGeoSiteURL, writeGeoSiteURL, validateGeoDataSources, parseCustomDNS } = window.shadowVpnSettings;
const groupStore = window.shadowVpnGroups;
let groupState = groupStore.readState();
let editingGroupId = groupState.groups[0]?.id || "";
let selectedDNS = readDNS();
let customDNSValue = readCustomDNS();
let fragmentationEnabled = readFragmentation();
let killSwitchEnabled = readKillSwitch();
let selectedAutoUpdate = readAutoUpdate();
let selectedPingMethod = readPingMethod();
let pingOnOpenEnabled = readPingOnOpen();
let selectedRoutingMode = readRoutingMode();
let routingDomainsValue = readRoutingDomains();
let geoIPURLValue = readGeoIPURL();
let geoSiteURLValue = readGeoSiteURL();
let autoUpdateTimer = null;
let pendingSubscriptionRefresh = false;
let currentHWID = "";
const sortStorageKey = "shadowvpn.serverSort";
const sortModes = new Set(["alphabetical", "subscription", "latency"]);
let selectedSort = "subscription";
const categoryLabels = new Map([
  ["all", "Все серверы"],
  ["fast", "Быстрые"],
  ["p2p", "P2P"],
  ["gemini", "Gemini"],
  ["warp", "WARP"],
  ["no-tls", "No TLS"],
]);
let selectedCategory = "all";
try {
  const savedSort = localStorage.getItem(sortStorageKey);
  if (sortModes.has(savedSort)) selectedSort = savedSort;
} catch { /* Keep the subscription order when storage is unavailable. */ }

const STATUS_LABEL = {
  disconnected: "Нажмите, чтобы подключиться",
  connecting: "Подключение…",
  connected: "Подключено",
  disconnecting: "Отключение…",
};

function renderServerList() {
  serverList.replaceChildren();
  const visibleProfiles = filterServerProfiles(serverProfiles, selectedCategory);
  if (currentState === "disconnected" && !visibleProfiles.some(profile => profile.id === selectedGroupId)) {
    selectedGroupId = visibleProfiles[0]?.id || "";
  }
  if (!visibleProfiles.length) {
    const empty = document.createElement("div");
    empty.className = "server-list-empty";
    empty.textContent = "В этой категории пока нет серверов";
    serverList.append(empty);
    return;
  }
  for (const profile of sortServerProfiles(visibleProfiles, pingResults, selectedSort)) {
    const card = document.createElement("button");
    card.type = "button";
    card.className = "server-card";
    card.classList.toggle("auto-card", Boolean(profile.auto));
    card.dataset.groupId = profile.id;
    card.classList.toggle("selected", profile.id === selectedGroupId);
    const activeAutoGroup = profile.auto ? groupStore.activeGroup(groupState) : null;
    const autoCandidates = profile.auto ? autoProfilesFor(profile.id) : [];
    const display = serverFlagAndName(profile.name);
    const icon = document.createElement("div");
    icon.className = "server-icon";
    icon.setAttribute("aria-hidden", "true");
    if (profile.auto) {
      icon.classList.add("auto-icon");
      icon.textContent = "✦";
    } else if (display.flagCode) {
      const image = document.createElement("img");
      image.src = `assets/flags/${display.flagCode}.svg`;
      image.alt = "";
      image.addEventListener("error", () => {
        icon.classList.remove("country-flag");
        icon.replaceChildren();
        icon.textContent = display.flag;
      }, { once: true });
      icon.classList.add("country-flag");
      icon.append(image);
    } else {
      icon.textContent = display.flag;
    }
    const text = document.createElement("div"); text.className = "server-text";
    const name = document.createElement("span"); name.className = "server-name"; name.textContent = display.name;
    const meta = document.createElement("span"); meta.className = "server-meta";
    meta.textContent = profile.auto
      ? activeAutoGroup
        ? `${activeAutoGroup.name.toUpperCase()}${profile.id === AUTO_NO_RU_PROFILE_ID ? " · БЕЗ РФ" : ""} · ${autoCandidates.length} СЕРВ.`
        : profile.id === AUTO_NO_RU_PROFILE_ID
          ? `АВТОВЫБОР · БЕЗ РФ · ${autoCandidates.length} СЕРВ.`
          : "АВТОВЫБОР · ВСЕ СЕРВЕРЫ"
      : `${profile.protocol.toUpperCase()} / ${profile.transport.toUpperCase()}`;
    const ping = document.createElement("span");
    ping.className = "server-ping";
    const result = pingResults.get(profile.id);
    if (result === "pending") {
      ping.textContent = "…";
      ping.classList.add("pending");
    } else if (result?.available) {
      ping.textContent = `${result.latencyMs} мс`;
      ping.classList.add(result.latencyMs >= 250 ? "critical" : result.latencyMs >= 120 ? "slow" : "good");
    } else if (result) {
      ping.textContent = "н/д";
      ping.classList.add("unavailable");
    } else {
      ping.textContent = "—";
    }
    text.append(name, meta); card.append(icon, text, ping);
    card.addEventListener("click", () => {
      if (requestBusy || currentState !== "disconnected") return;
      selectedGroupId = profile.id;
      serverList.querySelectorAll(".server-card").forEach(row => row.classList.toggle("selected", row.dataset.groupId === profile.id));
    });
    serverList.append(card);
  }
}

function updateCategoryControl() {
  serversHeading.textContent = categoryLabels.get(selectedCategory) || "Все серверы";
  for (const tab of serverCategoryTabs.querySelectorAll("[data-server-category]")) {
    const active = tab.dataset.serverCategory === selectedCategory;
    tab.classList.toggle("active", active);
    tab.setAttribute("aria-selected", String(active));
    tab.tabIndex = active ? 0 : -1;
  }
}

function setSortMenuOpen(open) {
  sortMenuOpen = open;
  sortMenu.hidden = !open;
  sortBtn.setAttribute("aria-expanded", String(open));
  if (open) sortMenu.querySelector(`[data-sort-mode="${selectedSort}"]`)?.focus({ preventScroll: true });
}

function updateSortControl() {
  const labels = {
    alphabetical: "По алфавиту",
    subscription: "Как в подписке",
    latency: "По минимальной задержке",
  };
  sortBtn.title = `Сортировка: ${labels[selectedSort]}`;
  sortBtn.setAttribute("aria-label", sortBtn.title);
  for (const item of sortMenu.querySelectorAll("[data-sort-mode]")) {
    const selected = item.dataset.sortMode === selectedSort;
    item.classList.toggle("selected", selected);
    item.setAttribute("aria-checked", String(selected));
  }
}

let receivedPush = false;

function isIPv4(value) {
  const parts = String(value).split(".");
  return parts.length === 4 && parts.every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}

function setIpText(label, value, mode = "") {
  ipLabel.textContent = label;
  ipValue.textContent = value;
  connectionDetails.classList.remove("protected", "unavailable");
  if (mode) connectionDetails.classList.add(mode);
}

function clearConnectedLocation() {
  connectedLocation.hidden = true;
  connectedLocationFlag.replaceChildren();
  connectedLocationFlag.classList.remove("country-flag");
  connectedLocationName.textContent = "";
}

function setConnectedLocation(profile) {
  if (!profile?.name) return clearConnectedLocation();
  const display = serverFlagAndName(profile.name);
  connectedLocationFlag.replaceChildren();
  connectedLocationFlag.classList.remove("country-flag");
  if (display.flagCode) {
    const image = document.createElement("img");
    image.src = `assets/flags/${display.flagCode}.svg`;
    image.alt = "";
    image.addEventListener("error", () => {
      connectedLocationFlag.classList.remove("country-flag");
      connectedLocationFlag.replaceChildren();
      connectedLocationFlag.textContent = display.flag;
    }, { once: true });
    connectedLocationFlag.classList.add("country-flag");
    connectedLocationFlag.append(image);
  } else {
    connectedLocationFlag.textContent = display.flag;
  }
  connectedLocationName.textContent = display.name;
  connectedLocation.hidden = false;
}

function applyAutoProfileChange(profile) {
  if (currentState !== "connected" || !profile?.name) return;
  const previousName = connectedLocationName.textContent;
  setConnectedLocation(profile);
  if (previousName && previousName !== connectedLocationName.textContent && !window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    connectedLocation.animate(
      [{ opacity: .35, transform: "translateY(3px)" }, { opacity: 1, transform: "translateY(0)" }],
      { duration: 380, easing: "cubic-bezier(.16, 1, .3, 1)" },
    );
  }
}

function stopIpEncryption() {
  if (encryptionTimer) window.clearInterval(encryptionTimer);
  encryptionTimer = null;
  connectionDetails.classList.remove("encrypting");
}

function startIpEncryption(label = "Шифрование IP") {
  stopIpEncryption();
  connectionDetails.classList.add("encrypting");
  ipLabel.textContent = label;
  let tick = 0;
  const source = isIPv4(directIp) ? directIp.split(".") : ["000", "000", "000", "000"];
  const renderFrame = () => {
    const hiddenParts = Math.min(4, Math.floor(tick / 3));
    ipValue.textContent = source.map((part, index) => {
      if (index >= 4 - hiddenParts) return "***";
      return [...part].map((digit, position) => (tick + index + position) % 3 === 0 ? String(Math.floor(Math.random() * 10)) : digit).join("");
    }).join(".");
    tick += 1;
    if (tick > 13) {
      window.clearInterval(encryptionTimer);
      encryptionTimer = null;
      ipValue.textContent = "***.***.***.***";
    }
  };
  renderFrame();
  encryptionTimer = window.setInterval(renderFrame, 85);
}

function formatDuration(totalSeconds) {
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  return [hours, minutes, seconds].map(value => String(value).padStart(2, "0")).join(":");
}

function updateConnectionTime() {
  if (!connectedAt) return;
  connectionTime.textContent = formatDuration(Math.max(0, Math.floor((Date.now() - connectedAt) / 1000)));
}

function startConnectionTimer() {
  if (!connectedAt) connectedAt = Date.now();
  connectionDivider.hidden = false;
  connectionDuration.hidden = false;
  updateConnectionTime();
  if (!connectionTimer) connectionTimer = window.setInterval(updateConnectionTime, 1000);
}

function stopConnectionTimer() {
  if (connectionTimer) window.clearInterval(connectionTimer);
  connectionTimer = null;
  connectedAt = 0;
  connectionTime.textContent = "00:00:00";
  connectionDivider.hidden = true;
  connectionDuration.hidden = true;
}

function resetTrafficDisplay(show = false) {
  downloadSpeed.textContent = "0 Б/с";
  downloadTotal.textContent = "0 Б";
  uploadSpeed.textContent = "0 Б/с";
  uploadTotal.textContent = "0 Б";
  trafficStats.hidden = !show;
}

function applyTraffic(sample) {
  if (currentState !== "connected") return;
  const traffic = normalizeTraffic(sample);
  downloadSpeed.textContent = formatSpeed(traffic.downloadBps);
  downloadTotal.textContent = formatBytes(traffic.downloadBytes);
  uploadSpeed.textContent = formatSpeed(traffic.uploadBps);
  uploadTotal.textContent = formatBytes(traffic.uploadBytes);
  trafficStats.hidden = false;
}

async function requestPublicIp(masked = false) {
  const reply = await window.vpnApi.publicIp(masked);
  const value = reply.result?.ip;
  const valid = masked ? /^\d{1,3}\.\d{1,3}\.\*{3}\.\*{3}$/.test(value) : isIPv4(value);
  if (!reply.ok || !valid) throw new Error(reply.error || "IP недоступен");
  return reply.result.ip;
}

async function loadDirectIp() {
  if (directIp) return directIp;
  if (!directIpPromise) {
    directIpPromise = requestPublicIp().then(ip => {
      if (currentState !== "disconnected") return "";
      directIp = ip;
      setIpText("Ваш IP", ip);
      return ip;
    }).catch(() => {
      if (currentState === "disconnected") setIpText("Ваш IP", "IP недоступен", "unavailable");
      return "";
    }).finally(() => { directIpPromise = null; });
  }
  return directIpPromise;
}

async function loadProtectedIp() {
  try {
    const ip = await requestPublicIp(true);
    if (currentState === "connected") setIpText("Защищённый IP", ip, "protected");
  } catch {
    if (currentState === "connected") setIpText("Защищённый IP", "IP скрыт", "protected");
  }
}

function applyState(state) {
  const previousState = currentState;
  currentState = state;
  dashboard.dataset.vpnState = state;
  statusText.textContent = STATUS_LABEL[state] ?? state;

  powerBtn.classList.remove("connected", "connecting");
  if (state === "connected") powerBtn.classList.add("connected");
  if (state === "connecting" || state === "disconnecting") powerBtn.classList.add("connecting");
  if (state !== "connected") clearConnectedLocation();

  if (state === "connecting") {
    stopConnectionTimer();
    resetTrafficDisplay();
    startIpEncryption();
  } else if (state === "connected") {
    stopIpEncryption();
    setIpText("Защищённый IP", "***.***.***.***", "protected");
    if (previousState !== "connected") {
      startConnectionTimer();
      resetTrafficDisplay(true);
    }
  } else if (state === "disconnecting") {
    startIpEncryption("Восстановление IP");
  } else if (state === "disconnected") {
    stopIpEncryption();
    stopConnectionTimer();
    resetTrafficDisplay();
    setIpText("Ваш IP", directIp || "Определяем…");
    if (!directIp && !directIpPromise) {
      window.setTimeout(() => {
        if (currentState === "disconnected") void loadDirectIp();
      }, 350);
    }
    if (pendingSubscriptionRefresh && !requestBusy) {
      window.setTimeout(() => {
        if (currentState === "disconnected" && pendingSubscriptionRefresh) void syncSubscription("automatic");
      }, 700);
    }
  }

  powerBtn.setAttribute("aria-pressed", String(state === "connected"));
  logsState.textContent = {
    disconnected: "Ядро запущено · VPN отключён",
    connecting: "Xray устанавливает соединение",
    connected: "Туннель активен",
    disconnecting: "Туннель закрывается",
  }[state] || "Диагностика активна";
  updatePingButton();
}

function updatePingButton() {
  powerBtn.disabled = requestBusy
    || currentState === "connecting"
    || currentState === "disconnecting"
    || (currentState === "disconnected" && !selectedGroupId);
  pingBtn.disabled = requestBusy || currentState !== "disconnected" || serverProfiles.length === 0;
  syncSubscriptionBtn.disabled = requestBusy || currentState !== "disconnected";
  changeSubscriptionBtn.disabled = requestBusy || currentState !== "disconnected";
  groupsMenuItem.disabled = requestBusy || currentState !== "disconnected";
}

function setMenuOpen(open) {
  menuOpen = open;
  appMenu.hidden = !open;
  menuBtn.setAttribute("aria-expanded", String(open));
  if (open) logsMenuItem.focus({ preventScroll: true });
}

function logClass(line) {
  const value = line.toLowerCase();
  if (value.includes("panic") || value.includes("error") || value.includes("failed") || value.includes("ошиб")) return "error";
  if (value.includes("warn") || value.includes("reject") || value.includes("timeout")) return "warning";
  if (value.includes("passed") || value.includes("ready") || value.includes("completed") || (value.includes("connected") && !value.includes("disconnected"))) return "success";
  if (value.includes("[bridge]")) return "bridge";
  return "";
}

function updateLogsCount() {
  logsCount.textContent = `${logLineCount} строк`;
}

function showEmptyLogs() {
  logsOutput.replaceChildren();
  logLineCount = 0;
  const empty = document.createElement("div");
  empty.className = "log-empty";
  empty.textContent = "Логи пока пусты\nПопробуйте подключиться к серверу";
  logsOutput.append(empty);
  updateLogsCount();
}

function appendLogLine(value) {
  const line = String(value || "").trimEnd();
  if (!line) return;
  logsOutput.querySelector(".log-empty")?.remove();
  const shouldFollow = logsOutput.scrollHeight - logsOutput.scrollTop - logsOutput.clientHeight < 48;
  const row = document.createElement("div");
  row.className = `log-line ${logClass(line)}`.trim();
  row.textContent = line;
  logsOutput.append(row);
  logLineCount += 1;
  while (logLineCount > 1500) {
    logsOutput.firstElementChild?.remove();
    logLineCount -= 1;
  }
  updateLogsCount();
  if (shouldFollow) logsOutput.scrollTop = logsOutput.scrollHeight;
}

async function openLogs() {
  setMenuOpen(false);
  logsOverlay.hidden = false;
  logsLoading = true;
  pendingLogLines = [];
  showEmptyLogs();
  closeLogsBtn.focus({ preventScroll: true });
  const reply = await window.vpnApi.getLogs();
  if (!logsOverlay.hidden) {
    logsOutput.replaceChildren();
    logLineCount = 0;
    if (reply.ok && Array.isArray(reply.result) && reply.result.length) {
      for (const line of reply.result) appendLogLine(line);
    } else if (!reply.ok) {
      appendLogLine(`[interface] Не удалось получить логи: ${reply.error}`);
    }
    for (const line of pendingLogLines) appendLogLine(line);
    if (!logsOutput.children.length) showEmptyLogs();
    logsOutput.scrollTop = logsOutput.scrollHeight;
  }
  pendingLogLines = [];
  logsLoading = false;
}

async function closeLogs() {
  if (logsOverlay.hidden) return;
  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (!reduceMotion) {
    await logsOverlay.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 150, easing: "ease-in" }).finished;
  }
  logsOverlay.hidden = true;
  menuBtn.focus({ preventScroll: true });
}

function updateDNSControl() {
  dnsSelect.value = selectedDNS;
  const provider = dnsProviders.find(item => item.id === selectedDNS) || dnsProviders[0];
  dnsDescription.textContent = `${provider.name} · ${provider.detail} · применяется внутри туннеля`;
  customDnsFields.hidden = selectedDNS !== "custom";
  if (selectedDNS === "custom") {
    const validation = parseCustomDNS(customDNSValue);
    customDnsError.textContent = customDNSValue ? validation.error : "";
  } else {
    customDnsError.textContent = "";
  }
}

function updateRoutingControl() {
  routingModeSelect.value = selectedRoutingMode;
  const mode = routingModes.find(item => item.id === selectedRoutingMode) || routingModes[0];
  routingModeDescription.textContent = mode.detail;
  routingDomainsFields.hidden = selectedRoutingMode === "full";
  routingGeoDataFields.hidden = selectedRoutingMode === "full";
  routingDomainsInput.value = routingDomainsValue;
  geoIPURLInput.value = geoIPURLValue;
  geoSiteURLInput.value = geoSiteURLValue;
  updateGeoDataValidation();
}

function updateGeoDataValidation() {
  if (selectedRoutingMode === "full") {
    geoDataError.textContent = "";
    geoIPURLInput.removeAttribute("aria-invalid");
    geoSiteURLInput.removeAttribute("aria-invalid");
    return { geoIPURL: "", geoSiteURL: "", error: "", field: "" };
  }
  const rules = routingDomainsValue.split(/\s+/).map(item => item.trim()).filter(Boolean);
  const validation = validateGeoDataSources(rules, geoIPURLValue, geoSiteURLValue);
  geoDataError.textContent = validation.error;
  geoIPURLInput.toggleAttribute("aria-invalid", validation.field === "geoip");
  geoSiteURLInput.toggleAttribute("aria-invalid", validation.field === "geosite");
  return validation;
}

async function loadDeviceInfo() {
  if (currentHWID) return;
  const reply = await window.vpnApi.deviceInfo();
  if (reply.ok && /^[0-9a-f]{64}$/i.test(reply.result?.hwid || "")) {
    currentHWID = reply.result.hwid;
    deviceHwid.textContent = currentHWID;
    deviceHwid.title = currentHWID;
    copyHwidBtn.disabled = false;
  } else {
    deviceHwid.textContent = "Недоступен";
    copyHwidBtn.disabled = true;
  }
}

async function loadAutoStart() {
  autoStartToggle.disabled = true;
  const reply = await window.vpnApi.getAutoStart();
  if (reply.ok) {
    autoStartToggle.checked = Boolean(reply.result);
    autoStartDescription.textContent = reply.result
      ? "ShadowVPN запускается после входа в Windows"
      : "Запускать ShadowVPN после входа в систему";
  } else {
    autoStartDescription.textContent = "Не удалось проверить автозапуск";
  }
  autoStartToggle.disabled = false;
}

function openSettings() {
  setMenuOpen(false);
  settingsOverlay.hidden = false;
  updateDNSControl();
  updateRoutingControl();
  void loadDeviceInfo();
  void loadAutoStart();
  dnsSelect.focus({ preventScroll: true });
}

async function closeSettings() {
  if (settingsOverlay.hidden) return;
  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (!reduceMotion) {
    await settingsOverlay.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 150, easing: "ease-in" }).finished;
  }
  settingsOverlay.hidden = true;
  menuBtn.focus({ preventScroll: true });
}

function persistGroups() {
  groupState = groupStore.writeState(groupState);
  renderServerList();
}

function currentEditedGroup() {
  return groupState.groups.find(group => group.id === editingGroupId) || null;
}

function renderGroupList() {
  groupsList.replaceChildren();
  for (const group of groupState.groups) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "group-list-item";
    button.classList.toggle("selected", group.id === editingGroupId);
    button.classList.toggle("active", group.id === groupState.activeGroupId);
    const name = document.createElement("strong");
    name.textContent = group.name;
    const count = groupStore.availableProfileIds(group, serverProfiles).length;
    const detail = document.createElement("small");
    detail.textContent = `${count} серверов${group.id === groupState.activeGroupId ? " · AUTO" : ""}`;
    button.append(name, detail);
    button.addEventListener("click", () => { editingGroupId = group.id; renderGroupsEditor(); });
    groupsList.append(button);
  }
}

function renderGroupHosts(group) {
  groupHosts.replaceChildren();
  const realProfiles = serverProfiles.filter(profile => !profile.auto);
  const selected = new Set(group.profileIds);
  for (const profile of realProfiles) {
    const display = serverFlagAndName(profile.name);
    const label = document.createElement("label");
    label.className = "group-host";
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.checked = selected.has(profile.id);
    const copy = document.createElement("span");
    copy.className = "group-host-copy";
    const name = document.createElement("strong");
    name.textContent = `${display.flag ? `${display.flag} ` : ""}${display.name}`;
    const meta = document.createElement("small");
    meta.textContent = `${profile.protocol.toUpperCase()} / ${profile.transport.toUpperCase()}`;
    copy.append(name, meta);
    label.append(checkbox, copy);
    checkbox.addEventListener("change", () => {
      const ids = new Set(group.profileIds);
      if (checkbox.checked) ids.add(profile.id); else ids.delete(profile.id);
      group.profileIds = groupStore.cleanProfileIds([...ids]);
      persistGroups();
      renderGroupsEditor();
    });
    groupHosts.append(label);
  }
  if (!realProfiles.length) {
    const empty = document.createElement("div");
    empty.className = "group-empty";
    empty.textContent = "В подписке пока нет серверов";
    groupHosts.append(empty);
  }
}

function renderGroupsEditor() {
  if (!currentEditedGroup() && groupState.groups.length) editingGroupId = groupState.groups[0].id;
  const group = currentEditedGroup();
  renderGroupList();
  groupEmpty.hidden = Boolean(group);
  groupFields.hidden = !group;
  if (!group) return;
  groupNameInput.value = group.name;
  const active = group.id === groupState.activeGroupId;
  activateGroupBtn.classList.toggle("active", active);
  activateGroupBtn.setAttribute("aria-pressed", String(active));
  groupAutoDescription.textContent = active ? "Активная группа Auto" : "Только выбранные серверы";
  const availableCount = groupStore.availableProfileIds(group, serverProfiles).length;
  groupHostCount.textContent = `${availableCount} выбрано`;
  renderGroupHosts(group);
}

function openGroups() {
  if (currentState !== "disconnected") return;
  setMenuOpen(false);
  groupsOverlay.hidden = false;
  renderGroupsEditor();
  (currentEditedGroup() ? groupNameInput : createGroupBtn).focus({ preventScroll: true });
}

async function closeGroups() {
  if (groupsOverlay.hidden) return;
  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (!reduceMotion) await groupsOverlay.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 150, easing: "ease-in" }).finished;
  groupsOverlay.hidden = true;
  menuBtn.focus({ preventScroll: true });
}

function createGroup() {
  const group = groupStore.makeGroup(groupState.groups, []);
  groupState.groups.push(group);
  if (!groupState.activeGroupId) groupState.activeGroupId = group.id;
  editingGroupId = group.id;
  persistGroups();
  renderGroupsEditor();
  groupNameInput.select();
}

function activeAutoProfileIds() {
  const group = groupStore.activeGroup(groupState);
  return group ? groupStore.availableProfileIds(group, serverProfiles) : [];
}

function isRussianRendererProfile(profile) {
  const display = serverFlagAndName(profile?.name);
  if (display.flagCode === "ru" || String(profile?.name || "").includes("🇷🇺")) return true;
  return String(display.name || "").normalize("NFKC").toLocaleLowerCase("ru-RU")
    .split(/\s+/)
    .map(part => part.replace(/^[^\p{L}]+|[^\p{L}]+$/gu, ""))
    .some(part => ["ru", "rus", "russia", "россия", "рф"].includes(part));
}

function autoProfilesFor(autoId) {
  const group = groupStore.activeGroup(groupState);
  const allowed = group ? new Set(groupStore.availableProfileIds(group, serverProfiles)) : null;
  return serverProfiles.filter(profile => !profile.auto
    && (!allowed || allowed.has(profile.id))
    && (autoId !== AUTO_NO_RU_PROFILE_ID || !isRussianRendererProfile(profile)));
}

function refreshAutoPingForGroup() {
  for (const autoId of [AUTO_PROFILE_ID, AUTO_NO_RU_PROFILE_ID]) {
    const values = autoProfilesFor(autoId).map(profile => pingResults.get(profile.id)).filter(result => result?.available);
    pingResults.set(autoId, values.length
      ? { id: autoId, available: true, latencyMs: Math.min(...values.map(result => result.latencyMs)) }
      : { id: autoId, available: false });
  }
}

/**
 * onStateChange (push) and getState() (poll) travel over unrelated IPC
 * channels with no ordering guarantee between them. If the user clicks
 * connect right after page load, a slow-to-resolve initial getState()
 * poll can arrive AFTER a push already reported "connected", silently
 * clobbering the UI back to a stale state. Once any push has arrived,
 * we trust pushes only and drop stale poll results.
 */
function applyPolledState(state) {
  if (receivedPush) return;
  applyState(state);
}

function applyPushedState(state) {
  receivedPush = true;
  applyState(state);
}

powerBtn.addEventListener("click", async () => {
  if (requestBusy || currentState === "connecting" || currentState === "disconnecting") return;
  if (!serverProfiles.length) { statusText.textContent = "Сначала добавьте подписку"; return; }
  const disconnecting = currentState === "connected";
  const selectedIsAuto = !disconnecting && [AUTO_PROFILE_ID, AUTO_NO_RU_PROFILE_ID].includes(selectedGroupId);
  const selectedAutoGroup = selectedIsAuto ? groupStore.activeGroup(groupState) : null;
  const selectedAutoProfileIds = selectedIsAuto ? autoProfilesFor(selectedGroupId).map(profile => profile.id) : [];
  if (selectedIsAuto && !selectedAutoProfileIds.length) {
    statusText.textContent = selectedAutoGroup
      ? `В группе «${selectedAutoGroup.name}» нет подходящих серверов`
      : "Для этого режима Auto нет подходящих серверов";
    if (selectedAutoGroup) openGroups();
    return;
  }
  let customDNSServers = [];
  if (!disconnecting && selectedDNS === "custom") {
    const validation = parseCustomDNS(customDNSValue);
    if (validation.error) {
      customDnsError.textContent = validation.error;
      statusText.textContent = validation.error;
      openSettings();
      customDnsInput.focus({ preventScroll: true });
      return;
    }
    customDNSServers = validation.servers;
  }
  const directDomains = routingDomainsValue.split(/\s+/).map(item => item.trim()).filter(Boolean);
  const geoData = disconnecting
    ? { geoIPURL: "", geoSiteURL: "", error: "", field: "" }
    : updateGeoDataValidation();
  if (!disconnecting && geoData.error) {
    statusText.textContent = geoData.error;
    openSettings();
    (geoData.field === "geosite" ? geoSiteURLInput : geoIPURLInput).focus({ preventScroll: true });
    return;
  }
  requestBusy = true; powerBtn.disabled = true;
  updatePingButton();
  try {
    if (!disconnecting && !directIp) {
      await Promise.race([
        loadDirectIp(),
        new Promise(resolve => window.setTimeout(resolve, 450)),
      ]);
    }
    const reply = disconnecting
      ? await window.vpnApi.disconnect()
      : await window.vpnApi.connect(selectedGroupId, selectedDNS, customDNSServers, fragmentationEnabled, killSwitchEnabled, selectedAutoProfileIds, selectedRoutingMode, directDomains, geoData.geoIPURL, geoData.geoSiteURL);
    if (!reply.ok) {
      statusText.textContent = reply.error;
    } else if (!disconnecting) {
      setConnectedLocation(reply.result);
      void loadProtectedIp();
    }
  } catch { statusText.textContent = "Не удалось связаться с ядром"; }
  finally {
    requestBusy = false;
    powerBtn.disabled = false;
    updatePingButton();
    if (disconnecting && currentState === "disconnected" && pendingSubscriptionRefresh) {
      window.setTimeout(() => void requestAutomaticSubscriptionUpdate(), 700);
    }
  }
});

async function runPingTest() {
  if (requestBusy || currentState !== "disconnected" || serverProfiles.length === 0) return;
  requestBusy = true;
  powerBtn.disabled = true;
  pingBtn.classList.add("testing");
  for (const profile of serverProfiles) pingResults.set(profile.id, "pending");
  renderServerList();
  updatePingButton();
  const pingLabel = selectedPingMethod === "tcp" ? "TCP" : `HTTP ${selectedPingMethod.toUpperCase()}`;
  statusText.textContent = `Проверяем ${pingLabel}-пинг серверов…`;
  try {
    const reply = await window.vpnApi.ping(selectedPingMethod);
    if (!reply.ok) throw new Error(reply.error);
    for (const result of reply.result) pingResults.set(result.id, result);
    refreshAutoPingForGroup();
    renderServerList();
    const realResults = reply.result.filter(result => !serverProfiles.find(profile => profile.id === result.id)?.auto);
    const available = realResults.filter(result => result.available).length;
    statusText.textContent = `${pingLabel}-пинг проверен: ${available} из ${realResults.length} доступны`;
  } catch (error) {
    for (const profile of serverProfiles) pingResults.delete(profile.id);
    renderServerList();
    statusText.textContent = error.message || "Не удалось проверить задержку";
  } finally {
    requestBusy = false;
    powerBtn.disabled = false;
    pingBtn.classList.remove("testing");
    updatePingButton();
  }
}

pingBtn.addEventListener("click", runPingTest);

window.vpnApi.onStateChange(applyPushedState);
window.vpnApi.onProfileChange(applyAutoProfileChange);
window.vpnApi.onTraffic(applyTraffic);
window.vpnApi.onLog(line => {
  const value = String(line);
  if (value.includes("Go core process exited")) {
    logsState.textContent = "Go/Xray завершил работу";
    logsLiveDot.classList.add("offline");
  } else if (value.includes("Go core process started")) {
    logsLiveDot.classList.remove("offline");
  }
  if (logsOverlay.hidden) return;
  if (logsLoading) pendingLogLines.push(line);
  else appendLogLine(line);
});
window.vpnApi.getState().then(applyPolledState);

updateCategoryControl();
renderServerList();
updatePingButton();

// The URL is retained locally; server credentials remain inside the Go process.
const welcomeScreen = document.getElementById("welcomeScreen");
const dashboard = document.getElementById("dashboard");
const subscriptionUrl = document.getElementById("subscriptionUrl");
const subscriptionError = document.getElementById("subscriptionError");
const subscriptionStorageKey = "shadowvpn.subscriptionUrl";
function autoUpdateIntervalMs() {
  return (autoUpdateIntervals.find(item => item.id === selectedAutoUpdate)?.minutes || 0) * 60000;
}
function clearAutoUpdateTimer() {
  if (autoUpdateTimer !== null) window.clearTimeout(autoUpdateTimer);
  autoUpdateTimer = null;
}
function scheduleSubscriptionUpdate(delayOverride) {
  clearAutoUpdateTimer();
  const interval = autoUpdateIntervalMs();
  if (!interval) return;
  const lastSync = readLastSubscriptionSync();
  const delay = Number.isFinite(delayOverride)
    ? Math.max(1000, delayOverride)
    : Math.max(1000, lastSync ? lastSync + interval - Date.now() : interval);
  autoUpdateTimer = window.setTimeout(() => {
    autoUpdateTimer = null;
    void requestAutomaticSubscriptionUpdate();
  }, delay);
}
async function requestAutomaticSubscriptionUpdate() {
  if (currentState !== "disconnected") {
    pendingSubscriptionRefresh = true;
    return;
  }
  if (requestBusy) {
    scheduleSubscriptionUpdate(60000);
    return;
  }
  await syncSubscription("automatic");
}
function subscriptionSyncSucceeded() {
  pendingSubscriptionRefresh = false;
  writeLastSubscriptionSync();
  scheduleSubscriptionUpdate();
}
function updateAutoUpdateControl() {
  autoUpdateSelect.value = selectedAutoUpdate;
  autoUpdateDescription.textContent = selectedAutoUpdate === "off"
    ? "Автоматическое обновление выключено"
    : "Работает в фоне · активный VPN не прерывается";
}
function updatePingMethodControl() {
  pingMethodSelect.value = selectedPingMethod;
  const method = pingMethods.find(item => item.id === selectedPingMethod) || pingMethods[0];
  pingMethodDescription.textContent = method.detail;
  const label = method.id === "tcp" ? "TCP-пинг" : `HTTP ${method.id.toUpperCase()}-пинг`;
  pingBtn.setAttribute("aria-label", `Измерить ${label} серверов`);
  pingBtn.title = `Измерить ${label}`;
}
function validSubscriptionUrl(value) {
  try {
    const url = new URL(value);
    return url.protocol === "https:" && Boolean(url.hostname) && !url.username && !url.password;
  } catch { return false; }
}
let screenTransitionRunning = false;
async function switchScreen(from, to, focusTarget) {
  if (screenTransitionRunning || !to.hidden) return;
  screenTransitionRunning = true;
  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  try {
    if (!reduceMotion) {
      await from.animate([
        { opacity: 1, transform: "translateY(0) scale(1)" },
        { opacity: 0, transform: "translateY(-8px) scale(.99)" },
      ], { duration: 180, easing: "cubic-bezier(.4,0,1,1)" }).finished;
    }
    from.hidden = true;
    to.hidden = false;
    if (!reduceMotion) {
      await to.animate([
        { opacity: 0, transform: "translateY(12px) scale(.99)" },
        { opacity: 1, transform: "translateY(0) scale(1)" },
      ], { duration: 420, easing: "cubic-bezier(.16,1,.3,1)" }).finished;
    }
    focusTarget.focus({ preventScroll: true });
  } finally { screenTransitionRunning = false; }
}
function openDashboard() {
  return switchScreen(welcomeScreen, dashboard, powerBtn);
}
async function importSubscription(trigger = "manual") {
  if (requestBusy) return;
  const value = subscriptionUrl.value.trim();
  if (!validSubscriptionUrl(value)) {
    subscriptionError.textContent = "Введите корректную HTTPS-ссылку на подписку";
    subscriptionUrl.setAttribute("aria-invalid", "true"); subscriptionUrl.focus(); return;
  }
  requestBusy = true;
  const button = document.querySelector(".subscription-submit");
  button.disabled = true; subscriptionUrl.disabled = true;
  button.textContent = "Загружаем серверы…"; subscriptionError.textContent = "";
  try {
    const reply = await window.vpnApi.importSubscription(value, trigger);
    if (!reply.ok) throw new Error(reply.error);
    const imported = Array.isArray(reply.result) ? { profiles: reply.result, skipped: 0 } : reply.result;
    serverProfiles = imported?.profiles || [];
    groupState = groupStore.writeState(groupState);
    pingResults.clear();
    if (!serverProfiles.length) throw new Error("В подписке нет серверов");
    selectedGroupId = serverProfiles[0].id;
    renderServerList();
    if (imported?.skipped > 0) statusText.textContent = `Подписка загружена · пропущено неподдерживаемых: ${imported.skipped}`;
    try { localStorage.setItem(subscriptionStorageKey, value); } catch { /* Import still works for this session. */ }
    subscriptionSyncSucceeded();
    await openDashboard();
    void loadDirectIp();
    if (trigger === "startup" && pingOnOpenEnabled) {
      window.setTimeout(() => void runPingTest(), 450);
    }
  } catch (e) { subscriptionError.textContent = e.message || "Ошибка импорта подписки"; }
  finally { requestBusy = false; button.disabled = false; subscriptionUrl.disabled = false; button.textContent = "Добавить подписку"; updatePingButton(); }
}

async function syncSubscription(trigger = "manual") {
  if (requestBusy || currentState !== "disconnected") return;
  let value = subscriptionUrl.value.trim();
  try { value = localStorage.getItem(subscriptionStorageKey) || value; } catch { /* Use the form value. */ }
  if (!validSubscriptionUrl(value)) {
    statusText.textContent = "Ссылка подписки не найдена";
    await switchScreen(dashboard, welcomeScreen, subscriptionUrl);
    return;
  }

  if (trigger === "manual") setMenuOpen(false);
  requestBusy = true;
  powerBtn.disabled = true;
  syncSubscriptionBtn.classList.add("syncing");
  updatePingButton();
  statusText.textContent = trigger === "automatic" ? "Автоматически обновляем подписку…" : "Синхронизируем подписку…";
  try {
    const reply = await window.vpnApi.importSubscription(value, trigger);
    if (!reply.ok) throw new Error(reply.error);
    const imported = Array.isArray(reply.result) ? { profiles: reply.result, skipped: 0 } : reply.result;
    if (!Array.isArray(imported?.profiles) || !imported.profiles.length) throw new Error("В подписке нет серверов");
    const previousSelection = selectedGroupId;
    serverProfiles = imported.profiles;
    groupState = groupStore.writeState(groupState);
    selectedGroupId = serverProfiles.some(profile => profile.id === previousSelection) ? previousSelection : serverProfiles[0].id;
    pingResults.clear();
    renderServerList();
    const serverCount = serverProfiles.filter(profile => !profile.auto).length;
    statusText.textContent = `Подписка синхронизирована · ${serverCount} серверов${imported.skipped ? ` · пропущено: ${imported.skipped}` : ""}`;
    subscriptionSyncSucceeded();
  } catch (error) {
    statusText.textContent = error.message || "Не удалось обновить подписку";
    scheduleSubscriptionUpdate(trigger === "automatic" ? 300000 : undefined);
  } finally {
    requestBusy = false;
    powerBtn.disabled = false;
    syncSubscriptionBtn.classList.remove("syncing");
    updatePingButton();
  }
}

document.getElementById("subscriptionForm").addEventListener("submit", event => { event.preventDefault(); void importSubscription("manual"); });
try {
  const saved = localStorage.getItem(subscriptionStorageKey);
  if (saved && validSubscriptionUrl(saved)) { subscriptionUrl.value = saved; void importSubscription("startup"); }
} catch { /* Storage may be unavailable. */ }
subscriptionUrl.addEventListener("input", () => {
  subscriptionError.textContent = "";
  subscriptionUrl.removeAttribute("aria-invalid");
});
menuBtn.addEventListener("click", event => {
  event.stopPropagation();
  if (!menuOpen) setSortMenuOpen(false);
  setMenuOpen(!menuOpen);
});
sortBtn.addEventListener("click", event => {
  event.stopPropagation();
  if (!sortMenuOpen) setMenuOpen(false);
  setSortMenuOpen(!sortMenuOpen);
});
sortMenu.addEventListener("click", event => {
  const item = event.target.closest("[data-sort-mode]");
  if (!item) return;
  selectedSort = sortModes.has(item.dataset.sortMode) ? item.dataset.sortMode : "subscription";
  try { localStorage.setItem(sortStorageKey, selectedSort); } catch { /* Keep it for this session. */ }
  setSortMenuOpen(false);
  updateSortControl();
  renderServerList();
  const hasResults = [...pingResults.values()].some(result => result && result !== "pending");
  if (selectedSort === "latency" && !hasResults) void runPingTest();
});
serverCategoryTabs.addEventListener("click", event => {
  const tab = event.target.closest("[data-server-category]");
  if (!tab || !categoryLabels.has(tab.dataset.serverCategory)) return;
  selectedCategory = tab.dataset.serverCategory;
  updateCategoryControl();
  renderServerList();
  updatePingButton();
  if (currentState === "disconnected" && !requestBusy) {
    statusText.textContent = selectedGroupId ? STATUS_LABEL.disconnected : "В этой категории пока нет серверов";
  }
});
logsMenuItem.addEventListener("click", openLogs);
syncSubscriptionBtn.addEventListener("click", () => { void syncSubscription("manual"); });
settingsMenuItem.addEventListener("click", openSettings);
groupsMenuItem.addEventListener("click", openGroups);
createGroupBtn.addEventListener("click", createGroup);
groupNameInput.addEventListener("input", () => {
  const group = currentEditedGroup();
  if (!group) return;
  group.name = groupNameInput.value.slice(0, 40);
  groupState = groupStore.writeState(groupState);
  renderGroupList();
  renderServerList();
});
groupNameInput.addEventListener("blur", () => {
  const group = currentEditedGroup();
  if (!group) return;
  group.name = groupStore.cleanName(group.name);
  persistGroups();
  renderGroupsEditor();
});
activateGroupBtn.addEventListener("click", () => {
  const group = currentEditedGroup();
  if (!group) return;
  groupState.activeGroupId = groupState.activeGroupId === group.id ? "" : group.id;
  persistGroups();
  refreshAutoPingForGroup();
  renderServerList();
  renderGroupsEditor();
});
deleteGroupBtn.addEventListener("click", () => {
  const group = currentEditedGroup();
  if (!group) return;
  groupState.groups = groupState.groups.filter(item => item.id !== group.id);
  if (groupState.activeGroupId === group.id) groupState.activeGroupId = "";
  editingGroupId = groupState.groups[0]?.id || "";
  persistGroups();
  refreshAutoPingForGroup();
  renderServerList();
  renderGroupsEditor();
});
groupSelectAllBtn.addEventListener("click", () => {
  const group = currentEditedGroup();
  if (!group) return;
  group.profileIds = serverProfiles.filter(profile => !profile.auto).map(profile => profile.id);
  persistGroups();
  renderGroupsEditor();
});
groupClearBtn.addEventListener("click", () => {
  const group = currentEditedGroup();
  if (!group) return;
  group.profileIds = [];
  persistGroups();
  renderGroupsEditor();
});
dnsSelect.addEventListener("change", () => {
  selectedDNS = writeDNS(dnsSelect.value);
  updateDNSControl();
  if (selectedDNS === "custom") customDnsInput.focus({ preventScroll: true });
});
customDnsInput.addEventListener("input", () => {
  customDNSValue = writeCustomDNS(customDnsInput.value);
  const validation = parseCustomDNS(customDNSValue);
  customDnsError.textContent = customDNSValue ? validation.error : "";
  customDnsInput.toggleAttribute("aria-invalid", Boolean(validation.error && customDNSValue));
});
fragmentationToggle.addEventListener("change", () => {
  fragmentationEnabled = writeFragmentation(fragmentationToggle.checked);
});
killSwitchToggle.addEventListener("change", () => {
  killSwitchEnabled = writeKillSwitch(killSwitchToggle.checked);
});
routingModeSelect.addEventListener("change", () => {
  selectedRoutingMode = writeRoutingMode(routingModeSelect.value);
  updateRoutingControl();
  if (selectedRoutingMode !== "full") routingDomainsInput.focus({ preventScroll: true });
});
routingDomainsInput.addEventListener("input", () => {
  routingDomainsValue = writeRoutingDomains(routingDomainsInput.value);
  updateGeoDataValidation();
});
geoIPURLInput.addEventListener("input", () => {
  geoIPURLValue = writeGeoIPURL(geoIPURLInput.value);
  updateGeoDataValidation();
});
geoSiteURLInput.addEventListener("input", () => {
  geoSiteURLValue = writeGeoSiteURL(geoSiteURLInput.value);
  updateGeoDataValidation();
});
autoStartToggle.addEventListener("change", async () => {
  const requested = autoStartToggle.checked;
  autoStartToggle.disabled = true;
  autoStartDescription.textContent = requested ? "Добавляем задачу запуска Windows…" : "Удаляем задачу запуска Windows…";
  const reply = await window.vpnApi.setAutoStart(requested);
  if (!reply.ok) {
    autoStartToggle.checked = !requested;
    autoStartDescription.textContent = reply.error || "Не удалось изменить автозапуск";
  } else {
    autoStartDescription.textContent = requested
      ? "ShadowVPN запускается после входа в Windows"
      : "Запускать ShadowVPN после входа в систему";
  }
  autoStartToggle.disabled = false;
});
pingOnOpenToggle.addEventListener("change", () => {
  pingOnOpenEnabled = writePingOnOpen(pingOnOpenToggle.checked);
});
autoUpdateSelect.addEventListener("change", () => {
  selectedAutoUpdate = writeAutoUpdate(autoUpdateSelect.value);
  pendingSubscriptionRefresh = false;
  updateAutoUpdateControl();
  scheduleSubscriptionUpdate();
});
pingMethodSelect.addEventListener("change", () => {
  selectedPingMethod = writePingMethod(pingMethodSelect.value);
  pingResults.clear();
  updatePingMethodControl();
  renderServerList();
});
copyHwidBtn.addEventListener("click", async () => {
  if (!currentHWID) return;
  try { await navigator.clipboard.writeText(currentHWID); }
  catch {
    const area = document.createElement("textarea");
    area.value = currentHWID;
    document.body.append(area);
    area.select();
    document.execCommand("copy");
    area.remove();
  }
  copyHwidBtn.textContent = "Скопировано";
  window.setTimeout(() => { copyHwidBtn.textContent = "Копировать"; }, 1200);
});
changeSubscriptionBtn.addEventListener("click", () => {
  if (requestBusy || currentState !== "disconnected") return;
  setMenuOpen(false);
  switchScreen(dashboard, welcomeScreen, subscriptionUrl);
});
closeLogsBtn.addEventListener("click", closeLogs);
logsOverlay.addEventListener("click", event => { if (event.target === logsOverlay) closeLogs(); });
closeSettingsBtn.addEventListener("click", closeSettings);
settingsOverlay.addEventListener("click", event => { if (event.target === settingsOverlay) closeSettings(); });
closeGroupsBtn.addEventListener("click", closeGroups);
groupsOverlay.addEventListener("click", event => { if (event.target === groupsOverlay) closeGroups(); });
copyLogsBtn.addEventListener("click", async () => {
  const text = [...logsOutput.querySelectorAll(".log-line")].map(row => row.textContent).join("\n");
  if (!text) return;
  try {
    await navigator.clipboard.writeText(text);
    copyLogsBtn.textContent = "Скопировано";
  } catch {
    const area = document.createElement("textarea");
    area.value = text;
    document.body.append(area);
    area.select();
    document.execCommand("copy");
    area.remove();
    copyLogsBtn.textContent = "Скопировано";
  }
  window.setTimeout(() => { copyLogsBtn.textContent = "Копировать"; }, 1200);
});
clearLogsBtn.addEventListener("click", async () => {
  const reply = await window.vpnApi.clearLogs();
  if (reply.ok) showEmptyLogs();
  else appendLogLine(`[interface] Не удалось очистить логи: ${reply.error}`);
});
document.addEventListener("click", event => {
  if (menuOpen && !event.target.closest(".menu-anchor")) setMenuOpen(false);
  if (sortMenuOpen && !event.target.closest(".sort-anchor")) setSortMenuOpen(false);
});
document.addEventListener("keydown", event => {
  if (event.key !== "Escape") return;
  if (!settingsOverlay.hidden) closeSettings();
  else if (!groupsOverlay.hidden) closeGroups();
  else if (!logsOverlay.hidden) closeLogs();
  else if (menuOpen) setMenuOpen(false);
  else if (sortMenuOpen) setSortMenuOpen(false);
});
document.getElementById("subscriptionHelp").addEventListener("click", () => {
  const hint = document.getElementById("subscriptionHint");
  hint.hidden = !hint.hidden;
});

for (const provider of dnsProviders) {
  const option = document.createElement("option");
  option.value = provider.id;
  option.textContent = `${provider.name} · ${provider.detail}`;
  dnsSelect.append(option);
}
for (const interval of autoUpdateIntervals) {
  const option = document.createElement("option");
  option.value = interval.id;
  option.textContent = interval.name;
  autoUpdateSelect.append(option);
}
for (const method of pingMethods) {
  const option = document.createElement("option");
  option.value = method.id;
  option.textContent = method.name;
  pingMethodSelect.append(option);
}
for (const mode of routingModes) {
  const option = document.createElement("option");
  option.value = mode.id;
  option.textContent = mode.name;
  routingModeSelect.append(option);
}
customDnsInput.value = customDNSValue;
selectedRoutingMode = readRoutingMode();
routingDomainsValue = readRoutingDomains();
geoIPURLValue = readGeoIPURL();
geoSiteURLValue = readGeoSiteURL();
fragmentationToggle.checked = fragmentationEnabled;
killSwitchToggle.checked = killSwitchEnabled;
pingOnOpenToggle.checked = pingOnOpenEnabled;
dashboard.dataset.vpnState = currentState;
updateDNSControl();
updateRoutingControl();
updateAutoUpdateControl();
updatePingMethodControl();
updateSortControl();

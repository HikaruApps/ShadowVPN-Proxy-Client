let serverProfiles = [];
let requestBusy = false;

const powerBtn = document.getElementById("powerBtn");
const statusText = document.getElementById("statusText");
const serverList = document.getElementById("serverList");
const pingBtn = document.getElementById("pingBtn");
const sortBtn = document.getElementById("sortBtn");
const sortMenu = document.getElementById("sortMenu");
const menuBtn = document.getElementById("menuBtn");
const appMenu = document.getElementById("appMenu");
const logsMenuItem = document.getElementById("logsMenuItem");
const syncSubscriptionBtn = document.getElementById("syncSubscriptionBtn");
const settingsMenuItem = document.getElementById("settingsMenuItem");
const changeSubscriptionBtn = document.getElementById("changeSubscriptionBtn");
const settingsOverlay = document.getElementById("settingsOverlay");
const closeSettingsBtn = document.getElementById("closeSettingsBtn");
const dnsSelect = document.getElementById("dnsSelect");
const dnsDescription = document.getElementById("dnsDescription");
const customDnsFields = document.getElementById("customDnsFields");
const customDnsInput = document.getElementById("customDnsInput");
const customDnsError = document.getElementById("customDnsError");
const fragmentationToggle = document.getElementById("fragmentationToggle");
const killSwitchToggle = document.getElementById("killSwitchToggle");
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

let selectedGroupId = "auto";
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
const { serverFlagAndName, sortServerProfiles } = window.shadowVpnDisplay;
const { dnsProviders, readDNS, writeDNS, readCustomDNS, writeCustomDNS, readFragmentation, writeFragmentation, readKillSwitch, writeKillSwitch, parseCustomDNS } = window.shadowVpnSettings;
let selectedDNS = readDNS();
let customDNSValue = readCustomDNS();
let fragmentationEnabled = readFragmentation();
let killSwitchEnabled = readKillSwitch();
const sortStorageKey = "shadowvpn.serverSort";
const sortModes = new Set(["alphabetical", "subscription", "latency"]);
let selectedSort = "subscription";
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
  for (const profile of sortServerProfiles(serverProfiles, pingResults, selectedSort)) {
    const card = document.createElement("button");
    card.type = "button";
    card.className = "server-card";
    card.dataset.groupId = profile.id;
    card.classList.toggle("selected", profile.id === selectedGroupId);
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
      ? "САМЫЙ БЫСТРЫЙ СЕРВЕР"
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
  statusText.textContent = STATUS_LABEL[state] ?? state;

  powerBtn.classList.remove("connected", "connecting");
  if (state === "connected") powerBtn.classList.add("connected");
  if (state === "connecting" || state === "disconnecting") powerBtn.classList.add("connecting");
  if (state !== "connected") clearConnectedLocation();

  if (state === "connecting") {
    stopConnectionTimer();
    startIpEncryption();
  } else if (state === "connected") {
    stopIpEncryption();
    setIpText("Защищённый IP", "***.***.***.***", "protected");
    if (previousState !== "connected") startConnectionTimer();
  } else if (state === "disconnecting") {
    startIpEncryption("Восстановление IP");
  } else if (state === "disconnected") {
    stopIpEncryption();
    stopConnectionTimer();
    setIpText("Ваш IP", directIp || "Определяем…");
    if (!directIp && !directIpPromise) {
      window.setTimeout(() => {
        if (currentState === "disconnected") void loadDirectIp();
      }, 350);
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
  pingBtn.disabled = requestBusy || currentState !== "disconnected" || serverProfiles.length === 0;
  syncSubscriptionBtn.disabled = requestBusy || currentState !== "disconnected";
  changeSubscriptionBtn.disabled = requestBusy || currentState !== "disconnected";
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

function openSettings() {
  setMenuOpen(false);
  settingsOverlay.hidden = false;
  updateDNSControl();
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
      : await window.vpnApi.connect(selectedGroupId, selectedDNS, customDNSServers, fragmentationEnabled, killSwitchEnabled);
    if (!reply.ok) {
      statusText.textContent = reply.error;
    } else if (!disconnecting) {
      setConnectedLocation(reply.result);
      void loadProtectedIp();
    }
  } catch { statusText.textContent = "Не удалось связаться с ядром"; }
  finally { requestBusy = false; powerBtn.disabled = false; updatePingButton(); }
});

async function runPingTest() {
  if (requestBusy || currentState !== "disconnected" || serverProfiles.length === 0) return;
  requestBusy = true;
  powerBtn.disabled = true;
  pingBtn.classList.add("testing");
  for (const profile of serverProfiles) pingResults.set(profile.id, "pending");
  renderServerList();
  updatePingButton();
  statusText.textContent = "Проверяем TCP-пинг серверов…";
  try {
    const reply = await window.vpnApi.ping();
    if (!reply.ok) throw new Error(reply.error);
    for (const result of reply.result) pingResults.set(result.id, result);
    renderServerList();
    const realResults = reply.result.filter(result => !serverProfiles.find(profile => profile.id === result.id)?.auto);
    const available = realResults.filter(result => result.available).length;
    statusText.textContent = `TCP-пинг проверен: ${available} из ${realResults.length} доступны`;
  } catch (error) {
    for (const profile of serverProfiles) pingResults.delete(profile.id);
    renderServerList();
    statusText.textContent = error.message || "Не удалось проверить TCP-пинг";
  } finally {
    requestBusy = false;
    powerBtn.disabled = false;
    pingBtn.classList.remove("testing");
    updatePingButton();
  }
}

pingBtn.addEventListener("click", runPingTest);

window.vpnApi.onStateChange(applyPushedState);
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

renderServerList();
updatePingButton();

// The URL is retained locally; server credentials remain inside the Go process.
const welcomeScreen = document.getElementById("welcomeScreen");
const dashboard = document.getElementById("dashboard");
const subscriptionUrl = document.getElementById("subscriptionUrl");
const subscriptionError = document.getElementById("subscriptionError");
const subscriptionStorageKey = "shadowvpn.subscriptionUrl";
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
async function importSubscription() {
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
    const reply = await window.vpnApi.importSubscription(value);
    if (!reply.ok) throw new Error(reply.error);
    serverProfiles = reply.result;
    pingResults.clear();
    if (!serverProfiles.length) throw new Error("В подписке нет серверов");
    selectedGroupId = serverProfiles[0].id;
    renderServerList();
    try { localStorage.setItem(subscriptionStorageKey, value); } catch { /* Import still works for this session. */ }
    await openDashboard();
    void loadDirectIp();
  } catch (e) { subscriptionError.textContent = e.message || "Ошибка импорта подписки"; }
  finally { requestBusy = false; button.disabled = false; subscriptionUrl.disabled = false; button.textContent = "Добавить подписку"; updatePingButton(); }
}

async function syncSubscription() {
  if (requestBusy || currentState !== "disconnected") return;
  let value = subscriptionUrl.value.trim();
  try { value = localStorage.getItem(subscriptionStorageKey) || value; } catch { /* Use the form value. */ }
  if (!validSubscriptionUrl(value)) {
    statusText.textContent = "Ссылка подписки не найдена";
    await switchScreen(dashboard, welcomeScreen, subscriptionUrl);
    return;
  }

  setMenuOpen(false);
  requestBusy = true;
  powerBtn.disabled = true;
  syncSubscriptionBtn.classList.add("syncing");
  updatePingButton();
  statusText.textContent = "Синхронизируем подписку…";
  try {
    const reply = await window.vpnApi.importSubscription(value);
    if (!reply.ok) throw new Error(reply.error);
    if (!Array.isArray(reply.result) || !reply.result.length) throw new Error("В подписке нет серверов");
    const previousSelection = selectedGroupId;
    serverProfiles = reply.result;
    selectedGroupId = serverProfiles.some(profile => profile.id === previousSelection) ? previousSelection : serverProfiles[0].id;
    pingResults.clear();
    renderServerList();
    const serverCount = serverProfiles.filter(profile => !profile.auto).length;
    statusText.textContent = `Подписка синхронизирована · ${serverCount} серверов`;
  } catch (error) {
    statusText.textContent = error.message || "Не удалось обновить подписку";
  } finally {
    requestBusy = false;
    powerBtn.disabled = false;
    syncSubscriptionBtn.classList.remove("syncing");
    updatePingButton();
  }
}

document.getElementById("subscriptionForm").addEventListener("submit", event => { event.preventDefault(); importSubscription(); });
try {
  const saved = localStorage.getItem(subscriptionStorageKey);
  if (saved && validSubscriptionUrl(saved)) { subscriptionUrl.value = saved; importSubscription(); }
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
logsMenuItem.addEventListener("click", openLogs);
syncSubscriptionBtn.addEventListener("click", syncSubscription);
settingsMenuItem.addEventListener("click", openSettings);
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
changeSubscriptionBtn.addEventListener("click", () => {
  if (requestBusy || currentState !== "disconnected") return;
  setMenuOpen(false);
  switchScreen(dashboard, welcomeScreen, subscriptionUrl);
});
closeLogsBtn.addEventListener("click", closeLogs);
logsOverlay.addEventListener("click", event => { if (event.target === logsOverlay) closeLogs(); });
closeSettingsBtn.addEventListener("click", closeSettings);
settingsOverlay.addEventListener("click", event => { if (event.target === settingsOverlay) closeSettings(); });
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
customDnsInput.value = customDNSValue;
fragmentationToggle.checked = fragmentationEnabled;
killSwitchToggle.checked = killSwitchEnabled;
updateDNSControl();
updateSortControl();

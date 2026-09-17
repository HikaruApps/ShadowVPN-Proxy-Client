// Formatting and validation for live TUN traffic counters.
(() => {
  const BYTE_UNITS = ["Б", "КБ", "МБ", "ГБ", "ТБ"];

  function safeCounter(value) {
    const number = Number(value);
    if (!Number.isFinite(number) || number <= 0) return 0;
    return Math.min(Math.floor(number), Number.MAX_SAFE_INTEGER);
  }

  function normalizeTraffic(value = {}) {
    const source = value && typeof value === "object" ? value : {};
    return {
      uploadBytes: safeCounter(source.uploadBytes),
      downloadBytes: safeCounter(source.downloadBytes),
      uploadBps: safeCounter(source.uploadBps),
      downloadBps: safeCounter(source.downloadBps),
    };
  }

  function formatBytes(value) {
    let amount = safeCounter(value);
    let unit = 0;
    while (amount >= 1024 && unit < BYTE_UNITS.length - 1) {
      amount /= 1024;
      unit += 1;
    }
    const digits = unit === 0 || amount >= 100 ? 0 : 1;
    return `${amount.toFixed(digits).replace(".", ",")} ${BYTE_UNITS[unit]}`;
  }

  function formatSpeed(value) {
    return `${formatBytes(value)}/с`;
  }

  window.shadowVpnTraffic = Object.freeze({ normalizeTraffic, formatBytes, formatSpeed });
})();

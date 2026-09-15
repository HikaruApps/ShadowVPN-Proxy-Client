(() => {
  const segmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });

  function regionalFlagCode(value) {
    const points = [...String(value)].map(character => character.codePointAt(0));
    if (points.length !== 2 || points.some(point => point < 0x1f1e6 || point > 0x1f1ff)) return "";
    return points.map(point => String.fromCharCode(97 + point - 0x1f1e6)).join("");
  }

  function serverFlagAndName(value) {
    const original = String(value || "Сервер").trim();
    const segments = [...segmenter.segment(original)];
    const flag = segments.find(({ segment }) => {
      const regionalSymbols = segment.match(/\p{Regional_Indicator}/gu) || [];
      return regionalSymbols.length === 2 || /[🏴🏳]/u.test(segment);
    });
    if (!flag) return { flag: "🌐", flagCode: "", name: original };
    const name = `${original.slice(0, flag.index)}${original.slice(flag.index + flag.segment.length)}`
      .replace(/\s{2,}/g, " ")
      .trim();
    return { flag: flag.segment, flagCode: regionalFlagCode(flag.segment), name: name || "Сервер" };
  }

  function sortServerProfiles(profiles, pingResults, mode = "subscription") {
    return profiles.map((profile, index) => ({ profile, index })).sort((left, right) => {
      if (left.profile.auto !== right.profile.auto) return left.profile.auto ? -1 : 1;
      if (left.profile.auto) return left.index - right.index;
      if (mode === "alphabetical") {
        const leftName = serverFlagAndName(left.profile.name).name;
        const rightName = serverFlagAndName(right.profile.name).name;
        const compared = leftName.localeCompare(rightName, ["ru", "en"], { sensitivity: "base", numeric: true });
        return compared || left.index - right.index;
      }
      if (mode !== "latency") return left.index - right.index;
      const leftPing = pingResults.get(left.profile.id);
      const rightPing = pingResults.get(right.profile.id);
      const leftAvailable = Boolean(leftPing && leftPing !== "pending" && leftPing.available);
      const rightAvailable = Boolean(rightPing && rightPing !== "pending" && rightPing.available);
      if (leftAvailable !== rightAvailable) return leftAvailable ? -1 : 1;
      if (leftAvailable && leftPing.latencyMs !== rightPing.latencyMs) return leftPing.latencyMs - rightPing.latencyMs;
      return left.index - right.index;
    }).map(item => item.profile);
  }

  function serverCategoriesForProfile(profile) {
    if (profile?.auto) return ["all"];
    const name = serverFlagAndName(profile?.name).name.normalize("NFKC").toLocaleLowerCase("en-US");
    const categories = ["all"];
    if (name.includes("hysteria") || /(^|[^a-z0-9])ws([^a-z0-9]|$)/i.test(name)) categories.push("fast");
    if (name.includes("torrent")) categories.push("p2p");
    if (name.includes("gemini")) categories.push("gemini");
    if (name.includes("warp")) categories.push("warp");
    if (/\bno[\s_-]+tls\b/i.test(name)) categories.push("no-tls");
    return categories;
  }

  function filterServerProfiles(profiles, category = "all") {
    return profiles.filter(profile => serverCategoriesForProfile(profile).includes(category));
  }

  window.shadowVpnDisplay = { regionalFlagCode, serverFlagAndName, sortServerProfiles, serverCategoriesForProfile, filterServerProfiles };
})();

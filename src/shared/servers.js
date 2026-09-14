/**
 * Same protocol-agnostic contract as the Android skeleton's types/vpn.ts.
 * Kept deliberately plain-JS here since the renderer has no build step;
 * if this grows, promote it to TS with a bundler (esbuild/vite).
 */

// Demo data — in the real app this comes from a subscription/import flow.
const serverProfiles = [
  {
    id: "pl-raw",
    groupId: "pl",
    groupDisplayName: "🇵🇱 Польша",
    variantLabel: "Обычный",
    address: "203.0.113.10",
    port: 443,
    protocol: "vless-reality",
    transport: "tcp",
  },
  {
    id: "pl-grpc",
    groupId: "pl",
    groupDisplayName: "🇵🇱 Польша",
    variantLabel: "Для нестабильного канала",
    address: "203.0.113.10",
    port: 8443,
    protocol: "vless-reality",
    transport: "grpc",
  },
  {
    id: "de-raw",
    groupId: "de",
    groupDisplayName: "🇩🇪 Германия",
    variantLabel: "Обычный",
    address: "203.0.113.20",
    port: 443,
    protocol: "vless-reality",
    transport: "tcp",
  },
];

/** Groups by explicit groupId; ungrouped profiles become single-item groups. */
function groupServerProfiles(profiles) {
  const groups = new Map();
  for (const profile of profiles) {
    const key = profile.groupId ?? profile.id;
    if (groups.has(key)) {
      groups.get(key).profiles.push(profile);
    } else {
      groups.set(key, {
        id: key,
        displayName: profile.groupDisplayName ?? profile.id,
        profiles: [profile],
      });
    }
  }
  return Array.from(groups.values());
}

function protocolLabel(profile) {
  const proto = profile.protocol.replace("vless-", "VLESS / ").toUpperCase();
  return proto;
}

module.exports = { serverProfiles, groupServerProfiles, protocolLabel };

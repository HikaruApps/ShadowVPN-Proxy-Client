package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xtls/xray-core/core"
)

func TestRegexpAndCyrillicRoutingRules(t *testing.T) {
	routing, err := newRoutingOptions("bypass", []string{"regexp:.*\\.ru$\nregexp:.*\\.рф$\nregexp:.*\\.su$\nregexp:^[a-z]{2,3}\\.example$\nкремль.рф, full:почта.рф"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"regexp:.*\\.ru$", "regexp:.*\\.xn--p1ai$", "regexp:.*\\.su$", "regexp:^[a-z]{2,3}\\.example$", "domain:xn--e1ajeds9e.xn--p1ai", "full:xn--80a1acny.xn--p1ai"}
	if strings.Join(routing.DirectDomains, "|") != strings.Join(want, "|") {
		t.Fatalf("rules = %q, want %q", routing.DirectDomains, want)
	}
	profile, err := parseURI(sample)
	if err != nil {
		t.Fatal(err)
	}
	config, err := makeConfigWithRoutingOptions(profile, "cloudflare", nil, false, "auto", routing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = core.LoadConfig("json", bytes.NewReader(config)); err != nil {
		t.Fatalf("Xray rejected regexp routing: %v", err)
	}
	for _, bad := range []string{"regexp:.*(\\.ru$", "example\\.com", "full:a\\b.com"} {
		if _, err := newRoutingOptions("bypass", []string{bad}, "", ""); err == nil {
			t.Fatalf("invalid rule %q was accepted", bad)
		}
	}
}

// Shape of a Remnawave "Xray JSON" subscription: full client configs with
// remarks, local inbounds, routing, and helper outbounds around "proxy".
const remnawaveJSON = `[
  {
    "remarks": "🇩🇪 Germany",
    "inbounds": [{"tag": "socks", "port": 10808, "listen": "0.0.0.0", "protocol": "socks"}],
    "outbounds": [
      {"tag": "direct", "protocol": "freedom"},
      {
        "tag": "proxy", "protocol": "vless",
        "settings": {"address": "de.example.com", "port": 443, "id": "11111111-1111-1111-1111-111111111111", "encryption": "none", "flow": "xtls-rprx-vision", "reverse": {"tag": "evil"}},
        "streamSettings": {"network": "tcp", "security": "reality",
          "realitySettings": {"serverName": "cover.example", "fingerprint": "chrome", "publicKey": "Q0_5QvIs5D-yaDgUc-ocHBQK7Hf1kKkAd6xaxtTD1ns", "shortId": "abcd"},
          "sockopt": {"interface": "eth9", "dialerProxy": "direct"}}
      },
      {"tag": "block", "protocol": "blackhole"}
    ],
    "routing": {"rules": [{"type": "field", "outboundTag": "direct", "ip": ["0.0.0.0/0"]}]}
  },
  {
    "remarks": "🇳🇱 Hysteria",
    "outbounds": [{
      "tag": "proxy", "protocol": "hysteria",
      "settings": {"version": 2, "address": "nl.example.com", "port": 8443},
      "streamSettings": {"network": "hysteria", "security": "tls",
        "tlsSettings": {"serverName": "nl.example.com", "alpn": ["h3"]},
        "hysteriaSettings": {"version": 2, "auth": "secret"},
        "finalmask": {"udp": [{"type": "salamander", "settings": {"password": "obfs-secret"}}], "tcp": [{"type": "fragment", "settings": {"packets": "1-3"}}]}}
    }]
  },
  {"remarks": "WireGuard", "outbounds": [{"tag": "proxy", "protocol": "wireguard", "settings": {}}]}
]`

func TestRemnawaveXrayJSONSubscription(t *testing.T) {
	skipped := 0
	profiles, err := parseSubscriptionWithStats([]byte(remnawaveJSON), &skipped)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || skipped != 1 {
		t.Fatalf("profiles=%d skipped=%d", len(profiles), skipped)
	}
	vless, hysteria := profiles[0], profiles[1]
	if vless.Name != "🇩🇪 Germany" || vless.Protocol != "vless" || vless.Address != "de.example.com" || vless.Port != 443 {
		t.Fatalf("wrong VLESS profile: %#v", vless)
	}
	if hysteria.Address != "nl.example.com" || hysteria.Port != 8443 || hysteria.Transport != "hysteria" {
		t.Fatalf("wrong Hysteria profile: %#v", hysteria)
	}
	for _, profile := range profiles {
		config, err := makeConfigWithOptions(profile, "cloudflare", nil, true, "auto")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = core.LoadConfig("json", bytes.NewReader(config)); err != nil {
			t.Fatalf("Xray rejected %s: %v", profile.Name, err)
		}
		text := string(config)
		if strings.Contains(text, "eth9") || strings.Contains(text, "dialerProxy") || strings.Contains(text, "evil") || strings.Contains(text, "10808") {
			t.Fatalf("untrusted subscription settings leaked into %s: %s", profile.Name, text)
		}
	}
	config, _ := makeConfigWithOptions(hysteria, "cloudflare", nil, false, "auto")
	if text := string(config); !strings.Contains(text, `"salamander"`) || !strings.Contains(text, "obfs-secret") || strings.Contains(text, `"packets":"1-3"`) {
		t.Fatalf("Hysteria obfs not imported correctly: %s", text)
	}
	resolved, err := profileWithEndpoint(vless, "203.0.113.7", "de.example.com")
	if err != nil || resolved.Address != "203.0.113.7" {
		t.Fatalf("flat endpoint not replaced: %#v err=%v", resolved, err)
	}
}

func TestHysteria2SalamanderURI(t *testing.T) {
	profile, err := parseURI("hysteria2://secret@example.com:443?sni=example.com&obfs=salamander&obfs-password=obfs-secret#Obfs")
	if err != nil {
		t.Fatal(err)
	}
	config, err := makeConfig(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = core.LoadConfig("json", bytes.NewReader(config)); err != nil {
		t.Fatalf("Xray rejected salamander config: %v", err)
	}
	if text := string(config); !strings.Contains(text, `"type":"salamander"`) || !strings.Contains(text, "obfs-secret") {
		t.Fatalf("salamander missing: %s", text)
	}
	if _, err := parseURI("hy2://secret@example.com:443?obfs=salamander#NoPassword"); err == nil {
		t.Fatal("salamander without password accepted")
	}
	if _, err := parseURI("hy2://secret@example.com:443?obfs=gecko-unknown&obfs-password=x#Bad"); err == nil {
		t.Fatal("unknown obfs accepted")
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
)

const sample = "vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&sni=example.com&type=xhttp&path=%2Fapi&mode=stream-up#Poland"

func TestSubscriptionFormats(t *testing.T) {
	for _, raw := range []string{sample, base64.StdEncoding.EncodeToString([]byte(sample)), base64.RawURLEncoding.EncodeToString([]byte(sample))} {
		ps, e := parseSubscription([]byte(raw))
		if e != nil || len(ps) != 1 {
			t.Fatalf("parse: %v", e)
		}
		if ps[0].Name != "Poland" || ps[0].Transport != "xhttp" {
			t.Fatal("metadata")
		}
		b, e := makeConfig(ps[0])
		if e != nil {
			t.Fatal(e)
		}
		if _, e = core.LoadConfig("json", bytes.NewReader(b)); e != nil {
			t.Fatalf("Xray rejected config: %v", e)
		}
		public, _ := json.Marshal(ps)
		if strings.Contains(string(public), "00000000-") || strings.Contains(string(public), "example.com") {
			t.Fatal("credentials exposed")
		}
	}
}
func TestJSONIgnoresUntrustedSystemConfiguration(t *testing.T) {
	p, _ := parseURI(sample)
	input := map[string]any{"remarks": "<img src=x>", "inbounds": []any{map[string]any{"port": 80}}, "routing": map[string]any{}, "outbounds": []any{p.Outbound}}
	p.Outbound["sendThrough"] = "127.0.0.1"
	p.Outbound["streamSettings"].(map[string]any)["sockopt"] = map[string]any{"interface": "evil"}
	p.Outbound["streamSettings"].(map[string]any)["finalmask"] = map[string]any{"tcp": []any{map[string]any{"type": "untrusted"}}}
	b, _ := json.Marshal(input)
	ps, e := parseSubscription(b)
	if e != nil {
		t.Fatal(e)
	}
	config, _ := makeConfig(ps[0])
	if strings.Contains(string(config), "evil") || strings.Contains(string(config), "sendThrough") || strings.Contains(string(config), "untrusted") {
		t.Fatal("system overrides imported")
	}
	if !strings.Contains(string(config), "autoOutboundsInterface") {
		t.Fatal("loop avoidance missing")
	}
}
func TestBadSubscriptions(t *testing.T) {
	for _, raw := range []string{"<html>login</html>", "[]", "vless://bad", strings.Replace(sample, ":443", ":99999", 1), strings.Replace(sample, "xhttp", "unknown", 1)} {
		if _, e := parseSubscription([]byte(raw)); e == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
func TestDuplicateProfiles(t *testing.T) {
	ps, e := parseSubscription([]byte(sample + "\n" + sample))
	if e != nil || len(ps) != 1 {
		t.Fatal("deduplication", e)
	}
}

func TestMixedURISubscriptionSkipsUnsupportedProtocols(t *testing.T) {
	skipped := 0
	profiles, err := parseSubscriptionWithStats([]byte("ss://ignored@example.com:443#Legacy\n"+sample+"\nhysteria2://secret@example.com:443?sni=cover.example&alpn=h3#Fast"), &skipped)
	if err != nil || len(profiles) != 2 || profiles[0].Name != "Poland" || profiles[1].Protocol != "hysteria" || skipped != 1 {
		t.Fatalf("mixed subscription was not filtered: profiles=%#v skipped=%d err=%v", profiles, skipped, err)
	}
	if _, err := parseSubscription([]byte("ss://ignored@example.com:443#Legacy")); err == nil {
		t.Fatal("unsupported-only subscription was accepted")
	}
}

func TestHysteria2URIProducesXrayConfig(t *testing.T) {
	profile, err := parseURI("hy2://secret@example.com:443?sni=cover.example&alpn=h3&pinSHA256=e8e2d387fdbffeb38e9c9065cf30a97ee23c0e3d32ee6f78ffae40966befccc9#Hysteria")
	if err != nil || profile.Protocol != "hysteria" || profile.Transport != "hysteria" || profile.Address != "example.com" {
		t.Fatalf("wrong Hysteria profile: %#v err=%v", profile, err)
	}
	config, err := makeConfig(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = core.LoadConfig("json", bytes.NewReader(config)); err != nil {
		t.Fatalf("Xray rejected Hysteria config: %v", err)
	}
	text := string(config)
	if !strings.Contains(text, `"protocol":"hysteria"`) || !strings.Contains(text, `"auth":"secret"`) || !strings.Contains(text, `"network":"hysteria"`) || !strings.Contains(text, `"pinnedPeerCertSha256"`) {
		t.Fatalf("Hysteria config incomplete: %s", text)
	}
}

func TestHysteria2RejectsRemovedAllowInsecureMode(t *testing.T) {
	if _, err := parseURI("hysteria2://secret@example.com:443?insecure=1#Legacy"); err == nil || !strings.Contains(err.Error(), "pinSHA256") {
		t.Fatalf("Hysteria insecure profile was not rejected safely: %v", err)
	}
}

func TestDomainRoutingModesProduceSafeRules(t *testing.T) {
	profile, err := parseURI(sample)
	if err != nil {
		t.Fatal(err)
	}
	routing, err := newRoutingOptions("bypass", []string{" example.com ", "full:private.example.com", "keyword:chat"})
	if err != nil {
		t.Fatal(err)
	}
	config, err := makeConfigWithRoutingOptions(profile, "cloudflare", nil, false, "auto", routing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = core.LoadConfig("json", bytes.NewReader(config)); err != nil {
		t.Fatalf("Xray rejected domain routing config: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	rules, _ := decoded["routing"].(map[string]any)
	routingRules, _ := rules["rules"].([]any)
	if len(routingRules) != 1 {
		t.Fatalf("unexpected routing rules: %#v", routingRules)
	}
	firstRule, _ := routingRules[0].(map[string]any)
	if firstRule["outboundTag"] != "direct" {
		t.Fatalf("bypass rule does not use direct outbound: %#v", firstRule)
	}
	if _, err := newRoutingOptions("proxy_only", nil); err == nil {
		t.Fatal("proxy_only without domains was accepted")
	}
	if full, err := newRoutingOptions("full", []string{"example.com"}); err != nil || len(full.DirectDomains) != 0 {
		t.Fatalf("full mode should ignore saved domain rules: %#v err=%v", full, err)
	}
}

func TestAutoDomainRoutingKeepsBalancerFallback(t *testing.T) {
	profile, err := parseURI(sample)
	if err != nil {
		t.Fatal(err)
	}
	routing, err := newRoutingOptions("proxy_only", []string{"domain:example.com"})
	if err != nil {
		t.Fatal(err)
	}
	config, err := makeAutoConfigWithRoutingOptions([]Profile{profile}, "cloudflare", nil, false, "auto", routing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = core.LoadConfig("json", bytes.NewReader(config)); err != nil {
		t.Fatalf("Xray rejected Auto domain routing config: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	routingConfig, _ := decoded["routing"].(map[string]any)
	rules, _ := routingConfig["rules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("unexpected Auto proxy-only rules: %#v", rules)
	}
	first, _ := rules[0].(map[string]any)
	second, _ := rules[1].(map[string]any)
	if first["balancerTag"] != "shadow-auto" || second["outboundTag"] != "direct" {
		t.Fatalf("Auto domain rule lost balancer/direct fallback: %#v", rules)
	}
}

func TestServiceProfilesWithUnspecifiedAddressAreHidden(t *testing.T) {
	serviceIPv4 := strings.Replace(sample, "example.com:443", "0.0.0.0:443", 1)
	serviceIPv6 := strings.Replace(sample, "example.com:443", "[::]:443", 1)
	profiles, err := parseSubscription([]byte(serviceIPv4 + "\n" + sample + "\n" + serviceIPv6))
	if err != nil || len(profiles) != 1 || profiles[0].Address != "example.com" {
		t.Fatalf("service profiles were not hidden: %#v err=%v", profiles, err)
	}
	if _, err := parseSubscription([]byte(serviceIPv4)); err == nil {
		t.Fatal("subscription containing only service profiles was accepted")
	}
}

func TestTCPPing(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			_ = connection.Close()
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	latency, err := tcpPing(ctx, "127.0.0.1", port)
	if err != nil || latency < 1 {
		t.Fatalf("TCP ping failed: latency=%d err=%v", latency, err)
	}
}

func TestProfileEndpointStaysPrivate(t *testing.T) {
	profiles, err := parseSubscription([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if profiles[0].Address != "example.com" || profiles[0].Port != 443 {
		t.Fatalf("wrong endpoint: %s:%d", profiles[0].Address, profiles[0].Port)
	}
	public, _ := json.Marshal(profiles)
	if strings.Contains(string(public), "example.com") {
		t.Fatal("TCP endpoint exposed to renderer")
	}
}

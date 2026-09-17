package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	xgeodata "github.com/xtls/xray-core/common/geodata"
	"github.com/xtls/xray-core/core"
	"google.golang.org/protobuf/proto"
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
	if !strings.Contains(string(config), `"statsInboundUplink":true`) || !strings.Contains(string(config), `"statsInboundDownlink":true`) {
		t.Fatal("TUN traffic counters missing")
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
	routing, err := newRoutingOptions("bypass", []string{" example.com ", "full:private.example.com", "keyword:chat"}, "", "")
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
	if _, err := newRoutingOptions("proxy_only", nil, "", ""); err == nil {
		t.Fatal("proxy_only without domains was accepted")
	}
	if full, err := newRoutingOptions("full", []string{"example.com"}, "bad-url", "bad-url"); err != nil || len(full.DirectDomains) != 0 {
		t.Fatalf("full mode should ignore saved domain rules: %#v err=%v", full, err)
	}
}

func TestGeoDataRoutingSeparatesDomainAndIPRules(t *testing.T) {
	routing, err := newRoutingOptions(
		"bypass",
		[]string{"geosite:youtube", "geoip:ru", "geosite:youtube"},
		"https://example.com/geoip.dat",
		"https://example.com/geosite.dat",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(routing.DirectDomains) != 1 || len(routing.IPRules) != 1 || !routing.NeedsGeoIP || !routing.NeedsGeoSite {
		t.Fatalf("GeoData rules were not classified: %#v", routing)
	}
	routing.AssetDir = t.TempDir()
	geoIP, err := proto.Marshal(&xgeodata.GeoIPList{Entry: []*xgeodata.GeoIP{{
		Code: "RU",
		Cidr: []*xgeodata.CIDR{{Ip: []byte{127, 0, 0, 0}, Prefix: 8}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	geoSite, err := proto.Marshal(&xgeodata.GeoSiteList{Entry: []*xgeodata.GeoSite{{
		Code:   "YOUTUBE",
		Domain: []*xgeodata.Domain{{Type: xgeodata.Domain_Domain, Value: "youtube.com"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(routing.AssetDir+"/geoip.dat", geoIP, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(routing.AssetDir+"/geosite.dat", geoSite, 0o600); err != nil {
		t.Fatal(err)
	}
	config := runtimeConfigWithRouting([]any{map[string]any{"tag": "proxy", "protocol": "freedom", "settings": map[string]any{}}}, dnsPreset{Name: "Test", Servers: []string{"1.1.1.1"}}, "auto", routing)
	routingConfig := config["routing"].(map[string]any)
	rules := routingConfig["rules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("GeoSite and GeoIP must be independent OR-rules: %#v", rules)
	}
	if _, ok := rules[0].(map[string]any)["domain"]; !ok {
		t.Fatalf("first GeoData rule is not a domain rule: %#v", rules[0])
	}
	if _, ok := rules[1].(map[string]any)["ip"]; !ok {
		t.Fatalf("second GeoData rule is not an IP rule: %#v", rules[1])
	}
	if config["env"].(map[string]any)["XRAY_LOCATION_ASSET"] != routing.AssetDir {
		t.Fatal("custom GeoData asset path is missing")
	}
	if len(config["geodata"].(map[string]any)["assets"].([]any)) != 2 {
		t.Fatal("native Xray GeoData refresh is missing")
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.LoadConfig("json", bytes.NewReader(encoded)); err != nil {
		t.Fatalf("Xray rejected GeoData routing config: %v", err)
	}
	if _, err := newRoutingOptions("bypass", []string{"geoip:ru"}, "", ""); err == nil {
		t.Fatal("geoip rule without a source was accepted")
	}
	if _, err := newRoutingOptions("bypass", []string{"geosite:youtube"}, "", "http://example.com/geosite.dat"); err == nil {
		t.Fatal("insecure GeoSite URL was accepted")
	}
}

func TestGeoDataProtobufValidation(t *testing.T) {
	geoIP, err := proto.Marshal(&xgeodata.GeoIPList{Entry: []*xgeodata.GeoIP{{
		Code: "RU",
		Cidr: []*xgeodata.CIDR{{Ip: []byte{127, 0, 0, 0}, Prefix: 8}},
	}}})
	if err != nil || validateGeoDataAsset(geoIP, "geoip.dat") != nil {
		t.Fatalf("valid GeoIP was rejected: %v", err)
	}
	geoSite, err := proto.Marshal(&xgeodata.GeoSiteList{Entry: []*xgeodata.GeoSite{{
		Code:   "YOUTUBE",
		Domain: []*xgeodata.Domain{{Type: xgeodata.Domain_Domain, Value: "youtube.com"}},
	}}})
	if err != nil || validateGeoDataAsset(geoSite, "geosite.dat") != nil {
		t.Fatalf("valid GeoSite was rejected: %v", err)
	}
	if validateGeoDataAsset([]byte("not protobuf"), "geoip.dat") == nil {
		t.Fatal("invalid GeoIP protobuf was accepted")
	}
}

func TestAutoDomainRoutingKeepsBalancerFallback(t *testing.T) {
	profile, err := parseURI(sample)
	if err != nil {
		t.Fatal(err)
	}
	routing, err := newRoutingOptions("proxy_only", []string{"domain:example.com"}, "", "")
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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/observatory"
	"github.com/xtls/xray-core/core"
)

func TestProfilesForRendererPrependsAutoWithoutSecrets(t *testing.T) {
	profiles, err := parseSubscription([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	public := profilesForRenderer(profiles)
	if len(public) != 3 || !public[0].Auto || public[0].ID != autoProfileID || public[0].Name != "Авто" || public[1].ID != autoNoRUProfileID || public[1].Name != "Авто без РФ" {
		t.Fatalf("auto profile missing: %#v", public)
	}
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), "example.com") || strings.Contains(string(encoded), "00000000-") {
		t.Fatal("credentials exposed in renderer profiles")
	}
}

func TestProfilesByIDPreservesSubscriptionOrder(t *testing.T) {
	profiles := []Profile{{ID: "one"}, {ID: "two"}, {ID: "three"}}
	filtered := profilesByID(profiles, []string{"three", "one", "missing"})
	if len(filtered) != 2 || filtered[0].ID != "one" || filtered[1].ID != "three" {
		t.Fatalf("wrong group candidates: %#v", filtered)
	}
	if all := profilesByID(profiles, nil); len(all) != len(profiles) {
		t.Fatalf("empty restriction should keep every profile: %#v", all)
	}
}

func TestConnectionInfoContainsOnlyPublicSelectedProfileData(t *testing.T) {
	profile := Profile{
		ID:       "1234567890abcdef12345678",
		Name:     "🇩🇪 Frankfurt",
		Address:  "vpn.secret.example",
		Port:     443,
		Outbound: map[string]any{"password": "top-secret"},
	}
	info := connectionInfoForProfile(profile)
	if info.ProfileID != profile.ID || info.Name != profile.Name {
		t.Fatalf("wrong connection info: %#v", info)
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, profile.Address) || strings.Contains(text, "top-secret") {
		t.Fatalf("connection response exposed a secret: %s", text)
	}
}

func TestFastestProfileIgnoresUnavailableAndKeepsStableTie(t *testing.T) {
	profiles := []Profile{{ID: "one"}, {ID: "two"}, {ID: "three"}}
	results := []PingResult{
		{ID: "one", LatencyMS: 9, Available: false},
		{ID: "two", LatencyMS: 21, Available: true},
		{ID: "three", LatencyMS: 21, Available: true},
	}
	index, best, ok := fastestProfile(profiles, results)
	if !ok || index != 1 || best.ID != "two" {
		t.Fatalf("wrong fastest profile: index=%d best=%#v ok=%t", index, best, ok)
	}
}

func TestFastestProfileReturnsUnavailable(t *testing.T) {
	if _, _, ok := fastestProfile([]Profile{{ID: "offline"}}, []PingResult{{ID: "offline"}}); ok {
		t.Fatal("offline profile selected")
	}
}

func TestBestObservedProfileSelectsFastestAliveAndKeepsStableTie(t *testing.T) {
	profiles := []Profile{{ID: "one"}, {ID: "two"}, {ID: "three"}}
	statuses := []*observatory.OutboundStatus{
		{OutboundTag: "auto-one", Alive: false, Delay: 4},
		{OutboundTag: "auto-two", Alive: true, Delay: 19},
		{OutboundTag: "auto-three", Alive: true, Delay: 19},
	}
	best, delay, ok := bestObservedProfile(profiles, statuses)
	if !ok || best.ID != "two" || delay != 19 {
		t.Fatalf("wrong observed profile: best=%#v delay=%d ok=%t", best, delay, ok)
	}
}

func TestPingProfilesWithAutoMirrorsBestLatency(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = connection.Close()
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	results := pingProfilesWithAuto(ctx, []Profile{{ID: "server", Address: "127.0.0.1", Port: port}})
	if len(results) != 3 || results[0].ID != autoProfileID || results[1].ID != autoNoRUProfileID || results[2].ID != "server" {
		t.Fatalf("unexpected results: %#v", results)
	}
	if !results[0].Available || !results[1].Available || results[0].LatencyMS != results[2].LatencyMS || results[1].LatencyMS != results[2].LatencyMS {
		t.Fatalf("auto latency does not mirror best server: %#v", results)
	}
}

func TestAutoNoRUFiltersRussianProfiles(t *testing.T) {
	profiles := []Profile{{ID: "ru", Name: "🇷🇺 Москва"}, {ID: "fi", Name: "🇫🇮 Finland"}, {ID: "ru2", Name: "RU | Russia"}}
	filtered := withoutRussianProfiles(profiles)
	if len(filtered) != 1 || filtered[0].ID != "fi" {
		t.Fatalf("Russian profiles were not filtered: %#v", filtered)
	}
}

func TestPingResultDoesNotExposeResolvedEndpoint(t *testing.T) {
	encoded, err := json.Marshal(PingResult{ID: "server", Available: true, LatencyMS: 12, ResolvedAddress: "203.0.113.7"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "203.0.113.7") {
		t.Fatalf("resolved endpoint exposed to renderer: %s", encoded)
	}
}

func TestPrepareAutoProfilesPutsFastestFirst(t *testing.T) {
	first, err := parseURI(strings.Replace(sample, "example.com:443", "192.0.2.10:443", 1))
	if err != nil {
		t.Fatal(err)
	}
	second, err := parseURI(strings.Replace(sample, "example.com:443", "192.0.2.20:443", 1))
	if err != nil {
		t.Fatal(err)
	}
	profiles := []Profile{first, second}
	results := []PingResult{
		{ID: first.ID, Available: true, LatencyMS: 80, ResolvedAddress: "192.0.2.10"},
		{ID: second.ID, Available: true, LatencyMS: 20, ResolvedAddress: "192.0.2.20"},
	}
	prepared, endpoints, warnings, err := prepareAutoProfiles(context.Background(), profiles, results)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(prepared) != 2 || prepared[0].ID != second.ID {
		t.Fatalf("wrong prepared order: profiles=%#v warnings=%#v", prepared, warnings)
	}
	if len(endpoints) != 2 || endpoints[0].Address != "192.0.2.20" {
		t.Fatalf("wrong endpoint allowlist: %#v", endpoints)
	}
}

func TestAutoConfigEnablesLeastPingFallback(t *testing.T) {
	first, _ := parseURI(strings.Replace(sample, "example.com:443", "192.0.2.10:443", 1))
	second, _ := parseURI(strings.Replace(sample, "example.com:443", "192.0.2.20:443", 1))
	config, err := makeAutoConfigWithOptions([]Profile{second, first}, "cloudflare", nil, false, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = core.LoadConfig("json", bytes.NewReader(config)); err != nil {
		t.Fatalf("Xray rejected continuous Auto config: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	observatory, _ := decoded["observatory"].(map[string]any)
	if observatory["probeInterval"] != autoProbeInterval {
		t.Fatalf("wrong observatory config: %#v", observatory)
	}
	routing, _ := decoded["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	rule, _ := rules[0].(map[string]any)
	if rule["balancerTag"] != "shadow-auto" {
		t.Fatalf("Auto traffic is not routed through the balancer: %#v", rule)
	}
	balancers, _ := routing["balancers"].([]any)
	balancer, _ := balancers[0].(map[string]any)
	strategy, _ := balancer["strategy"].(map[string]any)
	if strategy["type"] != "leastping" || balancer["fallbackTag"] != "auto-"+second.ID {
		t.Fatalf("wrong fallback balancer config: %#v", balancer)
	}
	outbounds, _ := decoded["outbounds"].([]any)
	firstOutbound, _ := outbounds[0].(map[string]any)
	if firstOutbound["tag"] != "auto-"+second.ID {
		t.Fatalf("fastest outbound is not first: %#v", firstOutbound["tag"])
	}
}

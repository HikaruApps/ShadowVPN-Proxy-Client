package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xtls/xray-core/core"
)

func TestAppRoutingRules(t *testing.T) {
	routing, err := newRoutingOptions("bypass", []string{`app:Steam.exe, C:\Program Files\Discord.exe` + "\nexample.com"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Steam", "steam", "Discord", "discord"}
	if strings.Join(routing.Processes, ",") != strings.Join(want, ",") || len(routing.DirectDomains) != 1 {
		t.Fatalf("processes=%v domains=%v", routing.Processes, routing.DirectDomains)
	}
	for _, bad := range []string{"app:", "app:a|b.exe", "app:.exe"} {
		if _, err := newRoutingOptions("bypass", []string{bad}, "", ""); err == nil {
			t.Fatalf("invalid app rule %q accepted", bad)
		}
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
		t.Fatalf("Xray rejected process rules: %v", err)
	}
	if !strings.Contains(string(config), `"process":["Steam","steam","Discord","discord"]`) {
		t.Fatalf("process rule missing: %s", config)
	}
}

func TestSystemProxyModeConfig(t *testing.T) {
	profile, err := parseURI(sample)
	if err != nil {
		t.Fatal(err)
	}
	routing, err := newRoutingOptions("proxy_only", []string{"app:telegram.exe"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	routing.Proxy = true
	config, err := makeConfigWithRoutingAndDoH(profile, "cloudflare", nil, "https://dns.example/dns-query", false, "auto", routing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = core.LoadConfig("json", bytes.NewReader(config)); err != nil {
		t.Fatalf("Xray rejected proxy-mode config: %v", err)
	}
	var decoded struct {
		Inbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Listen   string `json:"listen"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Inbounds) != 2 || decoded.Inbounds[0].Tag != "socks-in" || decoded.Inbounds[1].Listen != "127.0.0.1" {
		t.Fatalf("proxy inbounds wrong: %+v", decoded.Inbounds)
	}
	if strings.Contains(string(config), "tun-in") {
		t.Fatalf("proxy-mode config still references the TUN: %s", config)
	}
}

func TestSettingsVaultRoundTrip(t *testing.T) {
	directory := t.TempDir()
	previous := vaultPath
	vaultPath = func() (string, error) { return filepath.Join(directory, "vault.bin"), nil }
	defer func() { vaultPath = previous }()
	if data, err := loadVault(); err != nil || data != "" {
		t.Fatalf("empty vault = %q, %v", data, err)
	}
	state := `{"items":[{"id":"sub-a1b2c3d4","url":"https://sub.example/token"}]}`
	if err := saveVault(state); err != nil {
		t.Fatal(err)
	}
	if data, err := loadVault(); err != nil || data != state {
		t.Fatalf("vault = %q, %v", data, err)
	}
	if err := saveVault(""); err != nil {
		t.Fatal(err)
	}
	if data, _ := loadVault(); data != "" {
		t.Fatalf("cleared vault still holds %q", data)
	}
	if err := saveVault(strings.Repeat("x", vaultLimit+1)); err == nil {
		t.Fatal("oversized vault accepted")
	}
}

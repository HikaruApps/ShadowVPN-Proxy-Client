package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xtls/xray-core/core"
)

func TestSelectedDNSPresets(t *testing.T) {
	tests := []struct {
		id      string
		name    string
		address string
	}{
		{"", "Cloudflare", "1.1.1.1"},
		{"cloudflare", "Cloudflare", "2606:4700:4700::1111"},
		{"google", "Google", "8.8.8.8"},
		{"quad9", "Quad9", "9.9.9.9"},
	}
	for _, test := range tests {
		_, preset, err := selectedDNS(test.id, nil)
		if err != nil || preset.Name != test.name || !strings.Contains(strings.Join(preset.Servers, ","), test.address) {
			t.Fatalf("preset %q: %#v err=%v", test.id, preset, err)
		}
	}
}

func TestSelectedDNSRejectsUnknownValue(t *testing.T) {
	if _, _, err := selectedDNS("attacker.example", nil); err == nil {
		t.Fatal("unknown DNS preset accepted")
	}
}

func TestConfigUsesSelectedDNSOnly(t *testing.T) {
	profile, err := parseURI(sample)
	if err != nil {
		t.Fatal(err)
	}
	config, err := makeConfigWithDNS(profile, "quad9", nil)
	if err != nil {
		t.Fatal(err)
	}
	value := string(config)
	if !strings.Contains(value, "9.9.9.9") || strings.Contains(value, "1.1.1.1") {
		t.Fatalf("wrong DNS in config: %s", value)
	}
}

func TestCustomDNSValidation(t *testing.T) {
	id, preset, err := selectedDNS("custom", []string{" 192.168.1.1 ", "2606:4700:4700::1111", "192.168.1.1"})
	if err != nil || id != "custom" || len(preset.Servers) != 2 || preset.Servers[0] != "192.168.1.1" {
		t.Fatalf("custom DNS validation failed: id=%q preset=%#v err=%v", id, preset, err)
	}
	for _, servers := range [][]string{
		nil,
		{"not-an-ip"},
		{"0.0.0.0"},
		{"::"},
		{"224.0.0.1"},
		{"255.255.255.255"},
		{"1.1.1.1", "8.8.8.8", "9.9.9.9", "149.112.112.112", "8.8.4.4"},
	} {
		if _, _, err := selectedDNS("custom", servers); err == nil {
			t.Fatalf("invalid custom DNS accepted: %#v", servers)
		}
	}
}

func TestConfigUsesCustomDNS(t *testing.T) {
	profile, err := parseURI(sample)
	if err != nil {
		t.Fatal(err)
	}
	config, err := makeConfigWithDNS(profile, "custom", []string{"10.10.10.10", "2001:4860:4860::8888"})
	if err != nil || !strings.Contains(string(config), "10.10.10.10") || strings.Contains(string(config), "1.1.1.1") {
		t.Fatalf("custom DNS missing from config: %s err=%v", config, err)
	}
}

func TestConfigUsesNativeTLSHelloFragmentation(t *testing.T) {
	profile, err := parseURI(sample)
	if err != nil {
		t.Fatal(err)
	}
	config, err := makeConfigWithOptions(profile, "cloudflare", nil, true, "Ethernet")
	if err != nil {
		t.Fatal(err)
	}
	value := string(config)
	for _, expected := range []string{`"finalmask"`, `"type":"fragment"`, `"packets":"tlshello"`, `"length":"10-35"`} {
		if !strings.Contains(value, expected) {
			t.Fatalf("fragmentation field %s missing: %s", expected, value)
		}
	}
	if !strings.Contains(value, `"autoOutboundsInterface":"Ethernet"`) {
		t.Fatalf("fixed outbound interface missing: %s", value)
	}
	if _, err = core.LoadConfig("json", bytes.NewReader(config)); err != nil {
		t.Fatalf("Xray rejected fragmentation config: %v", err)
	}
	plain, err := makeConfigWithDNS(profile, "cloudflare", nil)
	if err != nil || strings.Contains(string(plain), `"finalmask"`) {
		t.Fatalf("fragmentation leaked into disabled config: %s err=%v", plain, err)
	}
}

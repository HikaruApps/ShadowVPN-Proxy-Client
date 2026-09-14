package main

import (
	"bytes"
	"context"
	"net"
	"reflect"
	"testing"

	"github.com/xtls/xray-core/core"
)

func TestResolveProfileEndpointPrefersIPv4AndPreservesReality(t *testing.T) {
	profile, err := parseURI("vless://00000000-0000-4000-8000-000000000001@edge.example:443?security=reality&sni=cover.example&fp=chrome&pbk=public&sid=abcd&type=tcp&flow=xtls-rprx-vision#Reality")
	if err != nil {
		t.Fatal(err)
	}
	originalLookup := lookupEndpointIPs
	lookupEndpointIPs = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{
			{IP: net.ParseIP("2001:db8::20")},
			{IP: net.ParseIP("203.0.113.20")},
			{IP: net.ParseIP("203.0.113.20")},
		}, nil
	}
	defer func() { lookupEndpointIPs = originalLookup }()

	resolved, info, err := resolveProfileEndpoint(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Address != "203.0.113.20" || info.SelectedAddress != "203.0.113.20" {
		t.Fatalf("wrong selected endpoint: profile=%q info=%q", resolved.Address, info.SelectedAddress)
	}
	if want := []string{"203.0.113.20", "2001:db8::20"}; !reflect.DeepEqual(info.Candidates, want) {
		t.Fatalf("candidates=%v; want %v", info.Candidates, want)
	}
	if address, _ := endpointFromOutbound(resolved.Outbound); address != "203.0.113.20" {
		t.Fatalf("runtime outbound still uses %q", address)
	}
	if address, _ := endpointFromOutbound(profile.Outbound); address != "edge.example" {
		t.Fatalf("source profile was mutated: %q", address)
	}
	stream := resolved.Outbound["streamSettings"].(map[string]any)
	reality := stream["realitySettings"].(map[string]any)
	if reality["serverName"] != "cover.example" {
		t.Fatalf("Reality SNI changed: %v", reality["serverName"])
	}
}

func TestResolveProfileEndpointPreservesImplicitTransportHostnames(t *testing.T) {
	for _, test := range []struct {
		name     string
		raw      string
		settings string
		value    string
		want     string
	}{
		{"tls", "vless://00000000-0000-4000-8000-000000000001@edge.example:443?security=tls&type=tcp", "tlsSettings", "serverName", "edge.example"},
		{"xhttp", "vless://00000000-0000-4000-8000-000000000001@edge.example:443?security=tls&sni=cover.example&type=xhttp&path=%2Fapi", "xhttpSettings", "host", "edge.example"},
		{"httpupgrade", "vless://00000000-0000-4000-8000-000000000001@edge.example:443?security=tls&sni=cover.example&type=httpupgrade&path=%2Fapi", "httpupgradeSettings", "host", "edge.example"},
		{"grpc", "vless://00000000-0000-4000-8000-000000000001@edge.example:443?security=tls&sni=cover.example&type=grpc&serviceName=api", "grpcSettings", "authority", "edge.example"},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile, err := parseURI(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := profileWithEndpoint(profile, "203.0.113.30", "edge.example")
			if err != nil {
				t.Fatal(err)
			}
			stream := resolved.Outbound["streamSettings"].(map[string]any)
			settings := stream[test.settings].(map[string]any)
			if settings[test.value] != test.want {
				t.Fatalf("%s.%s=%v; want %q", test.settings, test.value, settings[test.value], test.want)
			}
			config, err := makeConfig(resolved)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := core.LoadConfig("json", bytes.NewReader(config)); err != nil {
				t.Fatalf("Xray rejected resolved config: %v", err)
			}
		})
	}

	profile, err := parseURI("vless://00000000-0000-4000-8000-000000000001@edge.example:443?security=tls&sni=cover.example&type=ws&path=%2Fapi")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := profileWithEndpoint(profile, "203.0.113.30", "edge.example")
	if err != nil {
		t.Fatal(err)
	}
	stream := resolved.Outbound["streamSettings"].(map[string]any)
	settings := stream["wsSettings"].(map[string]any)
	headers := settings["headers"].(map[string]any)
	if headers["Host"] != "edge.example" {
		t.Fatalf("wsSettings.headers.Host=%v; want edge.example", headers["Host"])
	}
}

func TestResolveProfileEndpointSkipsDNSForLiteralIP(t *testing.T) {
	profile, err := parseURI("vless://00000000-0000-4000-8000-000000000001@198.51.100.40:443?security=tls&sni=cover.example&type=tcp")
	if err != nil {
		t.Fatal(err)
	}
	originalLookup := lookupEndpointIPs
	lookupEndpointIPs = func(context.Context, string) ([]net.IPAddr, error) {
		t.Fatal("DNS lookup called for an IP literal")
		return nil, nil
	}
	defer func() { lookupEndpointIPs = originalLookup }()

	resolved, info, err := resolveProfileEndpoint(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Address != "198.51.100.40" || info.OriginalAddress != "198.51.100.40" {
		t.Fatalf("unexpected bootstrap result: %#v %#v", resolved, info)
	}
}

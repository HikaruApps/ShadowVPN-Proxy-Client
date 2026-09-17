package main

import (
	"net/http"
	"regexp"
	"testing"
)

func TestClientIdentityHeaders(t *testing.T) {
	first := hashedHWID("machine-guid")
	if first != hashedHWID("machine-guid") || first == hashedHWID("other-guid") {
		t.Fatal("HWID hashing is not stable and device-specific")
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(first) {
		t.Fatalf("unexpected HWID format: %q", first)
	}
	request, _ := http.NewRequest("GET", "https://example.com/subscription", nil)
	setClientIdentityHeaders(request, deviceInfo{HWID: first, UserAgent: appUserAgent, OS: "windows"})
	if request.Header.Get("User-Agent") != "shadowvpn/0.23.0" || request.Header.Get("X-Hwid") != first || request.Header.Get("X-Device-Os") != "windows" {
		t.Fatalf("identity headers are incomplete: %#v", request.Header)
	}
}

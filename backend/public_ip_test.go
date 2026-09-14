package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePublicIPv4(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{" 28.9.123.45\n", "28.9.123.45", true},
		{"192.168.1.1", "", false},
		{"127.0.0.1", "", false},
		{"2001:db8::1", "", false},
		{"not-an-ip", "", false},
	}
	for _, test := range tests {
		got, ok := parsePublicIPv4(test.input)
		if got != test.want || ok != test.ok {
			t.Fatalf("parsePublicIPv4(%q) = %q, %v; want %q, %v", test.input, got, ok, test.want, test.ok)
		}
	}
}

func TestMaskedPublicIPv4(t *testing.T) {
	if got := maskedPublicIPv4("28.9.123.45"); got != "28.9.***.***" {
		t.Fatalf("maskedPublicIPv4() = %q", got)
	}
}

func TestPublicIPv4UsesFallbackEndpoint(t *testing.T) {
	invalid := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("not-an-ip"))
	}))
	defer invalid.Close()
	valid := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("28.9.123.45\n"))
	}))
	defer valid.Close()

	original := publicIPv4Endpoints
	publicIPv4Endpoints = []string{invalid.URL, valid.URL}
	defer func() { publicIPv4Endpoints = original }()

	got, err := publicIPv4(context.Background())
	if err != nil || got != "28.9.123.45" {
		t.Fatalf("publicIPv4() = %q, %v; want 28.9.123.45, nil", got, err)
	}
}

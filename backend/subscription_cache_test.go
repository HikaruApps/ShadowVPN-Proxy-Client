package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

const cacheTestSubscription = "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls&type=tcp#Cached"

func useTempSubscriptionCache(t *testing.T) {
	t.Helper()
	directory := t.TempDir()
	previous := subscriptionCacheDir
	subscriptionCacheDir = func() (string, error) { return directory, nil }
	t.Cleanup(func() { subscriptionCacheDir = previous })
}

func TestSubscriptionCacheRoundTrip(t *testing.T) {
	useTempSubscriptionCache(t)
	address := "https://sub.example/secret-token"
	metadata := subscriptionMetadata{Title: "ShadowVPN"}
	if err := saveSubscriptionCache(address, []byte(cacheTestSubscription), metadata); err != nil {
		t.Fatal(err)
	}
	cached, err := loadSubscriptionCache(address)
	if err != nil || string(cached.Body) != cacheTestSubscription || cached.Metadata.Title != "ShadowVPN" {
		t.Fatalf("cache = %+v, %v", cached, err)
	}
	if _, err := loadSubscriptionCache("https://sub.example/other"); err == nil {
		t.Fatal("unexpected cache hit for another subscription")
	}
}

func TestPreferCacheSkipsNetwork(t *testing.T) {
	useTempSubscriptionCache(t)
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	address := server.URL + "/sub"
	if err := saveSubscriptionCache(address, []byte(cacheTestSubscription), subscriptionMetadata{}); err != nil {
		t.Fatal(err)
	}
	profiles, _, _, cached, err := fetchSubscription(context.Background(), address, true, nil)
	if err != nil || !cached || len(profiles) != 1 || profiles[0].Name != "Cached" {
		t.Fatalf("profiles=%v cached=%t err=%v", profiles, cached, err)
	}
	if requests != 0 {
		t.Fatalf("cache-first load made %d network requests", requests)
	}
}

func TestFailedRefreshFallsBackToCache(t *testing.T) {
	useTempSubscriptionCache(t)
	// The self-signed test certificate fails verification, like a broken network would.
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	address := server.URL + "/sub"
	var logged []string
	logf := func(format string, values ...any) { logged = append(logged, fmt.Sprintf(format, values...)) }
	if _, _, _, _, err := fetchSubscription(context.Background(), address, false, logf); err == nil || !strings.Contains(err.Error(), "Сертификат") {
		t.Fatalf("expected certificate error without cache, got %v", err)
	}
	if err := saveSubscriptionCache(address, []byte(cacheTestSubscription), subscriptionMetadata{}); err != nil {
		t.Fatal(err)
	}
	profiles, _, _, cached, err := fetchSubscription(context.Background(), address, false, logf)
	if err != nil || !cached || len(profiles) != 1 {
		t.Fatalf("profiles=%v cached=%t err=%v", profiles, cached, err)
	}
	for _, line := range logged {
		if strings.Contains(line, "/sub") {
			t.Fatalf("log line leaks subscription path: %s", line)
		}
	}
}

func TestSubscriptionFetchReasonHidesURL(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://sub.example/secret-token", Err: errors.New("connection reset")}
	cause, message := subscriptionFetchReason(err)
	if strings.Contains(cause, "secret-token") || strings.Contains(message, "secret-token") {
		t.Fatalf("reason leaks URL: %q / %q", cause, message)
	}
	blocked := &url.Error{Op: "Get", URL: "https://x", Err: errors.New("dial tcp: An attempt was made to access a socket in a way forbidden by its access permissions.")}
	if cause, _ := subscriptionFetchReason(blocked); !strings.HasPrefix(cause, "blocked:") {
		t.Fatalf("blocked socket not classified: %q", cause)
	}
	localized := &url.Error{Op: "Get", URL: "https://x", Err: &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connectex", wsaeAccess)}}
	if cause, _ := subscriptionFetchReason(localized); !strings.HasPrefix(cause, "blocked:") {
		t.Fatalf("WSAEACCES not classified: %q", cause)
	}
}

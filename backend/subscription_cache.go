package main

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// WSAEACCES: a WFP filter (for example a Kill Switch left by a previous core)
// rejected the socket. Windows localizes the message, so match the code.
const wsaeAccess = syscall.Errno(10013)

// The last good subscription body is kept on disk so a restart does not need
// the network. On Windows it is sealed with DPAPI for the current user; server
// credentials therefore never leave the Go process in plain text.
const subscriptionCacheLimit = 4*1024*1024 + 64*1024

type cachedSubscription struct {
	SavedAt  int64                `json:"savedAt"`
	Body     []byte               `json:"body"`
	Metadata subscriptionMetadata `json:"metadata"`
}

var subscriptionCacheDir = func() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ShadowVPN", "subscriptions"), nil
}

func subscriptionCachePath(address string) (string, error) {
	directory, err := subscriptionCacheDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte("net.shadownet.shadowvpn:subscription:v1:" + address))
	return filepath.Join(directory, hex.EncodeToString(sum[:16])+".bin"), nil
}

func saveSubscriptionCache(address string, body []byte, metadata subscriptionMetadata) error {
	path, err := subscriptionCachePath(address)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(cachedSubscription{SavedAt: time.Now().Unix(), Body: body, Metadata: metadata})
	if err != nil {
		return err
	}
	sealed, err := protectCacheData(plain)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err = os.WriteFile(temporary, sealed, 0o600); err != nil {
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func loadSubscriptionCache(address string) (cachedSubscription, error) {
	path, err := subscriptionCachePath(address)
	if err != nil {
		return cachedSubscription{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return cachedSubscription{}, err
	}
	if info.Size() > subscriptionCacheLimit*2 {
		return cachedSubscription{}, errors.New("subscription cache is too large")
	}
	sealed, err := os.ReadFile(path)
	if err != nil {
		return cachedSubscription{}, err
	}
	plain, err := unprotectCacheData(sealed)
	if err != nil {
		return cachedSubscription{}, err
	}
	var cached cachedSubscription
	if err = json.Unmarshal(plain, &cached); err != nil || len(cached.Body) == 0 || len(cached.Body) > subscriptionCacheLimit {
		return cachedSubscription{}, errors.New("subscription cache is corrupted")
	}
	return cached, nil
}

// subscriptionFetchReason turns a transport error into a log-safe cause and a
// user-facing message. The URL (which carries the subscription token) is never
// included: url.Error is unwrapped first.
func subscriptionFetchReason(err error) (string, string) {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	cause := err.Error()
	var dnsErr *net.DNSError
	var certErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invalidErr x509.CertificateInvalidError
	var opErr *net.OpError
	var netErr net.Error
	lower := strings.ToLower(cause)
	switch {
	case errors.As(err, &dnsErr):
		return "dns: " + cause, "Не удалось найти сервер подписки (DNS). Проверьте интернет"
	case errors.As(err, &certErr), errors.As(err, &hostErr), errors.As(err, &invalidErr), strings.Contains(lower, "x509") || strings.Contains(lower, "certificate"):
		return "tls: " + cause, "Сертификат сервера подписки не прошёл проверку. Проверьте дату и время на компьютере"
	case errors.Is(err, wsaeAccess) || strings.Contains(lower, "forbidden by its access permissions"):
		return "blocked: " + cause, "Сетевой доступ заблокирован (возможно, остался Kill Switch прошлого сеанса). Перезапустите приложение"
	case errors.As(err, &netErr) && netErr.Timeout(), strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "timeout: " + cause, "Сервер подписки не ответил вовремя. Проверьте интернет"
	case errors.As(err, &opErr):
		return "network: " + cause, "Не удалось подключиться к серверу подписки. Проверьте интернет и ссылку"
	}
	return "other: " + cause, "Не удалось загрузить подписку. Проверьте интернет и ссылку"
}

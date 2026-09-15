package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

var publicIPv4Endpoints = []string{
	"https://api4.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://checkip.amazonaws.com",
}

func parsePublicIPv4(value string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil || ip.To4() == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		return "", false
	}
	return ip.To4().String(), true
}

func maskedPublicIPv4(value string) string {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return "***.***.***.***"
	}
	return parts[0] + "." + parts[1] + ".***.***"
}

func publicIPv4(parent context.Context) (string, error) {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 4 * time.Second, KeepAlive: 15 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   4 * time.Second,
		ResponseHeaderTimeout: 4 * time.Second,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, endpoint := range publicIPv4Endpoints {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err == nil {
			req.Header.Set("Accept", "text/plain")
			req.Header.Set("User-Agent", appUserAgent)
			var response *http.Response
			response, err = client.Do(req)
			if err == nil {
				body, readErr := io.ReadAll(io.LimitReader(response.Body, 65))
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK && readErr == nil {
					if ip, ok := parsePublicIPv4(string(body)); ok {
						cancel()
						return ip, nil
					}
				}
			}
		}
		cancel()
	}
	return "", errors.New("Не удалось определить внешний IPv4")
}

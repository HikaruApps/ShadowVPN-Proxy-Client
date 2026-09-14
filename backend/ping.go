package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

type PingResult struct {
	ID              string `json:"id"`
	LatencyMS       int64  `json:"latencyMs,omitempty"`
	Available       bool   `json:"available"`
	ResolvedAddress string `json:"-"`
}

func endpointFromOutbound(out map[string]any) (string, int) {
	settings, _ := out["settings"].(map[string]any)
	protocol, _ := out["protocol"].(string)
	key := "servers"
	if protocol == "vless" || protocol == "vmess" {
		key = "vnext"
	}
	raw, _ := settings[key].([]any)
	if len(raw) == 0 {
		return "", 0
	}
	server, _ := raw[0].(map[string]any)
	address, _ := server["address"].(string)
	return address, integerValue(server["port"])
}

func integerValue(value any) int {
	switch value := value.(type) {
	case int:
		return value
	case float64:
		return int(value)
	case json.Number:
		result, _ := strconv.Atoi(value.String())
		return result
	case string:
		result, _ := strconv.Atoi(value)
		return result
	default:
		return 0
	}
}

func tcpPing(ctx context.Context, address string, port int) (int64, error) {
	latency, _, err := tcpPingEndpoint(ctx, address, port)
	return latency, err
}

func tcpPingEndpoint(ctx context.Context, address string, port int) (int64, string, error) {
	if address == "" || port < 1 || port > 65535 {
		return 0, "", fmt.Errorf("missing endpoint")
	}
	started := time.Now()
	connection, err := (&net.Dialer{Timeout: 2500 * time.Millisecond}).DialContext(
		ctx,
		"tcp",
		net.JoinHostPort(address, strconv.Itoa(port)),
	)
	if err != nil {
		return 0, "", err
	}
	resolvedAddress := ""
	if remote, ok := connection.RemoteAddr().(*net.TCPAddr); ok && remote.IP != nil {
		resolvedAddress = remote.IP.String()
	}
	_ = connection.Close()
	latency := time.Since(started).Milliseconds()
	if latency < 1 {
		latency = 1
	}
	return latency, resolvedAddress, nil
}

func pingProfiles(parent context.Context, profiles []Profile) []PingResult {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	results := make([]PingResult, len(profiles))
	jobs := make(chan int)
	workers := 32
	if len(profiles) < workers {
		workers = len(profiles)
	}
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				profile := profiles[index]
				latency, resolvedAddress, err := tcpPingEndpoint(ctx, profile.Address, profile.Port)
				results[index] = PingResult{ID: profile.ID, LatencyMS: latency, Available: err == nil, ResolvedAddress: resolvedAddress}
			}
		}()
	}
	for index := range profiles {
		select {
		case jobs <- index:
		case <-ctx.Done():
			results[index] = PingResult{ID: profiles[index].ID}
		}
	}
	close(jobs)
	group.Wait()
	return results
}

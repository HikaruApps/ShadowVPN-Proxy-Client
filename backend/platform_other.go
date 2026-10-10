//go:build !windows

package main

import (
	"errors"
	"net"
	"time"
)

type killSwitchGuard struct{}

func checkPlatform() error                       { return errors.New("Windows required") }
func physicalOutboundInterface() (string, error) { return "", errors.New("Windows required") }
func startKillSwitchGuard([]proxyEndpoint) (*killSwitchGuard, error) {
	return nil, errors.New("Windows required")
}
func cleanupStaleNetworkState(func(string, ...any)) error { return nil }
func (*killSwitchGuard) Close() error                     { return nil }
func (*killSwitchGuard) AllowEndpoint(proxyEndpoint) error {
	return errors.New("Windows required")
}
func (*killSwitchGuard) AllowTunnel(uint64) error { return errors.New("Windows required") }
func interfaceDialer(interfaceName string, timeout time.Duration) (*net.Dialer, error) {
	if interfaceName != "" {
		return nil, errors.New("outbound interface pinning requires Windows")
	}
	return &net.Dialer{Timeout: timeout}, nil
}
func tunnelInterfaceLUID() (uint64, error) { return 0, errors.New("Windows required") }

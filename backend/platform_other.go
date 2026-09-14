//go:build !windows

package main

import "errors"

type killSwitchGuard struct{}

func checkPlatform() error                       { return errors.New("Windows required") }
func physicalOutboundInterface() (string, error) { return "", errors.New("Windows required") }
func startKillSwitchGuard([]proxyEndpoint) (*killSwitchGuard, error) {
	return nil, errors.New("Windows required")
}
func cleanupStaleKillSwitch() error   { return nil }
func (*killSwitchGuard) Close() error { return nil }

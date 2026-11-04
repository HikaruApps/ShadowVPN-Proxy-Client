//go:build !windows

package main

import "errors"

func enableSystemProxy(int, int) (func() error, error) {
	return nil, errors.New("system proxy requires Windows")
}

func restoreSystemProxy() error { return nil }

//go:build !windows

package main

import "os"

func deviceHWID() string {
	hostname, _ := os.Hostname()
	return hashedHWID("fallback:" + hostname)
}

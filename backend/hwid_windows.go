//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

func deviceHWID() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err == nil {
		defer key.Close()
		if machineGUID, _, readErr := key.GetStringValue("MachineGuid"); readErr == nil && machineGUID != "" {
			return hashedHWID(machineGUID)
		}
	}
	hostname, _ := os.Hostname()
	return hashedHWID("fallback:" + hostname)
}

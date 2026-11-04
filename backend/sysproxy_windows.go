//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// savedProxy is the user's own proxy setup, written to disk before the core
// changes it so a crashed core can still put it back on the next start.
type savedProxy struct {
	Enable    uint32 `json:"enable"`
	Server    string `json:"server"`
	Override  string `json:"override"`
	AutoURL   string `json:"autoUrl"`
	HasServer bool   `json:"hasServer"`
	HasOver   bool   `json:"hasOverride"`
	HasAuto   bool   `json:"hasAutoUrl"`
}

var (
	wininet               = windows.NewLazySystemDLL("wininet.dll")
	procInternetSetOption = wininet.NewProc("InternetSetOptionW")
)

func proxyMarkerPath() (string, error) {
	directory, err := subscriptionCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(directory), "system-proxy-restore.json"), nil
}

// notifyProxyChange tells WinINet users (browsers, most apps) to re-read the
// settings now instead of on their next start.
func notifyProxyChange() {
	const settingsChanged, refresh = 39, 37
	_, _, _ = procInternetSetOption.Call(0, settingsChanged, 0, 0)
	_, _, _ = procInternetSetOption.Call(0, refresh, 0, 0)
}

func readProxySettings(key registry.Key) savedProxy {
	var saved savedProxy
	if value, _, err := key.GetIntegerValue("ProxyEnable"); err == nil {
		saved.Enable = uint32(value)
	}
	if value, _, err := key.GetStringValue("ProxyServer"); err == nil {
		saved.Server, saved.HasServer = value, true
	}
	if value, _, err := key.GetStringValue("ProxyOverride"); err == nil {
		saved.Override, saved.HasOver = value, true
	}
	if value, _, err := key.GetStringValue("AutoConfigURL"); err == nil {
		saved.AutoURL, saved.HasAuto = value, true
	}
	return saved
}

func writeProxySettings(saved savedProxy) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	errs := []error{key.SetDWordValue("ProxyEnable", saved.Enable)}
	setOrDelete := func(name, value string, present bool) {
		if present {
			errs = append(errs, key.SetStringValue(name, value))
		} else if err := key.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	setOrDelete("ProxyServer", saved.Server, saved.HasServer)
	setOrDelete("ProxyOverride", saved.Override, saved.HasOver)
	setOrDelete("AutoConfigURL", saved.AutoURL, saved.HasAuto)
	notifyProxyChange()
	return errors.Join(errs...)
}

func enableSystemProxy(httpPort, socksPort int) (func() error, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	previous := readProxySettings(key)
	key.Close()
	marker, err := proxyMarkerPath()
	if err != nil {
		return nil, err
	}
	// Never overwrite a marker left by a crashed core: it holds the real
	// original settings, not our own proxy.
	if _, statErr := os.Stat(marker); errors.Is(statErr, os.ErrNotExist) {
		encoded, _ := json.Marshal(previous)
		if err = os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
			return nil, err
		}
		if err = os.WriteFile(marker, encoded, 0o600); err != nil {
			return nil, err
		}
	}
	ours := savedProxy{
		Enable:    1,
		Server:    fmt.Sprintf("http=127.0.0.1:%d;https=127.0.0.1:%d;socks=127.0.0.1:%d", httpPort, httpPort, socksPort),
		HasServer: true,
		Override:  "localhost;127.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.2*;172.30.*;172.31.*;192.168.*;<local>",
		HasOver:   true,
	}
	if err = writeProxySettings(ours); err != nil {
		_ = restoreSystemProxy()
		return nil, err
	}
	return restoreSystemProxy, nil
}

// restoreSystemProxy puts back the settings saved in the marker, if any.
func restoreSystemProxy() error {
	marker, err := proxyMarkerPath()
	if err != nil {
		return err
	}
	encoded, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var previous savedProxy
	if err = json.Unmarshal(encoded, &previous); err != nil {
		_ = os.Remove(marker)
		return err
	}
	if err = writeProxySettings(previous); err != nil {
		return err
	}
	return os.Remove(marker)
}

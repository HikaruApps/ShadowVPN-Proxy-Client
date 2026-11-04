package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"
)

// The settings vault keeps the UI's subscription list (the links are access
// keys) out of WebView localStorage: the renderer hands it to the core, which
// stores it sealed with DPAPI for the current Windows user.
const vaultLimit = 512 * 1024

var vaultMu sync.Mutex

var vaultPath = func() (string, error) {
	directory, err := subscriptionCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(directory), "vault.bin"), nil
}

func loadVault() (string, error) {
	vaultMu.Lock()
	defer vaultMu.Unlock()
	path, err := vaultPath()
	if err != nil {
		return "", err
	}
	sealed, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	plain, err := unprotectCacheData(sealed)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(plain) || len(plain) > vaultLimit {
		return "", errors.New("settings vault is corrupted")
	}
	return string(plain), nil
}

func saveVault(data string) error {
	if len(data) > vaultLimit || !utf8.ValidString(data) {
		return errors.New("Слишком большой объём настроек")
	}
	vaultMu.Lock()
	defer vaultMu.Unlock()
	path, err := vaultPath()
	if err != nil {
		return err
	}
	if data == "" {
		if err = os.Remove(path); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	sealed, err := protectCacheData([]byte(data))
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

func (w *worker) handleVault(r request) {
	if r.Method == "vaultLoad" {
		data, err := loadVault()
		if err != nil {
			w.logf("settings vault read failed: %v", err)
			w.send(map[string]any{"id": r.ID, "ok": false, "error": "Не удалось прочитать сохранённые подписки"})
			return
		}
		w.send(map[string]any{"id": r.ID, "ok": true, "result": map[string]any{"data": data}})
		return
	}
	if err := saveVault(r.Data); err != nil {
		w.logf("settings vault write failed: %v", err)
		w.send(map[string]any{"id": r.ID, "ok": false, "error": "Не удалось сохранить подписки"})
		return
	}
	w.send(map[string]any{"id": r.ID, "ok": true, "result": map[string]any{}})
}

//go:build windows

package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var cacheEntropy = []byte("net.shadownet.shadowvpn:cache:v1")

func dataBlob(value []byte) *windows.DataBlob {
	if len(value) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(value)), Data: &value[0]}
}

func takeBlob(blob *windows.DataBlob) []byte {
	if blob.Data == nil {
		return nil
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(blob.Data)))
	return append([]byte(nil), unsafe.Slice(blob.Data, blob.Size)...)
}

func protectCacheData(plain []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptProtectData(dataBlob(plain), nil, dataBlob(cacheEntropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(&out), nil
}

func unprotectCacheData(sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, errors.New("empty cache")
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(dataBlob(sealed), nil, dataBlob(cacheEntropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(&out), nil
}

//go:build !windows

package main

// Non-Windows builds are only used for tests; the file itself is 0600.
func protectCacheData(plain []byte) ([]byte, error)    { return append([]byte(nil), plain...), nil }
func unprotectCacheData(sealed []byte) ([]byte, error) { return append([]byte(nil), sealed...), nil }

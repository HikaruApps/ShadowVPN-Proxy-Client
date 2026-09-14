//go:build windows && (amd64 || arm64)

package main

import (
	"testing"
	"unsafe"
)

func TestWFPStructLayouts(t *testing.T) {
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"FWP_BYTE_BLOB", unsafe.Sizeof(wfpByteBlob{}), 16},
		{"FWP_VALUE0", unsafe.Sizeof(wfpValue{}), 16},
		{"FWPM_ACTION0", unsafe.Sizeof(wfpAction{}), 20},
		{"FWPM_FILTER_CONDITION0", unsafe.Sizeof(wfpCondition{}), 40},
		{"FWPM_SESSION0", unsafe.Sizeof(wfpSession{}), 72},
		{"FWPM_PROVIDER0", unsafe.Sizeof(wfpProvider{}), 64},
		{"FWPM_SUBLAYER0", unsafe.Sizeof(wfpSublayer{}), 72},
		{"FWPM_FILTER0", unsafe.Sizeof(wfpFilter{}), 200},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s size = %d, want %d", test.name, test.got, test.want)
		}
	}
}

func TestEndpointCondition(t *testing.T) {
	condition, storage, layer, err := endpointCondition("203.0.113.7")
	if err != nil || storage != nil || condition.value.typeID != wfpUint32 || layer != wfpLayerConnectV4 {
		t.Fatalf("unexpected IPv4 condition: condition=%+v storage=%v layer=%v err=%v", condition, storage, layer, err)
	}
	condition, storage, layer, err = endpointCondition("2001:db8::7")
	if err != nil || storage == nil || condition.value.typeID != wfpByteArray16Type || layer != wfpLayerConnectV6 {
		t.Fatalf("unexpected IPv6 condition: condition=%+v storage=%v layer=%v err=%v", condition, storage, layer, err)
	}
	if _, _, _, err = endpointCondition("vpn.example.com"); err == nil {
		t.Fatal("unresolved endpoint must be rejected")
	}
}

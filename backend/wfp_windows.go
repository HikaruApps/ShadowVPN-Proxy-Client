//go:build windows && (amd64 || arm64)

// The WFP ABI declarations in this file are derived from the MIT-licensed
// WireGuard for Windows firewall implementation and Microsoft's fwpmu.h.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type wfpDataType uint
type wfpMatchType uint32
type wfpActionType uint32

const (
	wfpEmpty           wfpDataType = 0
	wfpUint8           wfpDataType = 1
	wfpUint16          wfpDataType = 2
	wfpUint32          wfpDataType = 3
	wfpUint64          wfpDataType = 4
	wfpByteArray16Type wfpDataType = 11
	wfpByteBlobType    wfpDataType = 12

	wfpMatchEqual       wfpMatchType = 0
	wfpMatchFlagsAllSet wfpMatchType = 6

	wfpActionBlock  wfpActionType = 0x00001001
	wfpActionPermit wfpActionType = 0x00001002

	wfpFilterClearActionRight uint32 = 0x00000008
	wfpSessionDynamic         uint32 = 0x00000001
	rpcAuthWinNT              uint32 = 10

	ipProtoUDP    = 17
	ipProtoICMPv6 = 58
)

type wfpByteBlob struct {
	size uint32
	data *byte
}

type wfpByteArray16 struct {
	data [16]byte
}

type wfpValue struct {
	typeID wfpDataType
	value  uintptr
}

type wfpDisplayData struct {
	name        *uint16
	description *uint16
}

type wfpAction struct {
	typeID     wfpActionType
	filterType windows.GUID
}

type wfpCondition struct {
	fieldKey windows.GUID
	match    wfpMatchType
	value    wfpValue
}

type wfpSession struct {
	sessionKey windows.GUID
	display    wfpDisplayData
	flags      uint32
	txnWaitMS  uint32
	processID  uint32
	sid        *windows.SID
	username   *uint16
	kernelMode uint8
}

type wfpProvider struct {
	providerKey windows.GUID
	display     wfpDisplayData
	flags       uint32
	data        wfpByteBlob
	serviceName *uint16
}

type wfpSublayer struct {
	sublayerKey windows.GUID
	display     wfpDisplayData
	flags       uint32
	providerKey *windows.GUID
	data        wfpByteBlob
	weight      uint16
}

// FWPM_FILTER0 layout for 64-bit Windows.
type wfpFilter struct {
	filterKey       windows.GUID
	display         wfpDisplayData
	flags           uint32
	providerKey     *windows.GUID
	providerData    wfpByteBlob
	layerKey        windows.GUID
	sublayerKey     windows.GUID
	weight          wfpValue
	conditionCount  uint32
	conditions      *wfpCondition
	action          wfpAction
	padding         [4]byte
	providerContext windows.GUID
	reserved        *windows.GUID
	filterID        uint64
	effectiveWeight wfpValue
}

var (
	wfpDLL                   = windows.NewLazySystemDLL("fwpuclnt.dll")
	procEngineOpen           = wfpDLL.NewProc("FwpmEngineOpen0")
	procEngineClose          = wfpDLL.NewProc("FwpmEngineClose0")
	procProviderAdd          = wfpDLL.NewProc("FwpmProviderAdd0")
	procSublayerAdd          = wfpDLL.NewProc("FwpmSubLayerAdd0")
	procFilterAdd            = wfpDLL.NewProc("FwpmFilterAdd0")
	procTransactionBegin     = wfpDLL.NewProc("FwpmTransactionBegin0")
	procTransactionCommit    = wfpDLL.NewProc("FwpmTransactionCommit0")
	procTransactionAbort     = wfpDLL.NewProc("FwpmTransactionAbort0")
	procGetAppIDFromFileName = wfpDLL.NewProc("FwpmGetAppIdFromFileName0")
	procFreeMemory           = wfpDLL.NewProc("FwpmFreeMemory0")

	wfpConditionLocalInterface = windows.GUID{Data1: 0x4cd62a49, Data2: 0x59c3, Data3: 0x4969, Data4: [8]byte{0xb7, 0xf3, 0xbd, 0xa5, 0xd3, 0x28, 0x90, 0xa4}}
	wfpConditionRemoteAddress  = windows.GUID{Data1: 0xb235ae9a, Data2: 0x1d64, Data3: 0x49b8, Data4: [8]byte{0xa4, 0x4c, 0x5f, 0xf3, 0xd9, 0x09, 0x50, 0x45}}
	wfpConditionProtocol       = windows.GUID{Data1: 0x3971ef2b, Data2: 0x623e, Data3: 0x4f9a, Data4: [8]byte{0x8c, 0xb1, 0x6e, 0x79, 0xb8, 0x06, 0xb9, 0xa7}}
	wfpConditionLocalPort      = windows.GUID{Data1: 0x0c1ba1af, Data2: 0x5765, Data3: 0x453f, Data4: [8]byte{0xaf, 0x22, 0xa8, 0xf7, 0x91, 0xac, 0x77, 0x5b}}
	wfpConditionRemotePort     = windows.GUID{Data1: 0xc35a604d, Data2: 0xd22b, Data3: 0x4e1a, Data4: [8]byte{0x91, 0xb4, 0x68, 0xf6, 0x74, 0xee, 0x67, 0x4b}}
	wfpConditionAppID          = windows.GUID{Data1: 0xd78e1e87, Data2: 0x8644, Data3: 0x4ea5, Data4: [8]byte{0x94, 0x37, 0xd8, 0x09, 0xec, 0xef, 0xc9, 0x71}}
	wfpConditionFlags          = windows.GUID{Data1: 0x632ce23b, Data2: 0x5167, Data3: 0x435c, Data4: [8]byte{0x86, 0xd7, 0xe9, 0x03, 0x68, 0x4a, 0xa8, 0x0c}}
	wfpLayerConnectV4          = windows.GUID{Data1: 0xc38d57d1, Data2: 0x05a7, Data3: 0x4c33, Data4: [8]byte{0x90, 0x4f, 0x7f, 0xbc, 0xee, 0xe6, 0x0e, 0x82}}
	wfpLayerConnectV6          = windows.GUID{Data1: 0x4a72393b, Data2: 0x319f, Data3: 0x44bc, Data4: [8]byte{0x84, 0xc3, 0xba, 0x54, 0xdc, 0xb3, 0xb6, 0xb4}}
)

func wfpCall(name string, proc *windows.LazyProc, args ...uintptr) error {
	result, _, _ := proc.Call(args...)
	if result == 0 {
		return nil
	}
	return fmt.Errorf("%s: %w (0x%08x)", name, syscall.Errno(result), uint32(result))
}

func wfpDisplay(name, description string) (wfpDisplayData, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return wfpDisplayData{}, err
	}
	descriptionPtr, err := windows.UTF16PtrFromString(description)
	if err != nil {
		return wfpDisplayData{}, err
	}
	return wfpDisplayData{name: namePtr, description: descriptionPtr}, nil
}

func wfpScalar(typeID wfpDataType, value uintptr) wfpValue {
	return wfpValue{typeID: typeID, value: value}
}

func openWFPEngine() (uintptr, error) {
	if unsafe.Sizeof(wfpFilter{}) != 200 || unsafe.Sizeof(wfpSession{}) != 72 || unsafe.Sizeof(wfpSublayer{}) != 72 {
		return 0, errors.New("unsupported WFP ABI layout")
	}
	display, err := wfpDisplay("ShadowVPN", "ShadowVPN dynamic Kill Switch session")
	if err != nil {
		return 0, err
	}
	session := wfpSession{display: display, flags: wfpSessionDynamic, txnWaitMS: windows.INFINITE}
	var engine uintptr
	err = wfpCall("FwpmEngineOpen0", procEngineOpen, 0, uintptr(rpcAuthWinNT), 0, uintptr(unsafe.Pointer(&session)), uintptr(unsafe.Pointer(&engine)))
	runtime.KeepAlive(session)
	return engine, err
}

func closeWFPEngine(engine uintptr) error {
	if engine == 0 {
		return nil
	}
	return wfpCall("FwpmEngineClose0", procEngineClose, engine)
}

func registerWFPObjects(engine uintptr) (windows.GUID, windows.GUID, error) {
	providerKey, err := windows.GenerateGUID()
	if err != nil {
		return windows.GUID{}, windows.GUID{}, err
	}
	sublayerKey, err := windows.GenerateGUID()
	if err != nil {
		return windows.GUID{}, windows.GUID{}, err
	}
	providerDisplay, err := wfpDisplay("ShadowVPN", "ShadowVPN Kill Switch provider")
	if err != nil {
		return windows.GUID{}, windows.GUID{}, err
	}
	provider := wfpProvider{providerKey: providerKey, display: providerDisplay}
	if err := wfpCall("FwpmProviderAdd0", procProviderAdd, engine, uintptr(unsafe.Pointer(&provider)), 0); err != nil {
		return windows.GUID{}, windows.GUID{}, err
	}
	sublayerDisplay, err := wfpDisplay("ShadowVPN Kill Switch", "ShadowVPN WFP permit and block filters")
	if err != nil {
		return windows.GUID{}, windows.GUID{}, err
	}
	sublayer := wfpSublayer{sublayerKey: sublayerKey, display: sublayerDisplay, providerKey: &providerKey, weight: ^uint16(0)}
	if err := wfpCall("FwpmSubLayerAdd0", procSublayerAdd, engine, uintptr(unsafe.Pointer(&sublayer)), 0); err != nil {
		return windows.GUID{}, windows.GUID{}, err
	}
	runtime.KeepAlive(provider)
	runtime.KeepAlive(sublayer)
	return providerKey, sublayerKey, nil
}

func addWFPFilter(engine uintptr, providerKey, sublayerKey windows.GUID, name string, layer windows.GUID, weight uint8, action wfpActionType, conditions []wfpCondition) error {
	display, err := wfpDisplay(name, "Managed by ShadowVPN")
	if err != nil {
		return err
	}
	var conditionPtr *wfpCondition
	if len(conditions) > 0 {
		conditionPtr = &conditions[0]
	}
	flags := uint32(0)
	if action == wfpActionPermit {
		flags = wfpFilterClearActionRight
	}
	filter := wfpFilter{
		display:        display,
		flags:          flags,
		providerKey:    &providerKey,
		layerKey:       layer,
		sublayerKey:    sublayerKey,
		weight:         wfpScalar(wfpUint8, uintptr(weight)),
		conditionCount: uint32(len(conditions)),
		conditions:     conditionPtr,
		action:         wfpAction{typeID: action},
	}
	var id uint64
	err = wfpCall("FwpmFilterAdd0", procFilterAdd, engine, uintptr(unsafe.Pointer(&filter)), 0, uintptr(unsafe.Pointer(&id)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(conditions)
	return err
}

func currentProcessAppID() (*wfpByteBlob, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	path, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return nil, err
	}
	var appID *wfpByteBlob
	if err := wfpCall("FwpmGetAppIdFromFileName0", procGetAppIDFromFileName, uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&appID))); err != nil {
		return nil, err
	}
	return appID, nil
}

func freeWFPAppID(appID **wfpByteBlob) {
	if appID != nil && *appID != nil {
		_, _, _ = procFreeMemory.Call(uintptr(unsafe.Pointer(appID)))
	}
}

func endpointCondition(address string) (wfpCondition, *wfpByteArray16, windows.GUID, error) {
	ip := net.ParseIP(address)
	if ip == nil {
		return wfpCondition{}, nil, windows.GUID{}, errors.New("WFP endpoint must be a resolved IP address")
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return wfpCondition{
			fieldKey: wfpConditionRemoteAddress,
			match:    wfpMatchEqual,
			value:    wfpScalar(wfpUint32, uintptr(binary.BigEndian.Uint32(ipv4))),
		}, nil, wfpLayerConnectV4, nil
	}
	bytes16 := ip.To16()
	if bytes16 == nil {
		return wfpCondition{}, nil, windows.GUID{}, errors.New("invalid WFP endpoint address")
	}
	array := &wfpByteArray16{}
	copy(array.data[:], bytes16)
	return wfpCondition{
		fieldKey: wfpConditionRemoteAddress,
		match:    wfpMatchEqual,
		value:    wfpValue{typeID: wfpByteArray16Type, value: uintptr(unsafe.Pointer(array))},
	}, array, wfpLayerConnectV6, nil
}

func installWFPFilters(engine uintptr, providerKey, sublayerKey windows.GUID, tunnelLUID uint64, endpoints []proxyEndpoint) error {
	appID, err := currentProcessAppID()
	if err != nil {
		return err
	}
	defer freeWFPAppID(&appID)
	for index, candidate := range endpoints {
		endpoint, endpointBytes, endpointLayer, err := endpointCondition(candidate.Address)
		if err != nil {
			return err
		}
		endpointConditions := []wfpCondition{
			{fieldKey: wfpConditionAppID, match: wfpMatchEqual, value: wfpValue{typeID: wfpByteBlobType, value: uintptr(unsafe.Pointer(appID))}},
			endpoint,
			{fieldKey: wfpConditionRemotePort, match: wfpMatchEqual, value: wfpScalar(wfpUint16, uintptr(candidate.Port))},
		}
		name := fmt.Sprintf("Permit Xray VPN endpoint %d", index+1)
		if err := addWFPFilter(engine, providerKey, sublayerKey, name, endpointLayer, 15, wfpActionPermit, endpointConditions); err != nil {
			return err
		}
		runtime.KeepAlive(endpointBytes)
	}

	tunnelCondition := []wfpCondition{{
		fieldKey: wfpConditionLocalInterface,
		match:    wfpMatchEqual,
		value:    wfpValue{typeID: wfpUint64, value: uintptr(unsafe.Pointer(&tunnelLUID))},
	}}
	for _, definition := range []struct {
		name  string
		layer windows.GUID
	}{
		{"Permit ShadowVPN TUN IPv4", wfpLayerConnectV4},
		{"Permit ShadowVPN TUN IPv6", wfpLayerConnectV6},
	} {
		if err := addWFPFilter(engine, providerKey, sublayerKey, definition.name, definition.layer, 14, wfpActionPermit, tunnelCondition); err != nil {
			return err
		}
	}
	runtime.KeepAlive(tunnelLUID)

	loopbackCondition := []wfpCondition{{
		fieldKey: wfpConditionFlags,
		match:    wfpMatchFlagsAllSet,
		value:    wfpScalar(wfpUint32, 1),
	}}
	for _, definition := range []struct {
		name  string
		layer windows.GUID
	}{
		{"Permit loopback IPv4", wfpLayerConnectV4},
		{"Permit loopback IPv6", wfpLayerConnectV6},
	} {
		if err := addWFPFilter(engine, providerKey, sublayerKey, definition.name, definition.layer, 13, wfpActionPermit, loopbackCondition); err != nil {
			return err
		}
	}

	dhcpV4 := []wfpCondition{
		{fieldKey: wfpConditionProtocol, match: wfpMatchEqual, value: wfpScalar(wfpUint8, ipProtoUDP)},
		{fieldKey: wfpConditionLocalPort, match: wfpMatchEqual, value: wfpScalar(wfpUint16, 68)},
		{fieldKey: wfpConditionRemotePort, match: wfpMatchEqual, value: wfpScalar(wfpUint16, 67)},
		{fieldKey: wfpConditionRemoteAddress, match: wfpMatchEqual, value: wfpScalar(wfpUint32, 0xffffffff)},
	}
	if err := addWFPFilter(engine, providerKey, sublayerKey, "Permit DHCP IPv4", wfpLayerConnectV4, 12, wfpActionPermit, dhcpV4); err != nil {
		return err
	}
	dhcpV6 := []wfpCondition{
		{fieldKey: wfpConditionProtocol, match: wfpMatchEqual, value: wfpScalar(wfpUint8, ipProtoUDP)},
		{fieldKey: wfpConditionLocalPort, match: wfpMatchEqual, value: wfpScalar(wfpUint16, 546)},
		{fieldKey: wfpConditionRemotePort, match: wfpMatchEqual, value: wfpScalar(wfpUint16, 547)},
	}
	if err := addWFPFilter(engine, providerKey, sublayerKey, "Permit DHCP IPv6", wfpLayerConnectV6, 12, wfpActionPermit, dhcpV6); err != nil {
		return err
	}
	for _, icmpType := range []uintptr{133, 135, 136} {
		ndp := []wfpCondition{
			{fieldKey: wfpConditionProtocol, match: wfpMatchEqual, value: wfpScalar(wfpUint8, ipProtoICMPv6)},
			{fieldKey: wfpConditionLocalPort, match: wfpMatchEqual, value: wfpScalar(wfpUint16, icmpType)},
			{fieldKey: wfpConditionRemotePort, match: wfpMatchEqual, value: wfpScalar(wfpUint16, 0)},
		}
		if err := addWFPFilter(engine, providerKey, sublayerKey, fmt.Sprintf("Permit NDP type %d", icmpType), wfpLayerConnectV6, 12, wfpActionPermit, ndp); err != nil {
			return err
		}
	}

	if err := addWFPFilter(engine, providerKey, sublayerKey, "Block direct outbound IPv4", wfpLayerConnectV4, 0, wfpActionBlock, nil); err != nil {
		return err
	}
	return addWFPFilter(engine, providerKey, sublayerKey, "Block direct outbound IPv6", wfpLayerConnectV6, 0, wfpActionBlock, nil)
}

func enableWFPKillSwitch(tunnelLUID uint64, endpoints []proxyEndpoint) (uintptr, error) {
	if tunnelLUID == 0 {
		return 0, errors.New("ShadowVPN TUN interface has no LUID")
	}
	if len(endpoints) == 0 {
		return 0, errors.New("no VPN endpoints were provided")
	}
	for _, endpoint := range endpoints {
		if net.ParseIP(endpoint.Address) == nil {
			return 0, errors.New("WFP endpoint must be a resolved IP address")
		}
		if endpoint.Port < 1 || endpoint.Port > 65535 {
			return 0, errors.New("invalid VPN endpoint port")
		}
	}
	engine, err := openWFPEngine()
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = closeWFPEngine(engine)
		}
	}()
	if err := wfpCall("FwpmTransactionBegin0", procTransactionBegin, engine, 0); err != nil {
		return 0, err
	}
	abort := true
	defer func() {
		if abort {
			_ = wfpCall("FwpmTransactionAbort0", procTransactionAbort, engine)
		}
	}()
	providerKey, sublayerKey, err := registerWFPObjects(engine)
	if err != nil {
		return 0, err
	}
	if err := installWFPFilters(engine, providerKey, sublayerKey, tunnelLUID, endpoints); err != nil {
		return 0, err
	}
	if err := wfpCall("FwpmTransactionCommit0", procTransactionCommit, engine); err != nil {
		return 0, err
	}
	abort = false
	committed = true
	return engine, nil
}

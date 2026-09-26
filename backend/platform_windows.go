//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

const (
	tunnelAdapterName           = "ShadowVPN"
	legacyKillSwitchAdapterName = "ShadowVPN Kill Switch"
)

type killSwitchGuard struct {
	engine      uintptr
	providerKey windows.GUID
	sublayerKey windows.GUID
	mu          sync.Mutex
	endpoints   map[proxyEndpoint]struct{}
}

func checkPlatform() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("Для TUN запустите приложение от имени администратора через start-tun.cmd")
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	if _, e = os.Stat(filepath.Join(filepath.Dir(exe), "wintun.dll")); e != nil {
		return errors.New("Рядом с shadowvpn-core.exe отсутствует wintun.dll. Запустите build-windows.ps1")
	}
	return nil
}

func physicalOutboundInterface() (string, error) {
	routes, err := winipcfg.GetIPForwardTable2(windows.AF_UNSPEC)
	if err != nil {
		return "", err
	}
	bestMetric := ^uint32(0)
	bestName := ""
	bestWiFiMetric := ^uint32(0)
	bestWiFiName := ""
	for i := range routes {
		route := &routes[i]
		if route.DestinationPrefix.PrefixLength != 0 {
			continue
		}
		ifRow, err := route.InterfaceLUID.Interface()
		if err != nil || ifRow.OperStatus != winipcfg.IfOperStatusUp {
			continue
		}
		name := ifRow.Alias()
		if name == "" || strings.HasPrefix(strings.ToLower(name), "shadowvpn") || ifRow.Type == windows.IF_TYPE_SOFTWARE_LOOPBACK || ifRow.Type == windows.IF_TYPE_TUNNEL {
			continue
		}
		ipInterface, err := route.InterfaceLUID.IPInterface(windows.AF_INET)
		if err != nil {
			ipInterface, err = route.InterfaceLUID.IPInterface(windows.AF_INET6)
			if err != nil {
				continue
			}
		}
		metric := route.Metric + ipInterface.Metric
		if ifRow.Type == windows.IF_TYPE_IEEE80211 {
			if metric < bestWiFiMetric {
				bestWiFiMetric, bestWiFiName = metric, name
			}
		} else if metric < bestMetric {
			bestMetric, bestName = metric, name
		}
	}
	if bestWiFiName != "" {
		return bestWiFiName, nil
	}
	if bestName == "" {
		return "", errors.New("physical default route not found")
	}
	return bestName, nil
}

func tunnelInterfaceLUID() (uint64, error) {
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for {
		adapter, err := wintun.OpenAdapter(tunnelAdapterName)
		if err == nil {
			luid := adapter.LUID()
			closeErr := adapter.Close()
			if closeErr != nil {
				return 0, closeErr
			}
			return luid, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return 0, lastErr
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func startKillSwitchGuard(endpoints []proxyEndpoint) (*killSwitchGuard, error) {
	luid, err := tunnelInterfaceLUID()
	if err != nil {
		return nil, err
	}
	session, err := enableWFPKillSwitch(luid, endpoints)
	if err != nil {
		return nil, err
	}
	allowed := make(map[proxyEndpoint]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		allowed[endpoint] = struct{}{}
	}
	return &killSwitchGuard{
		engine: session.engine, providerKey: session.providerKey,
		sublayerKey: session.sublayerKey, endpoints: allowed,
	}, nil
}

func (guard *killSwitchGuard) AllowEndpoint(endpoint proxyEndpoint) error {
	if guard == nil || guard.engine == 0 {
		return nil
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if _, exists := guard.endpoints[endpoint]; exists {
		return nil
	}
	if err := installWFPEndpointFilters(guard.engine, guard.providerKey, guard.sublayerKey, []proxyEndpoint{endpoint}, len(guard.endpoints)); err != nil {
		return err
	}
	guard.endpoints[endpoint] = struct{}{}
	return nil
}

func (guard *killSwitchGuard) Close() error {
	if guard == nil || guard.engine == 0 {
		return nil
	}
	err := closeWFPEngine(guard.engine)
	guard.engine = 0
	return err
}

func interfaceDialer(interfaceName string, timeout time.Duration) (*net.Dialer, error) {
	dialer := &net.Dialer{Timeout: timeout}
	if interfaceName == "" {
		return dialer, nil
	}
	physicalInterface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, err
	}
	dialer.Control = func(_, address string, raw syscall.RawConn) error {
		var socketErr error
		controlErr := raw.Control(func(fd uintptr) {
			host, _, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				socketErr = splitErr
				return
			}
			if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
				var bytes [4]byte
				binary.BigEndian.PutUint32(bytes[:], uint32(physicalInterface.Index))
				index := *(*uint32)(unsafe.Pointer(&bytes[0]))
				socketErr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, 31, int(index))
			} else {
				socketErr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IPV6, 31, physicalInterface.Index)
			}
		})
		return errors.Join(controlErr, socketErr)
	}
	return dialer, nil
}

// Version 0.8 used a second Wintun adapter as a route guard. Remove any
// leftover routes once during migration; current WFP sessions are dynamic and
// Windows removes their filters automatically when shadowvpn-core exits.
func cleanupStaleKillSwitch() error {
	adapter, err := wintun.OpenAdapter(legacyKillSwitchAdapterName)
	if err != nil {
		return nil
	}
	luid := winipcfg.LUID(adapter.LUID())
	flushErr := errors.Join(
		luid.FlushRoutes(windows.AF_INET),
		luid.FlushRoutes(windows.AF_INET6),
		luid.FlushIPAddresses(windows.AF_INET),
		luid.FlushIPAddresses(windows.AF_INET6),
		luid.FlushDNS(windows.AF_INET),
		luid.FlushDNS(windows.AF_INET6),
	)
	return errors.Join(flushErr, adapter.Close())
}

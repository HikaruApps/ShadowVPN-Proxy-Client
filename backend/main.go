package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/app/observatory"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/extension"
	_ "github.com/xtls/xray-core/main/distro/all"
)

type request struct {
	ID             int      `json:"id"`
	Method         string   `json:"method"`
	URL            string   `json:"url"`
	URLs           []string `json:"urls"`
	Reason         string   `json:"reason"`
	ProfileID      string   `json:"profileId"`
	DNS            string   `json:"dns"`
	DNSServers     []string `json:"dnsServers"`
	DNSDoH         string   `json:"dnsDoh"`
	Fragment       bool     `json:"fragmentation"`
	KillSwitch     bool     `json:"killSwitch"`
	AutoProfileIDs []string `json:"autoProfileIds"`
	Masked         bool     `json:"masked"`
	PingMethod     string   `json:"pingMethod"`
	RouteMode      string   `json:"routeMode"`
	DirectDomains  []string `json:"directDomains"`
	GeoIPURL       string   `json:"geoIpUrl"`
	GeoSiteURL     string   `json:"geoSiteUrl"`
	ConnectionMode string   `json:"connectionMode"`
	AutoReconnect  bool     `json:"autoReconnect"`
	Data           string   `json:"data"`
}

type connectionInfo struct {
	ProfileID string `json:"profileId"`
	Name      string `json:"name"`
}

func connectionInfoForProfile(profile Profile) connectionInfo {
	return connectionInfo{ProfileID: profile.ID, Name: profile.Name}
}

type worker struct {
	instance      *core.Instance
	profiles      []Profile
	activeProfile connectionInfo
	activeMu      sync.RWMutex
	autoCancel    context.CancelFunc
	autoWG        sync.WaitGroup
	trafficCancel context.CancelFunc
	trafficWG     sync.WaitGroup
	killSwitch    *killSwitchGuard
	// System-proxy mode: puts the user's previous proxy settings back.
	proxyRestore func() error
	// Auto-reconnect: the last successful connect request, the tunnel health
	// monitor, and requests the worker sends to itself.
	lastConnect      *request
	healthCancel     context.CancelFunc
	healthWG         sync.WaitGroup
	internal         chan request
	reconnectTimer   *time.Timer
	reconnectAttempt int
	enc              *json.Encoder
	mu               sync.Mutex
	state            string
	diag             *log.Logger
}

func (w *worker) send(v any) { w.mu.Lock(); defer w.mu.Unlock(); _ = w.enc.Encode(v) }
func (w *worker) logf(format string, values ...any) {
	if w.diag != nil {
		w.diag.Printf("[shadowvpn] "+format, values...)
	}
}
func (w *worker) setState(s string) {
	w.state = s
	w.logf("state=%s", s)
	w.send(map[string]any{"event": "state", "state": s})
}
func (w *worker) clearActiveProfile() {
	w.activeMu.Lock()
	w.activeProfile = connectionInfo{}
	w.activeMu.Unlock()
}
func (w *worker) setActiveProfile(profile Profile) {
	w.activeMu.Lock()
	w.activeProfile = connectionInfoForProfile(profile)
	w.activeMu.Unlock()
}
func (w *worker) currentActiveProfile() connectionInfo {
	w.activeMu.RLock()
	defer w.activeMu.RUnlock()
	return w.activeProfile
}
func (w *worker) stopAutoMonitor() {
	if w.autoCancel == nil {
		return
	}
	w.autoCancel()
	w.autoCancel = nil
	w.autoWG.Wait()
}
func bestObservedProfile(profiles []Profile, statuses []*observatory.OutboundStatus) (Profile, int64, bool) {
	byTag := make(map[string]*observatory.OutboundStatus, len(statuses))
	for _, status := range statuses {
		if status != nil {
			byTag[status.OutboundTag] = status
		}
	}
	var best Profile
	var bestDelay int64
	found := false
	for _, profile := range profiles {
		status := byTag["auto-"+profile.ID]
		if status == nil || !status.Alive || status.Delay < 0 {
			continue
		}
		if !found || status.Delay < bestDelay {
			best = profile
			bestDelay = status.Delay
			found = true
		}
	}
	return best, bestDelay, found
}
func (w *worker) startAutoMonitor(parent context.Context, instance *core.Instance, profiles []Profile, initial Profile) {
	feature := instance.GetFeature(extension.ObservatoryType())
	observer, ok := feature.(extension.Observatory)
	if !ok {
		w.logf("auto UI monitor unavailable; Xray leastPing balancer remains active")
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.autoCancel = cancel
	w.autoWG.Add(1)
	go func() {
		defer w.autoWG.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		currentID := initial.ID
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				report, err := observer.GetObservation(ctx)
				if err != nil {
					continue
				}
				result, ok := report.(*observatory.ObservationResult)
				if !ok {
					continue
				}
				selected, delay, ok := bestObservedProfile(profiles, result.Status)
				if !ok || selected.ID == currentID {
					continue
				}
				previousID := currentID
				currentID = selected.ID
				w.setActiveProfile(selected)
				w.logf("auto route changed previous_profile_id=%s profile_id=%s name=%q latency_ms=%d", previousID, selected.ID, selected.Name, delay)
				w.send(map[string]any{
					"event":     "profile",
					"profile":   connectionInfoForProfile(selected),
					"reason":    "auto-fallback",
					"latencyMs": delay,
				})
			}
		}
	}()
}
func (w *worker) closeTunnel(preserveKillSwitch bool) error {
	w.stopHealthMonitor()
	w.stopAutoMonitor()
	w.stopTrafficMonitor()
	w.clearActiveProfile()
	var closeErr error
	if w.proxyRestore != nil {
		w.logf("restoring the previous system proxy settings")
		closeErr = w.proxyRestore()
		w.proxyRestore = nil
	}
	if w.instance == nil {
		if w.killSwitch != nil && !preserveKillSwitch {
			w.logf("disabling kill switch WFP policy")
			closeErr = w.killSwitch.Close()
			w.killSwitch = nil
		}
		return closeErr
	}
	v := w.instance
	w.instance = nil
	closeErr = errors.Join(closeErr, w.closeInstance(v))
	if w.killSwitch != nil && !preserveKillSwitch {
		w.logf("disabling kill switch WFP policy")
		closeErr = errors.Join(closeErr, w.killSwitch.Close())
		w.killSwitch = nil
	}
	return closeErr
}

func (w *worker) close() error {
	return w.closeTunnel(false)
}

// Xray's TUN teardown can block inside Wintun/gVisor. A blocked worker would
// keep the adapter, its routes and the WFP session alive until a reboot, so a
// hung close terminates the core instead: Windows then releases the
// process-owned Wintun adapter and the dynamic WFP session, and the desktop
// shell starts a fresh core on the next request.
const tunnelCloseTimeout = 15 * time.Second

var exitProcess = os.Exit

func (w *worker) closeInstance(instance *core.Instance) error {
	done := make(chan error, 1)
	go func() { done <- instance.Close() }()
	select {
	case err := <-done:
		return err
	case <-time.After(tunnelCloseTimeout):
		w.logf("Xray close did not finish within %s; terminating core so Windows releases TUN and WFP", tunnelCloseTimeout)
		if w.proxyRestore != nil {
			_ = w.proxyRestore()
			w.proxyRestore = nil
		}
		if w.killSwitch != nil {
			_ = w.killSwitch.Close()
			w.killSwitch = nil
		}
		w.send(map[string]any{"event": "state", "state": "disconnected"})
		exitProcess(3)
		return errors.New("Xray close timed out")
	}
}

// shutdown closes the tunnel with a hard deadline so the process can never
// outlive the desktop shell while holding network state.
func (w *worker) shutdown() {
	watchdog := time.AfterFunc(tunnelCloseTimeout+5*time.Second, func() {
		w.logf("shutdown did not finish in time; forcing exit")
		exitProcess(4)
	})
	defer watchdog.Stop()
	if err := w.close(); err != nil {
		w.logf("shutdown cleanup failed: %v", err)
	}
}
func profilesByID(profiles []Profile, ids []string) []Profile {
	if len(ids) == 0 {
		return profiles
	}
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	result := make([]Profile, 0, len(ids))
	for _, profile := range profiles {
		if _, ok := allowed[profile.ID]; ok {
			result = append(result, profile)
		}
	}
	return result
}

func (w *worker) connect(ctx context.Context, r request) error {
	id, dnsID, customDNS, autoProfileIDs, directDomains := r.ProfileID, r.DNS, r.DNSServers, r.AutoProfileIDs, r.DirectDomains
	dnsDoH, routeMode, geoIPURL, geoSiteURL := r.DNSDoH, r.RouteMode, r.GeoIPURL, r.GeoSiteURL
	fragmentation, killSwitch := r.Fragment, r.KillSwitch
	w.logf("connect requested profile_id=%s mode=%s", id, connectionModeName(r.ConnectionMode))
	validatedDNSID, dns, err := selectedDNSWithDoH(dnsID, customDNS, dnsDoH)
	if err != nil {
		w.logf("connect rejected: invalid DNS preset")
		return err
	}
	routing, routingErr := newRoutingOptions(routeMode, directDomains, geoIPURL, geoSiteURL)
	if routingErr != nil {
		w.logf("connect rejected: invalid routing settings: %v", routingErr)
		return routingErr
	}
	routing.Proxy = r.ConnectionMode == "proxy"
	if killSwitch && routing.Proxy {
		w.logf("connect rejected: kill switch needs the TUN mode")
		return errors.New("Kill Switch работает только в режиме TUN. Выключите его или смените режим подключения")
	}
	if killSwitch && routing.Mode != "full" {
		w.logf("connect rejected: kill switch cannot be combined with split routing mode=%s", routing.Mode)
		return errors.New("Kill Switch отключает прямой трафик, поэтому выключите его для выборочной маршрутизации")
	}
	w.logf("DNS selected id=%s name=%s; routing_mode=%s domain_rules=%d ip_rules=%d geodata=%t; TLS fragmentation=%t; kill_switch=%t", validatedDNSID, dns.Name, routing.Mode, len(routing.DirectDomains), len(routing.IPRules), routing.NeedsGeoIP || routing.NeedsGeoSite, fragmentation, killSwitch)
	if runtime.GOOS != "windows" {
		w.logf("connect rejected: unsupported platform=%s", runtime.GOOS)
		return errors.New("Эта сборка клиента поддерживает TUN только на Windows")
	}
	if w.instance != nil {
		return errors.New("Сначала отключите текущее соединение")
	}
	w.clearActiveProfile()
	var selected *Profile
	isAuto := isAutoProfileID(id)
	var autoProfiles []Profile
	var allowedEndpoints []proxyEndpoint
	if isAuto {
		candidates := profilesByID(w.profiles, autoProfileIDs)
		if id == autoNoRUProfileID {
			candidates = withoutRussianProfiles(candidates)
		}
		if len(autoProfileIDs) > 0 && len(candidates) == 0 {
			w.logf("auto group rejected: none of requested profiles exist requested=%d", len(autoProfileIDs))
			return errors.New("В выбранном Auto не осталось подходящих серверов из текущей подписки")
		}
		w.logf("auto selection started profiles=%d restricted=%t", len(candidates), len(autoProfileIDs) > 0)
		options := pingOptions{}
		if w.killSwitch != nil {
			options.OutboundInterface, err = physicalOutboundInterface()
			if err != nil {
				return errors.New("Kill Switch не нашёл физический сетевой интерфейс для переключения")
			}
			options.AllowEndpoint = w.killSwitch.AllowEndpoint
		}
		results := pingProfilesWithMethodOptions(ctx, candidates, "auto", options)
		_, best, ok := fastestProfile(candidates, results)
		if !ok {
			w.logf("auto selection failed: no reachable profiles")
			return errors.New("Авто не нашёл доступных серверов. Запустите проверку задержки")
		}
		prepareCtx, prepareCancel := context.WithTimeout(ctx, 12*time.Second)
		var skipped []string
		autoProfiles, allowedEndpoints, skipped, err = prepareAutoProfiles(prepareCtx, candidates, results)
		prepareCancel()
		if err != nil {
			w.logf("auto candidate preparation failed: %v", err)
			return errors.New("Авто не удалось подготовить серверы до запуска TUN")
		}
		for _, name := range skipped {
			w.logf("auto observer skipped unresolved profile name=%q", name)
		}
		selected = &autoProfiles[0]
		w.logf("auto selection completed profile_id=%s name=%q latency_ms=%d", selected.ID, selected.Name, best.LatencyMS)
		w.logf("continuous auto monitor prepared candidates=%d interval=%s", len(autoProfiles), autoProbeInterval)
	} else {
		for i := range w.profiles {
			if w.profiles[i].ID == id {
				selected = &w.profiles[i]
				break
			}
		}
	}
	if selected == nil {
		w.logf("connect rejected: profile not found")
		return errors.New("Выберите сервер из загруженной подписки")
	}
	w.logf("profile selected name=%q protocol=%s transport=%s endpoint=%s", selected.Name, selected.Protocol, selected.Transport, net.JoinHostPort(selected.Address, strconv.Itoa(selected.Port)))
	if routing.Proxy {
		if err := localProxyPortsFree(); err != nil {
			w.logf("system proxy ports busy: %v", err)
			return err
		}
	} else if err := checkPlatform(); err != nil {
		w.logf("platform check failed: %v", err)
		return err
	}
	w.logf("platform check passed; resolving proxy endpoint before TUN startup")
	resolveCtx, resolveCancel := context.WithTimeout(ctx, 8*time.Second)
	resolved, bootstrap, e := resolveProfileEndpoint(resolveCtx, *selected)
	resolveCancel()
	if e != nil {
		w.logf("endpoint bootstrap DNS failed host=%s error=%v", selected.Address, e)
		return errors.New("Не удалось определить IP VPN-сервера до запуска TUN")
	}
	w.logf("endpoint bootstrap DNS passed host=%s candidates=%s", bootstrap.OriginalAddress, strings.Join(bootstrap.Candidates, ","))
	pingCtx, pingCancel := context.WithTimeout(ctx, 6*time.Second)
	pingOptions := pingOptions{}
	if w.killSwitch != nil {
		pingOptions.OutboundInterface, e = physicalOutboundInterface()
		if e != nil {
			pingCancel()
			return errors.New("Kill Switch не нашёл физический сетевой интерфейс для переключения")
		}
		pingOptions.AllowEndpoint = w.killSwitch.AllowEndpoint
	}
	var latency int64
	var pingErr error
	for index, candidate := range bootstrap.Candidates {
		if candidate != resolved.Address {
			originalHost := bootstrap.OriginalAddress
			if net.ParseIP(originalHost) != nil {
				originalHost = ""
			}
			resolved, e = profileWithEndpoint(*selected, candidate, originalHost)
			if e != nil {
				pingErr = e
				break
			}
		}
		endpoint := net.JoinHostPort(resolved.Address, strconv.Itoa(resolved.Port))
		w.logf("endpoint bootstrap check started protocol=%s endpoint=%s candidate=%d/%d", selected.Protocol, endpoint, index+1, len(bootstrap.Candidates))
		if selected.Protocol == "hysteria" {
			latency, _, pingErr = httpPingProfileWithOptions(pingCtx, resolved, "head", pingOptions)
		} else {
			latency, _, pingErr = tcpPingEndpointWithOptions(pingCtx, resolved.Address, resolved.Port, pingOptions)
		}
		if pingErr == nil {
			bootstrap.SelectedAddress = candidate
			w.logf("endpoint bootstrap check passed protocol=%s endpoint=%s latency_ms=%d", selected.Protocol, endpoint, latency)
			break
		}
		w.logf("endpoint bootstrap candidate failed protocol=%s endpoint=%s error=%v", selected.Protocol, endpoint, pingErr)
	}
	pingCancel()
	if pingErr != nil {
		w.logf("endpoint bootstrap check failed protocol=%s candidates=%s error=%v", selected.Protocol, strings.Join(bootstrap.Candidates, ","), pingErr)
		return errors.New("VPN-сервер разрешён, но не прошёл проверку доступности")
	}
	if isAuto {
		autoProfiles[0] = resolved
		if len(allowedEndpoints) > 0 {
			allowedEndpoints[0] = proxyEndpoint{Address: resolved.Address, Port: resolved.Port}
		}
	} else {
		allowedEndpoints = []proxyEndpoint{{Address: resolved.Address, Port: resolved.Port}}
	}
	outboundInterface := "auto"
	if killSwitch {
		outboundInterface, e = physicalOutboundInterface()
		if e != nil {
			w.logf("kill switch could not resolve physical interface: %v", e)
			return errors.New("Kill Switch не нашёл физический сетевой интерфейс")
		}
		w.logf("kill switch pinned Xray outbound interface=%s", outboundInterface)
	}
	if routing.NeedsGeoIP || routing.NeedsGeoSite {
		w.logf("GeoData bootstrap started geoip=%t geosite=%t", routing.NeedsGeoIP, routing.NeedsGeoSite)
		geoDataCtx, geoDataCancel := context.WithTimeout(ctx, 45*time.Second)
		e = prepareGeoData(geoDataCtx, &routing)
		geoDataCancel()
		if e != nil {
			w.logf("GeoData bootstrap failed: %v", e)
			return e
		}
		w.logf("GeoData bootstrap completed; native daily refresh enabled")
	}
	w.logf("creating Xray TUN configuration with pre-resolved endpoint=%s", bootstrap.SelectedAddress)
	var config []byte
	if isAuto {
		config, e = makeAutoConfigWithRoutingAndDoH(autoProfiles, validatedDNSID, dns.Servers, dns.DoHURL, fragmentation, outboundInterface, routing)
	} else {
		config, e = makeConfigWithRoutingAndDoH(resolved, validatedDNSID, dns.Servers, dns.DoHURL, fragmentation, outboundInterface, routing)
	}
	if e != nil {
		w.logf("configuration generation failed: %v", e)
		return errors.New("Не удалось создать конфигурацию")
	}
	w.setState("connecting")
	conf, e := core.LoadConfig("json", bytes.NewReader(config))
	if e != nil {
		w.logf("Xray configuration rejected: %v", e)
		w.setState("disconnected")
		return errors.New("Xray отклонил конфигурацию сервера")
	}
	instance, e := core.New(conf)
	if e != nil {
		w.logf("Xray instance creation failed: %v", e)
		w.setState("disconnected")
		return errors.New("Не удалось создать Xray TUN. Проверьте права администратора и wintun.dll")
	}
	w.instance = instance
	w.logf("starting Xray instance and Wintun adapter")
	if e = instance.Start(); e != nil {
		w.logf("Xray start failed: %v", e)
		_ = w.close()
		w.setState("disconnected")
		return errors.New("Ошибка запуска TUN. Проверьте права администратора и сетевые адаптеры")
	}
	if killSwitch {
		if w.killSwitch == nil {
			w.killSwitch, e = startKillSwitchGuard(allowedEndpoints)
		} else {
			for _, endpoint := range allowedEndpoints {
				if e = w.killSwitch.AllowEndpoint(endpoint); e != nil {
					break
				}
			}
			if e == nil {
				var tunnelLUID uint64
				tunnelLUID, e = tunnelInterfaceLUID()
				if e == nil {
					e = w.killSwitch.AllowTunnel(tunnelLUID)
				}
			}
		}
		if e != nil {
			w.logf("kill switch activation failed: %v", e)
			_ = w.close()
			w.setState("disconnected")
			return errors.New("Не удалось включить Kill Switch. Соединение остановлено без изменения сети")
		}
		w.logf("kill switch WFP policy enabled tunnel=ShadowVPN endpoints=%d", len(allowedEndpoints))
	}
	w.logf("Xray started; checking HTTPS through its dispatcher")
	// Real TLS request through Xray's dispatcher, not the system's direct connection.
	probeCtx, cancel := context.WithTimeout(ctx, 24*time.Second)
	defer cancel()
	e = probeThroughInstance(probeCtx, instance, 24*time.Second)
	if e != nil {
		w.logf("connectivity probe failed: %v", e)
		_ = w.close()
		w.setState("disconnected")
		return errors.New("TUN запущен, но проверка интернета через сервер не прошла. Соединение отключено")
	}
	w.logf("connectivity probe passed; tunnel is ready")
	if routing.Proxy {
		restore, proxyErr := enableSystemProxy(localHTTPPort, localSocksPort)
		if proxyErr != nil {
			w.logf("system proxy activation failed: %v", proxyErr)
			_ = w.close()
			w.setState("disconnected")
			return errors.New("Не удалось включить системный прокси Windows")
		}
		w.proxyRestore = restore
		w.logf("system proxy enabled http=127.0.0.1:%d socks=127.0.0.1:%d", localHTTPPort, localSocksPort)
	}
	w.setActiveProfile(*selected)
	w.setState("connected")
	w.startTrafficMonitor(ctx, instance, routing.inboundTags())
	if isAuto {
		w.logf("continuous auto fallback active candidates=%d interval=%s", len(autoProfiles), autoProbeInterval)
		w.startAutoMonitor(ctx, instance, autoProfiles, *selected)
	}
	saved := r
	saved.ID = 0
	w.lastConnect = &saved
	if r.AutoReconnect {
		w.startHealthMonitor(ctx, instance)
	}
	return nil
}

func connectionModeName(mode string) string {
	if mode == "proxy" {
		return "proxy"
	}
	return "tun"
}

// instanceDial dials through the running Xray instance, as traffic from the
// tunnel or the local proxy would go.
func instanceDial(instance *core.Instance) func(context.Context, string, string) (net.Conn, error) {
	return func(c context.Context, _ string, address string) (net.Conn, error) {
		dest, err := xnet.ParseDestination("tcp:" + address)
		if err != nil {
			return nil, err
		}
		return core.Dial(c, instance, dest)
	}
}

// probeThroughInstance makes a real HTTPS request through Xray's dispatcher,
// not the system's direct connection.
func probeThroughInstance(ctx context.Context, instance *core.Instance, timeout time.Duration) error {
	transport := &http.Transport{DialContext: instanceDial(instance), TLSHandshakeTimeout: timeout}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.gstatic.com/generate_204", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
func normalizedSyncTrigger(reason string) string {
	switch reason {
	case "startup", "automatic", "cache":
		return reason
	}
	return "manual"
}

func main() {
	enc := json.NewEncoder(os.Stdout)
	// Reserve stdout exclusively for JSON IPC; core diagnostics must never corrupt it.
	os.Stdout = os.Stderr
	diag := log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	w := &worker{enc: enc, state: "disconnected", diag: diag}
	defer w.shutdown()
	w.logf("Go core started version=%s os=%s arch=%s pid=%d", core.Version(), runtime.GOOS, runtime.GOARCH, os.Getpid())
	if err := cleanupStaleNetworkState(w.logf); err != nil {
		w.logf("stale network state cleanup failed: %v", err)
	} else {
		w.logf("stale network state checked")
	}
	requests := make(chan request)
	w.internal = make(chan request)
	go func() {
		defer cancel()
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			var r request
			if json.Unmarshal(scanner.Bytes(), &r) != nil {
				continue
			}
			// The settings vault is plain file work: answer it here so a long
			// connect in the worker loop never delays saving the UI's state.
			if r.Method == "vaultLoad" || r.Method == "vaultSave" {
				w.handleVault(r)
				continue
			}
			select {
			case requests <- r:
			case <-ctx.Done():
				return
			}
		}
	}()
	w.send(map[string]any{"event": "ready", "version": core.Version()})
	for {
		select {
		case <-ctx.Done():
			w.logf("shutdown signal received")
			return
		case r := <-w.internal:
			if r.Method == "reconnect" {
				w.reconnect(ctx, r.Reason)
			}
		case r := <-requests:
			var result any = map[string]any{}
			var err error
			switch r.Method {
			case "deviceInfo":
				result = currentDeviceInfo()
			case "publicIp":
				w.logf("external IPv4 lookup requested masked=%t", r.Masked)
				// While connected, ask through the tunnel or local proxy: in the
				// system-proxy mode the core's own sockets bypass the proxy.
				var dial func(context.Context, string, string) (net.Conn, error)
				if w.instance != nil {
					dial = instanceDial(w.instance)
				}
				go func(id int, masked bool) {
					ip, ipErr := publicIPv4Via(ctx, dial)
					if ipErr != nil {
						w.logf("external IPv4 lookup failed: %v", ipErr)
						w.send(map[string]any{"id": id, "ok": false, "error": ipErr.Error()})
						return
					}
					if masked {
						ip = maskedPublicIPv4(ip)
					}
					w.logf("external IPv4 lookup completed masked=%t", masked)
					w.send(map[string]any{"id": id, "ok": true, "result": map[string]any{"ip": ip, "masked": masked}})
				}(r.ID, r.Masked)
				continue
			case "import":
				trigger := normalizedSyncTrigger(r.Reason)
				w.logf("subscription sync started trigger=%s host=%s", trigger, subscriptionHost(r.URL))
				if w.instance != nil {
					err = errors.New("Отключите VPN перед обновлением подписки")
				} else {
					var ps []Profile
					var skipped int
					var metadata subscriptionMetadata
					var cached bool
					ps, skipped, metadata, cached, err = fetchSubscription(ctx, r.URL, trigger == "cache", w.logf)
					if err == nil {
						w.profiles = ps
						result = map[string]any{"profiles": profilesForRenderer(ps), "skipped": skipped, "metadata": metadata, "cached": cached}
						w.logf("subscription sync completed profiles=%d skipped_unsupported=%d cached=%t", len(ps), skipped, cached)
					} else {
						w.logf("subscription sync failed: %v", err)
					}
				}
			case "importMany":
				trigger := normalizedSyncTrigger(r.Reason)
				w.logf("subscription batch sync started trigger=%s sources=%d", trigger, len(r.URLs))
				if len(r.URLs) == 0 || len(r.URLs) > 16 {
					err = errors.New("Добавьте от 1 до 16 подписок")
				} else if w.instance != nil {
					err = errors.New("Отключите VPN перед обновлением подписок")
				} else {
					var ps []Profile
					var sources []subscriptionSourceStatus
					var skipped int
					ps, sources, skipped, err = fetchSubscriptions(ctx, r.URLs, trigger == "cache", w.logf)
					if err == nil {
						w.profiles = ps
						result = map[string]any{"profiles": profilesForRenderer(ps), "skipped": skipped, "sources": sources}
						failed, cached := 0, 0
						for _, source := range sources {
							if source.Error != "" {
								failed++
							} else if source.Cached {
								cached++
							}
						}
						w.logf("subscription batch sync completed sources=%d failed=%d cached=%d profiles=%d skipped_unsupported=%d", len(sources), failed, cached, len(ps), skipped)
					} else {
						w.logf("subscription batch sync failed: %v", err)
					}
				}
			case "connect":
				w.cancelReconnect()
				if w.state == "reconnecting" {
					_ = w.close()
				}
				err = w.connect(ctx, r)
				if err == nil {
					result = w.currentActiveProfile()
				}
			case "switch":
				w.logf("server switch requested profile_id=%s", r.ProfileID)
				if w.instance == nil {
					err = errors.New("VPN уже отключён")
				} else {
					w.setState("disconnecting")
					err = w.closeTunnel(true)
					if err == nil {
						w.setState("connecting")
						err = w.connect(ctx, r)
					}
					if err == nil {
						result = w.currentActiveProfile()
						w.logf("server switch completed profile_id=%s", r.ProfileID)
					} else {
						_ = w.close()
						w.setState("disconnected")
						w.logf("server switch failed: %v", err)
					}
				}
			case "ping":
				method := strings.ToLower(r.PingMethod)
				if method != "head" && method != "get" {
					method = "tcp"
				}
				w.logf("latency test requested method=%s profiles=%d", method, len(w.profiles))
				if len(w.profiles) == 0 {
					err = errors.New("Сначала добавьте подписку")
				} else {
					options := pingOptions{}
					if w.instance != nil {
						options.OutboundInterface, err = physicalOutboundInterface()
						if err == nil && w.killSwitch != nil {
							options.AllowEndpoint = w.killSwitch.AllowEndpoint
						}
						if err == nil {
							w.logf("latency test pinned to physical interface=%s", options.OutboundInterface)
						}
					}
					if err == nil {
						result = pingProfilesWithAutoMethodOptions(ctx, w.profiles, method, options)
					}
					w.logf("latency test completed method=%s", method)
				}
			case "diagnose":
				w.logf("connection check requested profile_id=%s connected=%t", r.ProfileID, w.instance != nil)
				var steps []diagnoseStep
				steps, err = w.diagnose(ctx, r.ProfileID, r.AutoProfileIDs)
				if err == nil {
					result = map[string]any{"steps": steps}
					for _, step := range steps {
						w.logf("connection check step=%s state=%s", step.ID, step.State)
					}
				}
			case "disconnect":
				w.logf("disconnect requested")
				w.cancelReconnect()
				w.setState("disconnecting")
				err = w.close()
				w.setState("disconnected")
				if err != nil {
					w.logf("disconnect failed: %v", err)
					err = errors.New("Не удалось полностью закрыть TUN. Проверьте сетевой адаптер ShadowVPN")
				} else {
					w.logf("Xray instance and TUN adapter closed")
				}
			case "shutdown":
				w.logf("application shutdown requested")
				w.shutdown()
				w.send(map[string]any{"id": r.ID, "ok": true})
				return
			default:
				err = errors.New("Неизвестная команда")
			}
			if err != nil {
				w.send(map[string]any{"id": r.ID, "ok": false, "error": err.Error()})
			} else {
				w.send(map[string]any{"id": r.ID, "ok": true, "result": result})
			}
		}
	}
}

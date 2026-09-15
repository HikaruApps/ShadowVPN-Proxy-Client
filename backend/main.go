package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	neturl "net/url"
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
	Reason         string   `json:"reason"`
	ProfileID      string   `json:"profileId"`
	DNS            string   `json:"dns"`
	DNSServers     []string `json:"dnsServers"`
	Fragment       bool     `json:"fragmentation"`
	KillSwitch     bool     `json:"killSwitch"`
	AutoProfileIDs []string `json:"autoProfileIds"`
	Masked         bool     `json:"masked"`
	PingMethod     string   `json:"pingMethod"`
	RouteMode      string   `json:"routeMode"`
	DirectDomains  []string `json:"directDomains"`
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
	killSwitch    *killSwitchGuard
	enc           *json.Encoder
	mu            sync.Mutex
	state         string
	diag          *log.Logger
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
func (w *worker) close() error {
	w.stopAutoMonitor()
	w.clearActiveProfile()
	var closeErr error
	if w.instance == nil {
		if w.killSwitch != nil {
			w.logf("disabling kill switch WFP policy")
			closeErr = w.killSwitch.Close()
			w.killSwitch = nil
		}
		return closeErr
	}
	v := w.instance
	w.instance = nil
	closeErr = v.Close()
	if w.killSwitch != nil {
		w.logf("disabling kill switch WFP policy")
		closeErr = errors.Join(closeErr, w.killSwitch.Close())
		w.killSwitch = nil
	}
	return closeErr
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

func (w *worker) connect(ctx context.Context, id, dnsID string, customDNS, autoProfileIDs, directDomains []string, routeMode string, fragmentation, killSwitch bool) error {
	w.logf("connect requested profile_id=%s", id)
	validatedDNSID, dns, err := selectedDNS(dnsID, customDNS)
	if err != nil {
		w.logf("connect rejected: invalid DNS preset")
		return err
	}
	routing, routingErr := newRoutingOptions(routeMode, directDomains)
	if routingErr != nil {
		w.logf("connect rejected: invalid routing settings: %v", routingErr)
		return routingErr
	}
	if killSwitch && routing.Mode != "full" {
		w.logf("connect rejected: kill switch cannot be combined with split routing mode=%s", routing.Mode)
		return errors.New("Kill Switch отключает прямой трафик, поэтому выключите его для выборочной маршрутизации")
	}
	w.logf("DNS selected id=%s name=%s; routing_mode=%s direct_domains=%d; TLS fragmentation=%t; kill_switch=%t", validatedDNSID, dns.Name, routing.Mode, len(routing.DirectDomains), fragmentation, killSwitch)
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
		results := pingProfilesWithMethod(ctx, candidates, "auto")
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
	if err := checkPlatform(); err != nil {
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
			latency, _, pingErr = httpPingProfile(pingCtx, resolved, "head")
		} else {
			latency, pingErr = tcpPing(pingCtx, resolved.Address, resolved.Port)
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
	w.logf("creating Xray TUN configuration with pre-resolved endpoint=%s", bootstrap.SelectedAddress)
	var config []byte
	if isAuto {
		config, e = makeAutoConfigWithRoutingOptions(autoProfiles, validatedDNSID, dns.Servers, fragmentation, outboundInterface, routing)
	} else {
		config, e = makeConfigWithRoutingOptions(resolved, validatedDNSID, dns.Servers, fragmentation, outboundInterface, routing)
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
		w.killSwitch, e = startKillSwitchGuard(allowedEndpoints)
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
	transport := &http.Transport{DialContext: func(c context.Context, network, address string) (net.Conn, error) {
		dest, e := xnet.ParseDestination("tcp:" + address)
		if e != nil {
			return nil, e
		}
		return core.Dial(c, instance, dest)
	}, TLSHandshakeTimeout: 20 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 24 * time.Second}
	req, _ := http.NewRequestWithContext(probeCtx, "GET", "https://www.gstatic.com/generate_204", nil)
	resp, e := client.Do(req)
	if e == nil {
		_ = resp.Body.Close()
		if resp.StatusCode != 204 {
			e = errors.New("unexpected status")
		}
	}
	if e != nil {
		w.logf("connectivity probe failed: %v", e)
		_ = w.close()
		w.setState("disconnected")
		return errors.New("TUN запущен, но проверка интернета через сервер не прошла. Соединение отключено")
	}
	w.logf("connectivity probe passed; tunnel is ready")
	w.setActiveProfile(*selected)
	w.setState("connected")
	if isAuto {
		w.logf("continuous auto fallback active candidates=%d interval=%s", len(autoProfiles), autoProbeInterval)
		w.startAutoMonitor(ctx, instance, autoProfiles, *selected)
	}
	return nil
}
func main() {
	enc := json.NewEncoder(os.Stdout)
	// Reserve stdout exclusively for JSON IPC; core diagnostics must never corrupt it.
	os.Stdout = os.Stderr
	diag := log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	w := &worker{enc: enc, state: "disconnected", diag: diag}
	defer w.close()
	w.logf("Go core started version=%s os=%s arch=%s pid=%d", core.Version(), runtime.GOOS, runtime.GOARCH, os.Getpid())
	if err := cleanupStaleKillSwitch(); err != nil {
		w.logf("stale kill switch cleanup failed: %v", err)
	} else {
		w.logf("stale kill switch state checked")
	}
	requests := make(chan request)
	go func() {
		defer cancel()
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 64*1024)
		for scanner.Scan() {
			var r request
			if json.Unmarshal(scanner.Bytes(), &r) != nil {
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
		case r := <-requests:
			var result any = map[string]any{}
			var err error
			switch r.Method {
			case "deviceInfo":
				result = currentDeviceInfo()
			case "publicIp":
				w.logf("external IPv4 lookup requested masked=%t", r.Masked)
				go func(id int, masked bool) {
					ip, ipErr := publicIPv4(ctx)
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
				trigger := r.Reason
				if trigger != "startup" && trigger != "automatic" {
					trigger = "manual"
				}
				host := "invalid"
				if parsed, parseErr := neturl.Parse(r.URL); parseErr == nil {
					host = parsed.Hostname()
				}
				w.logf("subscription sync started trigger=%s host=%s", trigger, host)
				if w.instance != nil {
					err = errors.New("Отключите VPN перед обновлением подписки")
				} else {
					var ps []Profile
					var skipped int
					ps, skipped, err = fetchSubscription(ctx, r.URL)
					if err == nil {
						w.profiles = ps
						result = map[string]any{"profiles": profilesForRenderer(ps), "skipped": skipped}
						w.logf("subscription sync completed profiles=%d skipped_unsupported=%d", len(ps), skipped)
					} else {
						w.logf("subscription sync failed: %v", err)
					}
				}
			case "connect":
				err = w.connect(ctx, r.ProfileID, r.DNS, r.DNSServers, r.AutoProfileIDs, r.DirectDomains, r.RouteMode, r.Fragment, r.KillSwitch)
				if err == nil {
					result = w.currentActiveProfile()
				}
			case "ping":
				method := strings.ToLower(r.PingMethod)
				if method != "head" && method != "get" {
					method = "tcp"
				}
				w.logf("latency test requested method=%s profiles=%d", method, len(w.profiles))
				if w.instance != nil {
					err = errors.New("Отключите VPN перед проверкой задержки")
				} else if len(w.profiles) == 0 {
					err = errors.New("Сначала добавьте подписку")
				} else {
					result = pingProfilesWithAutoMethod(ctx, w.profiles, method)
					w.logf("latency test completed method=%s", method)
				}
			case "disconnect":
				w.logf("disconnect requested")
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
				_ = w.close()
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

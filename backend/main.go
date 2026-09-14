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

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	_ "github.com/xtls/xray-core/main/distro/all"
)

type request struct {
	ID         int      `json:"id"`
	Method     string   `json:"method"`
	URL        string   `json:"url"`
	ProfileID  string   `json:"profileId"`
	DNS        string   `json:"dns"`
	DNSServers []string `json:"dnsServers"`
	Fragment   bool     `json:"fragmentation"`
	KillSwitch bool     `json:"killSwitch"`
	Masked     bool     `json:"masked"`
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
func (w *worker) close() error {
	w.activeProfile = connectionInfo{}
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
func (w *worker) connect(ctx context.Context, id, dnsID string, customDNS []string, fragmentation, killSwitch bool) error {
	w.logf("connect requested profile_id=%s", id)
	validatedDNSID, dns, err := selectedDNS(dnsID, customDNS)
	if err != nil {
		w.logf("connect rejected: invalid DNS preset")
		return err
	}
	w.logf("DNS selected id=%s name=%s; TLS fragmentation=%t; kill_switch=%t", validatedDNSID, dns.Name, fragmentation, killSwitch)
	if runtime.GOOS != "windows" {
		w.logf("connect rejected: unsupported platform=%s", runtime.GOOS)
		return errors.New("Эта сборка клиента поддерживает TUN только на Windows")
	}
	if w.instance != nil {
		return errors.New("Сначала отключите текущее соединение")
	}
	w.activeProfile = connectionInfo{}
	var selected *Profile
	isAuto := id == autoProfileID
	var autoProfiles []Profile
	var allowedEndpoints []proxyEndpoint
	if isAuto {
		w.logf("auto selection started profiles=%d", len(w.profiles))
		results := pingProfiles(ctx, w.profiles)
		_, best, ok := fastestProfile(w.profiles, results)
		if !ok {
			w.logf("auto selection failed: no reachable profiles")
			return errors.New("Авто не нашёл доступных серверов. Запустите проверку TCP-пинга")
		}
		prepareCtx, prepareCancel := context.WithTimeout(ctx, 12*time.Second)
		var skipped []string
		autoProfiles, allowedEndpoints, skipped, err = prepareAutoProfiles(prepareCtx, w.profiles, results)
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
		w.logf("endpoint bootstrap TCP check started endpoint=%s candidate=%d/%d", endpoint, index+1, len(bootstrap.Candidates))
		latency, pingErr = tcpPing(pingCtx, resolved.Address, resolved.Port)
		if pingErr == nil {
			bootstrap.SelectedAddress = candidate
			w.logf("endpoint bootstrap TCP check passed endpoint=%s latency_ms=%d", endpoint, latency)
			break
		}
		w.logf("endpoint bootstrap TCP candidate failed endpoint=%s error=%v", endpoint, pingErr)
	}
	pingCancel()
	if pingErr != nil {
		w.logf("endpoint bootstrap TCP check failed candidates=%s error=%v", strings.Join(bootstrap.Candidates, ","), pingErr)
		return errors.New("VPN-сервер разрешён, но недоступен по TCP")
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
		config, e = makeAutoConfigWithOptions(autoProfiles, validatedDNSID, dns.Servers, fragmentation, outboundInterface)
	} else {
		config, e = makeConfigWithOptions(resolved, validatedDNSID, dns.Servers, fragmentation, outboundInterface)
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
	if isAuto {
		w.logf("continuous auto health monitor active candidates=%d interval=%s", len(autoProfiles), autoProbeInterval)
	}
	w.activeProfile = connectionInfoForProfile(*selected)
	w.setState("connected")
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
				host := "invalid"
				if parsed, parseErr := neturl.Parse(r.URL); parseErr == nil {
					host = parsed.Hostname()
				}
				w.logf("subscription sync started host=%s", host)
				if w.instance != nil {
					err = errors.New("Отключите VPN перед обновлением подписки")
				} else {
					var ps []Profile
					ps, err = fetchSubscription(ctx, r.URL)
					if err == nil {
						w.profiles = ps
						result = profilesForRenderer(ps)
						w.logf("subscription sync completed profiles=%d", len(ps))
					} else {
						w.logf("subscription sync failed: %v", err)
					}
				}
			case "connect":
				err = w.connect(ctx, r.ProfileID, r.DNS, r.DNSServers, r.Fragment, r.KillSwitch)
				if err == nil {
					result = w.activeProfile
				}
			case "ping":
				w.logf("TCP latency test requested profiles=%d", len(w.profiles))
				if w.instance != nil {
					err = errors.New("Отключите VPN перед проверкой TCP-пинга")
				} else if len(w.profiles) == 0 {
					err = errors.New("Сначала добавьте подписку")
				} else {
					result = pingProfilesWithAuto(ctx, w.profiles)
					w.logf("TCP latency test completed")
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

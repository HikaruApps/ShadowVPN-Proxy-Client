package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"
)

// diagnoseStep is one line of the connection check the app shows. A failed
// step is a normal result with a reason the user can act on, not an error.
type diagnoseStep struct {
	ID     string `json:"id"`
	State  string `json:"state"` // ok, fail or skip
	Detail string `json:"detail"`
}

// diagnoseCandidates picks what a connection to profileID would use: the
// profile itself, or for Auto its first few candidates.
func diagnoseCandidates(profiles []Profile, profileID string, autoProfileIDs []string) ([]Profile, error) {
	if profileID == "" {
		return nil, errors.New("Выберите сервер")
	}
	var candidates []Profile
	if isAutoProfileID(profileID) {
		candidates = profilesByID(profiles, autoProfileIDs)
		if profileID == autoNoRUProfileID {
			candidates = withoutRussianProfiles(candidates)
		}
	} else {
		candidates = profilesByID(profiles, []string{profileID})
	}
	if len(candidates) == 0 {
		return nil, errors.New("Сервер отсутствует в подписке. Обновите её")
	}
	if len(candidates) > 4 {
		candidates = candidates[:4]
	}
	return candidates, nil
}

// diagnose checks, in the order a connection needs them: the computer's own
// network, the server's address, a real request through the server, and, when
// connected, the internet through the tunnel.
func (w *worker) diagnose(ctx context.Context, profileID string, autoProfileIDs []string) ([]diagnoseStep, error) {
	candidates, err := diagnoseCandidates(w.profiles, profileID, autoProfileIDs)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	connected := w.instance != nil
	steps := make([]diagnoseStep, 0, 4)
	skipRest := func(reason string) []diagnoseStep {
		for _, id := range []string{"network", "dns", "server", "vpn"}[len(steps):] {
			steps = append(steps, diagnoseStep{ID: id, State: "skip", Detail: reason})
		}
		return steps
	}

	// Network: a default route outside the tunnel, and while disconnected a
	// short TCP handshake through it.
	options := pingOptions{}
	interfaceName, err := physicalOutboundInterface()
	switch {
	case err != nil:
		steps = append(steps, diagnoseStep{ID: "network", State: "fail", Detail: "Нет активного подключения к сети: Wi-Fi или кабель не подключены"})
		return skipRest("Пропущено: нет сети"), nil
	case connected:
		steps = append(steps, diagnoseStep{ID: "network", State: "ok", Detail: interfaceName + " · подключено"})
		options.OutboundInterface = interfaceName
		if w.killSwitch != nil {
			options.AllowEndpoint = w.killSwitch.AllowEndpoint
		}
	default:
		if reachErr := internetReachable(ctx, interfaceName); reachErr != nil {
			steps = append(steps, diagnoseStep{ID: "network", State: "fail", Detail: interfaceName + " без доступа в интернет: " + maskIPs(explainProbeError(reachErr))})
			return skipRest("Пропущено: нет интернета"), nil
		}
		steps = append(steps, diagnoseStep{ID: "network", State: "ok", Detail: interfaceName + " · интернет есть"})
	}

	// DNS: the server's own name, resolved the way connect resolves it.
	dnsStep := diagnoseStep{ID: "dns", State: "fail"}
	resolved := make([]Profile, 0, len(candidates))
	var dnsErr error
	for _, profile := range candidates {
		resolveCtx, done := context.WithTimeout(ctx, 6*time.Second)
		endpoint, info, resolveErr := resolveProfileEndpoint(resolveCtx, profile)
		done()
		if resolveErr != nil {
			dnsErr = resolveErr
			continue
		}
		resolved = append(resolved, endpoint)
		if dnsStep.State != "ok" {
			dnsStep.State = "ok"
			if net.ParseIP(info.OriginalAddress) != nil {
				dnsStep.Detail = "Найден · задан IP-адресом " + maskIP(info.SelectedAddress)
			} else {
				dnsStep.Detail = "Найден · " + info.OriginalAddress + " → " + maskIP(info.SelectedAddress)
			}
		}
	}
	if dnsStep.State != "ok" {
		dnsStep.Detail = "Не найден: не удалось узнать IP сервера. Проверьте DNS провайдера или смените сеть"
		if dnsErr != nil {
			dnsStep.Detail += " (" + maskIPs(shortError(dnsErr)) + ")"
		}
		steps = append(steps, dnsStep)
		return skipRest("Пропущено: адрес сервера не найден"), nil
	}
	steps = append(steps, dnsStep)

	// Server: a real HTTPS request through each candidate, like the HTTP ping.
	type answer struct {
		name    string
		latency int64
		err     error
	}
	answers := make(chan answer, len(resolved))
	for _, profile := range resolved {
		go func(profile Profile) {
			probeCtx, done := context.WithTimeout(ctx, 10*time.Second)
			defer done()
			latency, _, probeErr := httpPingProfileWithOptions(probeCtx, profile, "head", options)
			answers <- answer{profile.Name, latency, probeErr}
		}(profile)
	}
	var working []answer
	var firstErr error
	for range resolved {
		item := <-answers
		if item.err == nil {
			working = append(working, item)
		} else if firstErr == nil {
			firstErr = item.err
		}
	}
	if len(working) == 0 {
		steps = append(steps, diagnoseStep{ID: "server", State: "fail", Detail: "Недоступен: " + maskIPs(explainProbeError(firstErr))})
	} else {
		sort.Slice(working, func(a, b int) bool { return working[a].latency < working[b].latency })
		detail := fmt.Sprintf("Доступен · отвечает за %d мс", working[0].latency)
		if len(candidates) > 1 {
			detail = fmt.Sprintf("Доступен · быстрее всех «%s», %d мс · работают %d из %d", working[0].name, working[0].latency, len(working), len(candidates))
		}
		steps = append(steps, diagnoseStep{ID: "server", State: "ok", Detail: detail})
	}

	// Internet through the tunnel: the public IP as the world sees it.
	if !connected {
		steps = append(steps, diagnoseStep{ID: "vpn", State: "skip", Detail: "VPN не подключён: подключитесь и запустите проверку снова"})
		return steps, nil
	}
	ip, ipErr := publicIPv4Via(ctx, instanceDial(w.instance))
	if ipErr != nil {
		steps = append(steps, diagnoseStep{ID: "vpn", State: "fail", Detail: "Не работает: " + maskIPs(explainProbeError(ipErr))})
	} else {
		steps = append(steps, diagnoseStep{ID: "vpn", State: "ok", Detail: "Работает · ваш IP " + maskIP(ip)})
	}
	return steps, nil
}

// internetReachable opens a TCP connection to well-known anycast resolvers
// through the physical interface; any one answering is enough.
func internetReachable(ctx context.Context, interfaceName string) error {
	dialer, err := interfaceDialer(interfaceName, 4*time.Second)
	if err != nil {
		return err
	}
	var lastErr error
	for _, target := range []string{"1.1.1.1:443", "8.8.8.8:443", "9.9.9.9:443"} {
		connection, dialErr := dialer.DialContext(ctx, "tcp", target)
		if dialErr == nil {
			_ = connection.Close()
			return nil
		}
		lastErr = dialErr
	}
	return lastErr
}

// explainProbeError turns a probe failure into something a user can act on.
func explainProbeError(err error) string {
	if err == nil {
		return "нет ответа"
	}
	if errors.Is(err, wsaeAccess) {
		return "соединение заблокировано брандмауэром или Kill Switch"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "deadline") || strings.Contains(text, "timeout"):
		return "нет ответа за отведённое время. Сервер заблокирован в этой сети, перегружен или не принимает ваш ключ"
	case strings.Contains(text, "refused"):
		return "сервер отклонил подключение. Возможно, он выключен"
	case strings.Contains(text, "eof") || strings.Contains(text, "reset") || strings.Contains(text, "closed pipe"):
		return "сервер обрывает соединение. Обычно это неверный ключ или истёкшая подписка"
	case strings.Contains(text, "certificate") || strings.Contains(text, "tls"):
		return "ошибка TLS. Проверьте настройки сервера или обновите подписку"
	}
	return shortError(err)
}

func shortError(err error) string {
	text := err.Error()
	if runes := []rune(text); len(runes) > 120 {
		text = string(runes[:120]) + "…"
	}
	return text
}

var ipv4Pattern = regexp.MustCompile(`\b(\d{1,3}\.\d{1,3})\.\d{1,3}\.\d{1,3}\b`)

// maskIP hides the host part of an address shown to the user: the last two
// octets of IPv4, everything after the first two groups of IPv6.
func maskIP(address string) string {
	ip := net.ParseIP(address)
	if ip == nil {
		return maskIPs(address)
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.*.*", v4[0], v4[1])
	}
	groups := strings.SplitN(ip.String(), ":", 3)
	if len(groups) < 3 {
		return "*"
	}
	return groups[0] + ":" + groups[1] + ":*:*"
}

// maskIPs masks the IPv4 addresses inside a message, such as a dial error.
func maskIPs(text string) string {
	return ipv4Pattern.ReplaceAllString(text, "$1.*.*")
}

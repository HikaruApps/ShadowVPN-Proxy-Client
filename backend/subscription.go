package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Profile struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Protocol  string         `json:"protocol"`
	Transport string         `json:"transport"`
	Auto      bool           `json:"auto,omitempty"`
	Address   string         `json:"-"`
	Port      int            `json:"-"`
	Outbound  map[string]any `json:"-"`
}

func newProfile(name string, out map[string]any) Profile {
	b, _ := json.Marshal(out)
	sum := sha256.Sum256(b)
	proto, _ := out["protocol"].(string)
	transport := "tcp"
	if stream, ok := out["streamSettings"].(map[string]any); ok {
		if v, ok := stream["network"].(string); ok {
			transport = v
		}
	}
	if name == "" {
		name = strings.ToUpper(proto)
	}
	address, port := endpointFromOutbound(out)
	return Profile{
		ID:        hex.EncodeToString(sum[:12]),
		Name:      name,
		Protocol:  proto,
		Transport: transport,
		Address:   address,
		Port:      port,
		Outbound:  out,
	}
}
func decodeBase64(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, e := enc.DecodeString(s); e == nil {
			return b, nil
		}
	}
	return nil, errors.New("Некорректный Base64")
}
func fetchSubscription(ctx context.Context, address string) ([]Profile, error) {
	u, e := url.Parse(address)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("Нужна HTTPS-ссылка на подписку")
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || r.URL.Scheme != "https" || r.URL.User != nil {
			return errors.New("Недопустимое перенаправление")
		}
		return nil
	}}
	req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return nil, errors.New("Некорректная ссылка")
	}
	req.Header.Set("User-Agent", "v2rayN/7.0 ShadowVPN/0.10.0")
	req.Header.Set("Accept", "application/json, text/plain;q=0.9")
	resp, e := client.Do(req)
	if e != nil {
		return nil, errors.New("Не удалось загрузить подписку. Проверьте интернет и ссылку")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Сервер подписки вернул HTTP %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if e != nil {
		return nil, errors.New("Ошибка чтения подписки")
	}
	if len(b) > 4*1024*1024 {
		return nil, errors.New("Подписка превышает 4 МБ")
	}
	return parseSubscription(b)
}
func parseSubscription(b []byte) ([]Profile, error) {
	s := strings.TrimSpace(strings.TrimPrefix(string(b), "\ufeff"))
	profiles := []Profile{}
	if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
		var items []map[string]any
		if strings.HasPrefix(s, "[") {
			if json.Unmarshal([]byte(s), &items) != nil {
				return nil, errors.New("Некорректный JSON")
			}
		} else {
			var item map[string]any
			if json.Unmarshal([]byte(s), &item) != nil {
				return nil, errors.New("Некорректный JSON")
			}
			items = append(items, item)
		}
		for _, item := range items {
			name, _ := item["remarks"].(string)
			outs, _ := item["outbounds"].([]any)
			if _, ok := item["protocol"]; ok {
				outs = []any{item}
			}
			for _, raw := range outs {
				out, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				proto, _ := out["protocol"].(string)
				switch proto {
				case "vless", "vmess", "trojan", "shadowsocks":
				default:
					continue
				}
				// Import connection settings only. Never accept subscription-supplied listeners,
				// routing, local source addresses, proxy chaining, or socket/interface overrides.
				clean := map[string]any{"protocol": proto, "settings": out["settings"], "tag": "proxy"}
				if st, ok := out["streamSettings"].(map[string]any); ok {
					copy := map[string]any{}
					for k, v := range st {
						if k != "sockopt" && k != "finalmask" {
							copy[k] = v
						}
					}
					clean["streamSettings"] = copy
				}
				if name == "" {
					name, _ = out["tag"].(string)
				}
				profiles = append(profiles, newProfile(name, clean))
				break
			}
		}
	} else {
		if !strings.Contains(s, "://") {
			decoded, e := decodeBase64(s)
			if e != nil {
				return nil, errors.New("Формат подписки не распознан")
			}
			s = string(decoded)
		}
		for _, line := range strings.Fields(s) {
			if strings.HasPrefix(line, "#") {
				continue
			}
			p, e := parseURI(line)
			if e != nil {
				return nil, e
			}
			profiles = append(profiles, p)
		}
	}
	visible := profiles[:0]
	for _, profile := range profiles {
		ip := net.ParseIP(strings.TrimSpace(profile.Address))
		if ip != nil && ip.IsUnspecified() {
			continue
		}
		visible = append(visible, profile)
	}
	profiles = visible
	if len(profiles) == 0 {
		return nil, errors.New("Нет поддерживаемых серверов. Нужна Xray JSON или подписка VLESS/Trojan")
	}
	if len(profiles) > 500 {
		return nil, errors.New("В подписке больше 500 серверов")
	}
	seen := map[string]bool{}
	unique := []Profile{}
	for _, p := range profiles {
		if !seen[p.ID] {
			unique = append(unique, p)
			seen[p.ID] = true
		}
	}
	return unique, nil
}
func parseURI(raw string) (Profile, error) {
	u, e := url.Parse(raw)
	if e != nil || u.User == nil || u.Hostname() == "" {
		return Profile{}, errors.New("Некорректная ссылка сервера")
	}
	if u.Scheme != "vless" && u.Scheme != "trojan" {
		return Profile{}, fmt.Errorf("URI-протокол %s пока не поддерживается; используйте Xray JSON", u.Scheme)
	}
	port, e := strconv.Atoi(u.Port())
	if e != nil || port < 1 || port > 65535 {
		return Profile{}, errors.New("Некорректный порт сервера")
	}
	q := u.Query()
	security := q.Get("security")
	if security == "" {
		if u.Scheme == "trojan" {
			security = "tls"
		} else {
			security = "none"
		}
	}
	stream := map[string]any{}
	network := q.Get("type")
	if network == "" {
		network = "tcp"
	}
	stream["network"] = network
	stream["security"] = security
	switch security {
	case "tls":
		tls := map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp")}
		if q.Get("alpn") != "" {
			tls["alpn"] = strings.Split(q.Get("alpn"), ",")
		}
		stream["tlsSettings"] = tls
	case "reality":
		stream["realitySettings"] = map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp"), "publicKey": q.Get("pbk"), "shortId": q.Get("sid"), "spiderX": q.Get("spx")}
	case "none":
	default:
		return Profile{}, errors.New("Неизвестный тип защиты соединения")
	}
	switch network {
	case "tcp", "raw":
		if h := q.Get("headerType"); h != "" && h != "none" {
			return Profile{}, errors.New("TCP headerType требует Xray JSON")
		}
	case "ws":
		stream["wsSettings"] = map[string]any{"path": q.Get("path"), "headers": map[string]string{"Host": q.Get("host")}}
	case "grpc":
		stream["grpcSettings"] = map[string]any{"serviceName": q.Get("serviceName"), "authority": q.Get("authority"), "multiMode": q.Get("mode") == "multi"}
	case "xhttp":
		x := map[string]any{"path": q.Get("path"), "host": q.Get("host")}
		if q.Get("mode") != "" {
			x["mode"] = q.Get("mode")
		}
		if q.Get("extra") != "" {
			var extra map[string]any
			if json.Unmarshal([]byte(q.Get("extra")), &extra) != nil {
				return Profile{}, errors.New("Некорректный XHTTP extra")
			}
			x["extra"] = extra
		}
		stream["xhttpSettings"] = x
	case "httpupgrade":
		stream["httpupgradeSettings"] = map[string]any{"path": q.Get("path"), "host": q.Get("host")}
	default:
		return Profile{}, fmt.Errorf("Транспорт %s требует Xray JSON", network)
	}
	out := map[string]any{"tag": "proxy", "protocol": u.Scheme, "streamSettings": stream}
	if u.Scheme == "vless" {
		encryption := q.Get("encryption")
		if encryption == "" {
			encryption = "none"
		}
		out["settings"] = map[string]any{"vnext": []any{map[string]any{"address": u.Hostname(), "port": port, "users": []any{map[string]any{"id": u.User.Username(), "encryption": encryption, "flow": q.Get("flow")}}}}}
	} else {
		out["settings"] = map[string]any{"servers": []any{map[string]any{"address": u.Hostname(), "port": port, "password": u.User.Username()}}}
	}
	return newProfile(u.Fragment, out), nil
}
func makeConfig(p Profile) ([]byte, error) {
	return makeConfigWithDNS(p, "cloudflare", nil)
}

func makeConfigWithDNS(p Profile, dnsID string, customServers []string) ([]byte, error) {
	return makeConfigWithOptions(p, dnsID, customServers, false, "auto")
}

func makeConfigWithOptions(p Profile, dnsID string, customServers []string, fragmentation bool, outboundInterface string) ([]byte, error) {
	_, dns, err := selectedDNS(dnsID, customServers)
	if err != nil {
		return nil, err
	}
	outbound, err := configuredOutbound(p, "proxy", fragmentation)
	if err != nil {
		return nil, err
	}
	return marshalRuntimeConfig([]any{outbound}, dns, outboundInterface, false)
}

func makeAutoConfigWithOptions(profiles []Profile, dnsID string, customServers []string, fragmentation bool, outboundInterface string) ([]byte, error) {
	if len(profiles) == 0 {
		return nil, errors.New("для Auto не переданы серверы")
	}
	_, dns, err := selectedDNS(dnsID, customServers)
	if err != nil {
		return nil, err
	}
	outbounds := make([]any, 0, len(profiles))
	for _, profile := range profiles {
		outbound, err := configuredOutbound(profile, "auto-"+profile.ID, fragmentation)
		if err != nil {
			return nil, err
		}
		outbounds = append(outbounds, outbound)
	}
	return marshalRuntimeConfig(outbounds, dns, outboundInterface, true)
}

func configuredOutbound(p Profile, tag string, fragmentation bool) (map[string]any, error) {
	outbound, err := cloneObject(p.Outbound)
	if err != nil {
		return nil, err
	}
	outbound["tag"] = tag
	if fragmentation {
		stream, _ := outbound["streamSettings"].(map[string]any)
		if stream == nil {
			stream = map[string]any{}
			outbound["streamSettings"] = stream
		}
		stream["finalmask"] = map[string]any{
			"tcp": []any{map[string]any{
				"type": "fragment",
				"settings": map[string]any{
					"packets":  "tlshello",
					"length":   "10-35",
					"delay":    "0-1",
					"maxSplit": "3-5",
				},
			}},
		}
	}
	return outbound, nil
}

func marshalRuntimeConfig(outbounds []any, dns dnsPreset, outboundInterface string, observeAuto bool) ([]byte, error) {
	if outboundInterface == "" {
		outboundInterface = "auto"
	}
	config := map[string]any{
		"log":       map[string]any{"loglevel": "debug"},
		"inbounds":  []any{map[string]any{"tag": "tun-in", "protocol": "tun", "settings": map[string]any{"name": "ShadowVPN", "desc": "ShadowVPN", "mtu": 1500, "gateway": []string{"172.31.255.1/30", "fd31:ffff::1/126"}, "dns": dns.Servers, "autoSystemRoutingTable": []string{"0.0.0.0/0", "::/0"}, "autoOutboundsInterface": outboundInterface}}},
		"outbounds": outbounds,
	}
	if observeAuto {
		config["observatory"] = map[string]any{
			"subjectSelector":   []string{"auto-"},
			"probeUrl":          "https://www.gstatic.com/generate_204",
			"probeInterval":     autoProbeInterval,
			"enableConcurrency": true,
		}
	}
	return json.Marshal(config)
}

func cloneObject(value map[string]any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return result, nil
}

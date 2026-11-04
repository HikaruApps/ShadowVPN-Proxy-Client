package main

import (
	"fmt"
	"net"
	"strconv"
)

// localProxyPortsFree fails early when another client (v2rayN and others use
// the same defaults) already listens on the system-proxy ports.
func localProxyPortsFree() error {
	for _, port := range []int{localSocksPort, localHTTPPort} {
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return fmt.Errorf("Порт 127.0.0.1:%d занят другой программой. Закройте другой VPN-клиент или выберите режим TUN", port)
		}
		_ = listener.Close()
	}
	return nil
}

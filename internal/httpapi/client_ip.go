package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func (a *API) clientIP(r *http.Request) string {
	remote, ok := parseRemoteIP(r.RemoteAddr)
	if !ok {
		return ""
	}
	if !a.isTrustedProxy(remote) {
		return remote.String()
	}

	forwarded := forwardedForIPs(r.Header.Values("X-Forwarded-For"))
	for i := len(forwarded) - 1; i >= 0; i-- {
		if !a.isTrustedProxy(forwarded[i]) {
			return forwarded[i].String()
		}
	}
	if len(forwarded) > 0 {
		return forwarded[0].String()
	}
	return remote.String()
}

func (a *API) isTrustedProxy(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range a.trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func parseRemoteIP(remoteAddr string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil {
		addr, parseErr := netip.ParseAddr(host)
		if parseErr == nil {
			return addr.Unmap(), true
		}
	}

	value := strings.Trim(strings.TrimSpace(remoteAddr), "[]")
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func forwardedForIPs(values []string) []netip.Addr {
	var result []netip.Addr
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			addr, err := netip.ParseAddr(strings.TrimSpace(part))
			if err != nil {
				continue
			}
			result = append(result, addr.Unmap())
		}
	}
	return result
}

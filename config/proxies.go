package config

import (
	"errors"
	"net"
	"strings"
)

// ParseTrustedProxies splits the comma-separated trusted proxy list into IP
// addresses and CIDR ranges, normalized. Only requests arriving from one of
// these may set the client address with X-Forwarded-For or X-Real-IP; for any
// other peer the headers are ignored, since anyone can send them. Empty
// entries are ignored; the first invalid one is an error.
func ParseTrustedProxies(raw string) ([]string, error) {
	proxies := []string{}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err != nil {
				return nil, errors.New("'" + entry + "' is not a valid IP address or CIDR range")
			}
			proxies = append(proxies, network.String())
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			return nil, errors.New("'" + entry + "' is not a valid IP address or CIDR range")
		}
		proxies = append(proxies, ip.String())
	}
	return proxies, nil
}

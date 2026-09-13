package collector

import (
	"net"
	"sort"
)

func ipAddresses() []string {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		result = append(result, ip.String())
	}
	sort.Strings(result)
	return result
}

package collector

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/seesize/seesize/internal/model"
)

type netCounter struct{ rx, tx uint64 }
type networkState struct {
	selected []string
	previous map[string]netCounter
	at       time.Time
}

// Empty selection retains the legacy all-non-loopback scope.
func (c *Collector) SetNetworkInterfaces(value string) error {
	seen := map[string]bool{}
	var names []string
	if strings.TrimSpace(value) != "" {
		for _, part := range strings.Split(value, ",") {
			name := strings.TrimSpace(part)
			if name == "" || name == "lo" || strings.ContainsAny(name, " /:\t\r\n") {
				return fmt.Errorf("invalid non-loopback interface %q", name)
			}
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
	}
	sort.Strings(names)
	c.network = networkState{selected: names}
	return nil
}

func (n *networkState) sample(input string, now time.Time) (model.NetworkMetrics, error) {
	counters := map[string]netCounter{}
	for _, line := range strings.Split(input, "\n") {
		name, data, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "lo" {
			continue
		}
		if len(n.selected) > 0 {
			found := false
			for _, selected := range n.selected {
				if name == selected {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		fields := strings.Fields(data)
		if len(fields) < 9 {
			n.previous = nil
			return model.NetworkMetrics{}, fmt.Errorf("invalid network counters for %s", name)
		}
		rx, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			n.previous = nil
			return model.NetworkMetrics{}, err
		}
		tx, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			n.previous = nil
			return model.NetworkMetrics{}, err
		}
		counters[name] = netCounter{rx, tx}
	}
	result := model.NetworkMetrics{Scope: "all-non-loopback", Interfaces: []string{}, MissingInterfaces: []string{}}
	if len(n.selected) > 0 {
		result.Scope = "selected"
		for _, name := range n.selected {
			if _, ok := counters[name]; !ok {
				result.MissingInterfaces = append(result.MissingInterfaces, name)
			}
		}
	}
	seconds := now.Sub(n.at).Seconds()
	result.RateUnavailable = len(counters) == 0 || len(result.MissingInterfaces) > 0 || seconds <= 0 || n.previous == nil || len(n.previous) != len(counters)
	for name, current := range counters {
		result.Interfaces = append(result.Interfaces, name)
		result.ReceivedBytes += current.rx
		result.TransmittedBytes += current.tx
		if old, ok := n.previous[name]; ok && seconds > 0 {
			// A reset in either direction starts a fresh baseline for this interface.
			if current.rx >= old.rx && current.tx >= old.tx {
				result.ReceivedBytesPerSecond += float64(current.rx-old.rx) / seconds
				result.SentBytesPerSecond += float64(current.tx-old.tx) / seconds
			} else {
				result.RateUnavailable = true
			}
		} else {
			result.RateUnavailable = true
		}
	}
	sort.Strings(result.Interfaces)
	n.previous = counters
	n.at = now
	return result, nil
}

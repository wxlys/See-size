package collector

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

type cpuTimes struct {
	total uint64
	idle  uint64
}

func parseCPUStat(input string) (cpuTimes, error) {
	line, _, _ := strings.Cut(input, "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, fmt.Errorf("invalid /proc/stat cpu line")
	}

	var values []uint64
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuTimes{}, fmt.Errorf("parse cpu counter %q: %w", field, err)
		}
		values = append(values, value)
	}

	var total uint64
	for _, value := range values {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return cpuTimes{total: total, idle: idle}, nil
}

func cpuPercent(previous, current cpuTimes) float64 {
	if current.total <= previous.total || current.idle < previous.idle {
		return 0
	}
	totalDelta := current.total - previous.total
	idleDelta := current.idle - previous.idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}
	return 100 * float64(totalDelta-idleDelta) / float64(totalDelta)
}

func parseMemInfo(input string) (map[string]uint64, error) {
	result := make(map[string]uint64)
	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse meminfo %s: %w", key, err)
		}
		if len(fields) >= 3 && fields[2] == "kB" {
			value *= 1024
		}
		result[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if result["MemTotal"] == 0 {
		return nil, fmt.Errorf("MemTotal missing from /proc/meminfo")
	}
	return result, nil
}

func parseNetDev(input string) (received, sent uint64, err error) {
	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		name, data, _ := strings.Cut(line, ":")
		if strings.TrimSpace(name) == "lo" {
			continue
		}
		fields := strings.Fields(data)
		if len(fields) < 9 {
			continue
		}
		rx, parseErr := strconv.ParseUint(fields[0], 10, 64)
		if parseErr != nil {
			return 0, 0, parseErr
		}
		tx, parseErr := strconv.ParseUint(fields[8], 10, 64)
		if parseErr != nil {
			return 0, 0, parseErr
		}
		received += rx
		sent += tx
	}
	return received, sent, scanner.Err()
}

func parseOSRelease(input string) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		result[key] = strings.Trim(strings.TrimSpace(value), "\"")
	}
	return result
}

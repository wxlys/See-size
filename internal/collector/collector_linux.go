//go:build linux

package collector

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/seesize/seesize/internal/model"
)

type Collector struct {
	previousCPU      cpuTimes
	previousReceived uint64
	previousSent     uint64
	previousAt       time.Time
	hasPrevious      bool
}

func New() *Collector { return &Collector{} }

func (c *Collector) Collect(agentID, agentVersion string) (model.Heartbeat, error) {
	now := time.Now().UTC()
	hostname, err := os.Hostname()
	if err != nil {
		return model.Heartbeat{}, fmt.Errorf("hostname: %w", err)
	}

	osReleaseBytes, _ := os.ReadFile("/etc/os-release")
	osRelease := parseOSRelease(string(osReleaseBytes))
	kernelBytes, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	uptimeBytes, _ := os.ReadFile("/proc/uptime")
	loadBytes, _ := os.ReadFile("/proc/loadavg")
	statBytes, err := os.ReadFile("/proc/stat")
	if err != nil {
		return model.Heartbeat{}, fmt.Errorf("read /proc/stat: %w", err)
	}
	currentCPU, err := parseCPUStat(string(statBytes))
	if err != nil {
		return model.Heartbeat{}, err
	}

	memBytes, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return model.Heartbeat{}, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	mem, err := parseMemInfo(string(memBytes))
	if err != nil {
		return model.Heartbeat{}, err
	}

	netBytes, _ := os.ReadFile("/proc/net/dev")
	received, sent, _ := parseNetDev(string(netBytes))

	var diskStat syscall.Statfs_t
	if err := syscall.Statfs("/", &diskStat); err != nil {
		return model.Heartbeat{}, fmt.Errorf("stat root filesystem: %w", err)
	}
	blockSize := uint64(diskStat.Bsize)
	totalDisk := diskStat.Blocks * blockSize
	availableDisk := diskStat.Bavail * blockSize
	freeDisk := diskStat.Bfree * blockSize
	usedDisk := totalDisk - freeDisk

	var cpuUsage, rxRate, txRate float64
	if c.hasPrevious {
		cpuUsage = cpuPercent(c.previousCPU, currentCPU)
		seconds := now.Sub(c.previousAt).Seconds()
		if seconds > 0 {
			if received >= c.previousReceived {
				rxRate = float64(received-c.previousReceived) / seconds
			}
			if sent >= c.previousSent {
				txRate = float64(sent-c.previousSent) / seconds
			}
		}
	}
	c.previousCPU = currentCPU
	c.previousReceived = received
	c.previousSent = sent
	c.previousAt = now
	c.hasPrevious = true

	uptime, _ := strconv.ParseFloat(strings.Fields(string(uptimeBytes))[0], 64)
	loadFields := strings.Fields(string(loadBytes))
	var load1, load5, load15 float64
	if len(loadFields) >= 3 {
		load1, _ = strconv.ParseFloat(loadFields[0], 64)
		load5, _ = strconv.ParseFloat(loadFields[1], 64)
		load15, _ = strconv.ParseFloat(loadFields[2], 64)
	}

	totalMemory := mem["MemTotal"]
	availableMemory := mem["MemAvailable"]
	if availableMemory == 0 {
		availableMemory = mem["MemFree"] + mem["Buffers"] + mem["Cached"]
	}
	swapUsed := uint64(0)
	if mem["SwapTotal"] >= mem["SwapFree"] {
		swapUsed = mem["SwapTotal"] - mem["SwapFree"]
	}

	return model.Heartbeat{
		ProtocolVersion: model.ProtocolVersion,
		AgentID:         agentID,
		AgentVersion:    agentVersion,
		Hostname:        hostname,
		OS:              runtime.GOOS,
		Distribution:    osRelease["NAME"],
		SystemVersion:   osRelease["VERSION_ID"],
		KernelVersion:   strings.TrimSpace(string(kernelBytes)),
		Architecture:    runtime.GOARCH,
		IPAddresses:     ipAddresses(),
		UptimeSeconds:   uptime,
		CollectedAt:     now,
		Metrics: model.Metrics{
			CPUPercent: cpuUsage,
			Load1:      load1,
			Load5:      load5,
			Load15:     load15,
			Memory: model.MemoryMetrics{
				TotalBytes:     totalMemory,
				UsedBytes:      totalMemory - availableMemory,
				AvailableBytes: availableMemory,
				CachedBytes:    mem["Cached"] + mem["SReclaimable"],
				BuffersBytes:   mem["Buffers"],
				SwapTotalBytes: mem["SwapTotal"],
				SwapUsedBytes:  swapUsed,
			},
			RootDisk: model.DiskMetrics{
				MountPoint:     "/",
				TotalBytes:     totalDisk,
				UsedBytes:      usedDisk,
				AvailableBytes: availableDisk,
			},
			Network: model.NetworkMetrics{
				ReceivedBytes:          received,
				TransmittedBytes:       sent,
				ReceivedBytesPerSecond: rxRate,
				SentBytesPerSecond:     txRate,
			},
		},
	}, nil
}

package model

import "time"

const ProtocolVersion = "v1"

type MemoryMetrics struct {
	TotalBytes     uint64 `json:"total_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	CachedBytes    uint64 `json:"cached_bytes"`
	BuffersBytes   uint64 `json:"buffers_bytes"`
	SwapTotalBytes uint64 `json:"swap_total_bytes"`
	SwapUsedBytes  uint64 `json:"swap_used_bytes"`
}

type DiskMetrics struct {
	MountPoint     string `json:"mount_point"`
	TotalBytes     uint64 `json:"total_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

type NetworkMetrics struct {
	Scope                  string   `json:"scope,omitempty"`
	Interfaces             []string `json:"interfaces,omitempty"`
	MissingInterfaces      []string `json:"missing_interfaces,omitempty"`
	ReceivedBytes          uint64   `json:"received_bytes"`
	TransmittedBytes       uint64   `json:"transmitted_bytes"`
	ReceivedBytesPerSecond float64  `json:"received_bytes_per_second"`
	SentBytesPerSecond     float64  `json:"sent_bytes_per_second"`
}

type Metrics struct {
	CPUPercent float64        `json:"cpu_percent"`
	Load1      float64        `json:"load_1"`
	Load5      float64        `json:"load_5"`
	Load15     float64        `json:"load_15"`
	Memory     MemoryMetrics  `json:"memory"`
	RootDisk   DiskMetrics    `json:"root_disk"`
	Network    NetworkMetrics `json:"network"`
}

type Heartbeat struct {
	ProtocolVersion string    `json:"protocol_version"`
	AgentID         string    `json:"agent_id"`
	AgentVersion    string    `json:"agent_version"`
	Hostname        string    `json:"hostname"`
	OS              string    `json:"os"`
	Distribution    string    `json:"distribution"`
	SystemVersion   string    `json:"system_version"`
	KernelVersion   string    `json:"kernel_version"`
	Architecture    string    `json:"architecture"`
	IPAddresses     []string  `json:"ip_addresses"`
	UptimeSeconds   float64   `json:"uptime_seconds"`
	CollectedAt     time.Time `json:"collected_at"`
	Metrics         Metrics   `json:"metrics"`
}

type Server struct {
	Heartbeat
	LastSeen   time.Time `json:"last_seen"`
	Online     bool      `json:"online"`
	ObservedIP string    `json:"observed_ip"`
}

type MetricSample struct {
	AgentID     string    `json:"agent_id"`
	CollectedAt time.Time `json:"collected_at"`
	Metrics     Metrics   `json:"metrics"`
}

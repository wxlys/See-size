//go:build !linux

package collector

import (
	"os"
	"runtime"
	"time"

	"github.com/seesize/seesize/internal/model"
)

type Collector struct{}

func New() *Collector { return &Collector{} }

func (c *Collector) Collect(agentID, agentVersion string) (model.Heartbeat, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return model.Heartbeat{}, err
	}
	return model.Heartbeat{
		ProtocolVersion: model.ProtocolVersion,
		AgentID:         agentID,
		AgentVersion:    agentVersion,
		Hostname:        hostname,
		OS:              runtime.GOOS,
		Architecture:    runtime.GOARCH,
		IPAddresses:     ipAddresses(),
		CollectedAt:     time.Now().UTC(),
	}, nil
}

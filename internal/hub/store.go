package hub

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/seesize/seesize/internal/model"
)

type MetricStore interface {
	Upsert(context.Context, model.Heartbeat, string, time.Time) error
	List(context.Context, time.Time) ([]model.Server, error)
	History(context.Context, string, time.Time, time.Time, int) ([]model.MetricSample, error)
}

type Store struct {
	mu      sync.RWMutex
	servers map[string]model.Server
	offline time.Duration
}

func NewStore(offlineAfter time.Duration) *Store {
	return &Store{servers: make(map[string]model.Server), offline: offlineAfter}
}

func (s *Store) Upsert(_ context.Context, heartbeat model.Heartbeat, observedIP string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.servers[heartbeat.AgentID] = model.Server{
		Heartbeat:  heartbeat,
		LastSeen:   now,
		Online:     true,
		ObservedIP: observedIP,
	}
	return nil
}

func (s *Store) List(_ context.Context, now time.Time) ([]model.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	servers := make([]model.Server, 0, len(s.servers))
	for _, server := range s.servers {
		server.Online = now.Sub(server.LastSeen) <= s.offline
		servers = append(servers, server)
	}
	sort.Slice(servers, func(i, j int) bool {
		if servers[i].Online != servers[j].Online {
			return servers[i].Online
		}
		return servers[i].Hostname < servers[j].Hostname
	})
	return servers, nil
}

func (s *Store) History(_ context.Context, agentID string, from, to time.Time, limit int) ([]model.MetricSample, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	server, ok := s.servers[agentID]
	if !ok || server.CollectedAt.Before(from) || server.CollectedAt.After(to) || limit == 0 {
		return []model.MetricSample{}, nil
	}
	return []model.MetricSample{{
		AgentID:     agentID,
		CollectedAt: server.CollectedAt,
		Metrics:     server.Metrics,
	}}, nil
}

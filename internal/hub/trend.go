package hub

import (
	"context"
	"encoding/json"
	"github.com/seesize/seesize/internal/model"
	"net/http"
	"time"
)

// Values are CPU %, memory %, disk %, download B/s, upload B/s.
type TrendPoint struct {
	NetworkUnavailable bool       `json:"network_unavailable"`
	NetworkScope       string     `json:"network_scope"`
	At                 time.Time  `json:"at"`
	Count              int        `json:"count"`
	Average            [5]float64 `json:"average"`
	Peak               [5]float64 `json:"peak"`
}
type Trend struct {
	From        time.Time    `json:"from"`
	To          time.Time    `json:"to"`
	First       *time.Time   `json:"first"`
	Last        *time.Time   `json:"last"`
	StepSeconds float64      `json:"step_seconds"`
	Points      []TrendPoint `json:"points"`
}

func (s *SQLiteStore) Trend(ctx context.Context, id string, from, to time.Time) (Trend, error) {
	step := to.Sub(from) / 360
	result := Trend{From: from, To: to, StepSeconds: step.Seconds(), Points: []TrendPoint{}}
	buckets := make([]TrendPoint, 360)
	rows, err := s.db.QueryContext(ctx, `SELECT collected_at_ns,metrics_json FROM metric_samples WHERE agent_id=? AND collected_at_ns>=? AND collected_at_ns<? ORDER BY collected_at_ns`, id, from.UnixNano(), to.UnixNano())
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var ns int64
		var body []byte
		if err := rows.Scan(&ns, &body); err != nil {
			return result, err
		}
		var m model.Metrics
		if err := json.Unmarshal(body, &m); err != nil {
			return result, err
		}
		at := time.Unix(0, ns).UTC()
		if result.First == nil {
			first := at
			result.First = &first
		}
		last := at
		result.Last = &last
		index := int(at.Sub(from) / step)
		if index < 0 || index >= 360 {
			continue
		}
		b := &buckets[index]
		scopeBytes, _ := json.Marshal([]any{m.Network.Scope, m.Network.Interfaces})
		scope := string(scopeBytes)
		if b.Count == 0 {
			b.NetworkScope = scope
		} else if b.NetworkScope != scope {
			b.NetworkUnavailable = true
		}
		if m.Network.RateUnavailable || m.Network.Scope == "unavailable" || len(m.Network.MissingInterfaces) > 0 {
			b.NetworkUnavailable = true
		}
		b.At = from.Add(time.Duration(index) * step)
		b.Count++
		values := [5]float64{m.CPUPercent, 0, 0, m.Network.ReceivedBytesPerSecond, m.Network.SentBytesPerSecond}
		if m.Memory.TotalBytes > 0 {
			values[1] = float64(m.Memory.UsedBytes) / float64(m.Memory.TotalBytes) * 100
		}
		if m.RootDisk.TotalBytes > 0 {
			values[2] = float64(m.RootDisk.UsedBytes) / float64(m.RootDisk.TotalBytes) * 100
		}
		for i, v := range values {
			b.Average[i] += v
			if v > b.Peak[i] {
				b.Peak[i] = v
			}
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	for _, b := range buckets {
		if b.Count > 0 {
			for i := range b.Average {
				b.Average[i] /= float64(b.Count)
			}
			result.Points = append(result.Points, b)
		}
	}
	return result, nil
}
func (s *Server) handleTrend(w http.ResponseWriter, r *http.Request) {
	duration := time.Hour
	switch r.URL.Query().Get("range") {
	case "", "1h":
	case "30m":
		duration = 30 * time.Minute
	case "24h":
		duration = 24 * time.Hour
	default:
		writeJSON(w, 400, map[string]string{"error": "range must be 30m, 1h or 24h"})
		return
	}
	store, ok := s.store.(interface {
		Trend(context.Context, string, time.Time, time.Time) (Trend, error)
	})
	if !ok {
		writeJSON(w, 503, map[string]string{"error": "trend storage unavailable"})
		return
	}
	to := s.now().UTC()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := store.Trend(ctx, r.PathValue("agentID"), to.Add(-duration), to)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to load trend"})
		return
	}
	writeJSON(w, 200, result)
}

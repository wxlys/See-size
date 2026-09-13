package disk

import (
	"context"
	"time"
)

// Schedule runs immediately and waits a full interval after each job. Jobs
// never overlap or build a backlog, even when a filesystem operation is slow.
func Schedule(ctx context.Context, interval time.Duration, job func(context.Context)) {
	if interval <= 0 {
		return
	}
	for ctx.Err() == nil {
		job(ctx)
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

type GrowthAlert struct {
	Path  string    `json:"path"`
	Delta int64     `json:"delta"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
}

// GrowthAlerts expresses net growth over the actual scan interval, not an
// instantaneous spike or a claim about its cause. Parent and child totals overlap.
func GrowthAlerts(previous, current Snapshot, threshold int64) []GrowthAlert {
	out := []GrowthAlert{}
	if threshold <= 0 || !current.At.After(previous.At) {
		return out
	}
	changes, ok := Compare(previous, current)
	if !ok {
		return out
	}
	for _, c := range changes {
		if c.Delta >= threshold {
			out = append(out, GrowthAlert{c.Path, c.Delta, previous.At, current.At})
		}
	}
	return out
}

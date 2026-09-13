package hub

import (
	"context"
	"fmt"
	"time"
)

type CleanupResult struct {
	Events    int64 `json:"events_deleted"`
	Metrics   int64 `json:"metrics_deleted"`
	Snapshots int64 `json:"snapshots_deleted"`
}

// CleanupBatch removes a bounded number of expired records from each table.
// Inventory is intentionally retained, including offline servers.
func (s *SQLiteStore) CleanupBatch(ctx context.Context, now time.Time, retention time.Duration, batch int) (CleanupResult, error) {
	result := CleanupResult{}
	if retention <= 0 || batch < 1 || batch > 10000 {
		return result, fmt.Errorf("positive retention and batch size 1..10000 required")
	}
	cutoff := now.Add(-retention).UnixNano()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM metric_samples WHERE (agent_id,collected_at_ns) IN (SELECT agent_id,collected_at_ns FROM metric_samples WHERE collected_at_ns < ? ORDER BY collected_at_ns LIMIT ?)`, cutoff, batch)
	if err != nil {
		return CleanupResult{}, err
	}
	result.Metrics, err = res.RowsAffected()
	if err != nil {
		return CleanupResult{}, err
	}
	res, err = tx.ExecContext(ctx, `DELETE FROM disk_snapshots WHERE (agent_id,root,at_ns) IN (SELECT agent_id,root,at_ns FROM disk_snapshots WHERE at_ns < ? ORDER BY at_ns LIMIT ?)`, cutoff, batch)
	if err != nil {
		return CleanupResult{}, err
	}
	result.Snapshots, err = res.RowsAffected()
	if err != nil {
		return CleanupResult{}, err
	}
	res, err = tx.ExecContext(ctx, `DELETE FROM disk_events WHERE id IN (SELECT id FROM disk_events WHERE to_ns < ? ORDER BY to_ns LIMIT ?)`, cutoff, batch)
	if err != nil {
		return CleanupResult{}, err
	}
	result.Events, err = res.RowsAffected()
	if err != nil {
		return CleanupResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return CleanupResult{}, err
	}
	return result, nil
}

// RunRetention starts with a sweep, then repeats periodically. Batches release
// the database connection between transactions so ingestion can continue.
func (s *SQLiteStore) RunRetention(ctx context.Context, retention, interval time.Duration, report func(CleanupResult, error)) {
	if retention <= 0 || interval <= 0 {
		report(CleanupResult{}, fmt.Errorf("retention and cleanup interval must be positive"))
		return
	}
	sweep := func() {
		total := CleanupResult{}
		work, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		for {
			batch, err := s.CleanupBatch(work, time.Now().UTC(), retention, 1000)
			total.Metrics += batch.Metrics
			total.Snapshots += batch.Snapshots
			total.Events += batch.Events
			if err != nil || (batch.Metrics < 1000 && batch.Snapshots < 1000 && batch.Events < 1000) {
				report(total, err)
				return
			}
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-work.Done():
				timer.Stop()
				report(total, work.Err())
				return
			case <-timer.C:
			}
		}
	}
	sweep()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}

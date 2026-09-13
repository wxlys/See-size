package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/seesize/seesize/internal/model"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db      *sql.DB
	offline time.Duration
}

func OpenSQLite(path string, offlineAfter time.Duration) (*SQLiteStore, error) {
	if path == "" {
		return nil, fmt.Errorf("database path must not be empty")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// A single connection keeps connection-scoped SQLite pragmas deterministic
	// and is sufficient for the prototype's one-writer workload.
	db.SetMaxOpenConns(1)
	store := &SQLiteStore{db: db, offline: offlineAfter}
	if err := store.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) initialize(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS disk_snapshots (agent_id TEXT NOT NULL, root TEXT NOT NULL, at_ns INTEGER NOT NULL, snapshot_json BLOB NOT NULL, PRIMARY KEY(agent_id,root,at_ns))`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS servers (
			agent_id TEXT PRIMARY KEY,
			heartbeat_json BLOB NOT NULL,
			observed_ip TEXT NOT NULL,
			last_seen_ns INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS metric_samples (
			agent_id TEXT NOT NULL,
			collected_at_ns INTEGER NOT NULL,
			metrics_json BLOB NOT NULL,
			PRIMARY KEY (agent_id, collected_at_ns),
			FOREIGN KEY (agent_id) REFERENCES servers(agent_id) ON DELETE CASCADE
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_metric_samples_time ON metric_samples(collected_at_ns)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize database: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Upsert(ctx context.Context, heartbeat model.Heartbeat, observedIP string, now time.Time) error {
	heartbeatJSON, err := json.Marshal(heartbeat)
	if err != nil {
		return fmt.Errorf("encode heartbeat: %w", err)
	}
	metricsJSON, err := json.Marshal(heartbeat.Metrics)
	if err != nil {
		return fmt.Errorf("encode metrics: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin heartbeat transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO servers(agent_id, heartbeat_json, observed_ip, last_seen_ns)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			heartbeat_json = excluded.heartbeat_json,
			observed_ip = excluded.observed_ip,
			last_seen_ns = excluded.last_seen_ns`,
		heartbeat.AgentID, heartbeatJSON, observedIP, now.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("store server: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO metric_samples(agent_id, collected_at_ns, metrics_json)
		VALUES (?, ?, ?)
		ON CONFLICT(agent_id, collected_at_ns) DO UPDATE SET metrics_json = excluded.metrics_json`,
		heartbeat.AgentID, heartbeat.CollectedAt.UTC().UnixNano(), metricsJSON)
	if err != nil {
		return fmt.Errorf("store metric sample: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit heartbeat transaction: %w", err)
	}
	return nil
}

func (s *SQLiteStore) List(ctx context.Context, now time.Time) ([]model.Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT heartbeat_json, observed_ip, last_seen_ns FROM servers`)
	if err != nil {
		return nil, fmt.Errorf("query servers: %w", err)
	}
	defer rows.Close()
	servers := make([]model.Server, 0)
	for rows.Next() {
		var heartbeatJSON []byte
		var observedIP string
		var lastSeenNS int64
		if err := rows.Scan(&heartbeatJSON, &observedIP, &lastSeenNS); err != nil {
			return nil, fmt.Errorf("scan server: %w", err)
		}
		var heartbeat model.Heartbeat
		if err := json.Unmarshal(heartbeatJSON, &heartbeat); err != nil {
			return nil, fmt.Errorf("decode stored heartbeat: %w", err)
		}
		lastSeen := time.Unix(0, lastSeenNS).UTC()
		servers = append(servers, model.Server{
			Heartbeat:  heartbeat,
			LastSeen:   lastSeen,
			Online:     now.Sub(lastSeen) <= s.offline,
			ObservedIP: observedIP,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate servers: %w", err)
	}
	sort.Slice(servers, func(i, j int) bool {
		if servers[i].Online != servers[j].Online {
			return servers[i].Online
		}
		return servers[i].Hostname < servers[j].Hostname
	})
	return servers, nil
}

func (s *SQLiteStore) History(ctx context.Context, agentID string, from, to time.Time, limit int) ([]model.MetricSample, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT collected_at_ns, metrics_json
		FROM (
			SELECT collected_at_ns, metrics_json
			FROM metric_samples
			WHERE agent_id = ? AND collected_at_ns BETWEEN ? AND ?
			ORDER BY collected_at_ns DESC
			LIMIT ?
		)
		ORDER BY collected_at_ns ASC`, agentID, from.UTC().UnixNano(), to.UTC().UnixNano(), limit)
	if err != nil {
		return nil, fmt.Errorf("query metric history: %w", err)
	}
	defer rows.Close()
	samples := make([]model.MetricSample, 0)
	for rows.Next() {
		var collectedAtNS int64
		var metricsJSON []byte
		if err := rows.Scan(&collectedAtNS, &metricsJSON); err != nil {
			return nil, fmt.Errorf("scan metric sample: %w", err)
		}
		var metrics model.Metrics
		if err := json.Unmarshal(metricsJSON, &metrics); err != nil {
			return nil, fmt.Errorf("decode stored metrics: %w", err)
		}
		samples = append(samples, model.MetricSample{
			AgentID:     agentID,
			CollectedAt: time.Unix(0, collectedAtNS).UTC(),
			Metrics:     metrics,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate metric history: %w", err)
	}
	return samples, nil
}

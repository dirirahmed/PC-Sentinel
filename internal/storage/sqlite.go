// Package storage persists downsampled metric history and alerts in a local
// SQLite database. It uses a pure-Go (WebAssembly) SQLite build, so the
// Windows binary needs no C toolchain or DLLs.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

const schemaVersion = 1

const schema = `
CREATE TABLE IF NOT EXISTS samples (
	ts         INTEGER PRIMARY KEY,
	cpu        REAL NOT NULL,
	cpu_max    REAL NOT NULL,
	mem        REAL NOT NULL,
	mem_used   INTEGER NOT NULL,
	gpu        REAL,
	gpu_max    REAL,
	cpu_temp   REAL,
	gpu_temp   REAL,
	net_rx     REAL NOT NULL,
	net_tx     REAL NOT NULL,
	disk_read  REAL,
	disk_write REAL
);
CREATE TABLE IF NOT EXISTS disk_usage (
	ts           INTEGER NOT NULL,
	mount        TEXT NOT NULL,
	used_percent REAL NOT NULL,
	free_bytes   INTEGER NOT NULL,
	PRIMARY KEY (ts, mount)
);
CREATE TABLE IF NOT EXISTS alerts (
	id          TEXT PRIMARY KEY,
	rule        TEXT NOT NULL,
	component   TEXT NOT NULL,
	target      TEXT NOT NULL DEFAULT '',
	severity    TEXT NOT NULL,
	title       TEXT NOT NULL,
	reason      TEXT NOT NULL,
	value       REAL NOT NULL,
	threshold   REAL NOT NULL,
	started_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL,
	resolved_at INTEGER
);
CREATE INDEX IF NOT EXISTS alerts_started_at ON alerts (started_at);
`

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	dsn := (&url.URL{Scheme: "file", OmitHost: true, Path: filepath.ToSlash(path),
		RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(wal)&_pragma=synchronous(normal)"}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serialises writers and keeps the WASM runtime's memory small.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("database schema version %d is newer than this build supports (%d)", version, schemaVersion)
	}
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
	return err
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) InsertSample(ctx context.Context, p models.HistoryPoint) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO samples
		(ts, cpu, cpu_max, mem, mem_used, gpu, gpu_max, cpu_temp, gpu_temp, net_rx, net_tx, disk_read, disk_write)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Timestamp.Unix(), p.CPU, p.CPUMax, p.Memory, int64(p.MemoryUsed), nullable(p.GPU), nullable(p.GPUMax),
		nullable(p.CPUTemp), nullable(p.GPUTemp), p.NetRx, p.NetTx, nullable(p.DiskRead), nullable(p.DiskWrite))
	return err
}

// QuerySamples returns history between from and to, averaged into buckets
// of the given width. Peak columns keep the bucket maximum.
func (s *Store) QuerySamples(ctx context.Context, from, to time.Time, bucket time.Duration) ([]models.HistoryPoint, error) {
	width := max(int64(bucket.Seconds()), 1)
	rows, err := s.db.QueryContext(ctx, `SELECT (ts / ?) * ? AS b,
			AVG(cpu), MAX(cpu_max), AVG(mem), AVG(mem_used), AVG(gpu), MAX(gpu_max),
			AVG(cpu_temp), AVG(gpu_temp), AVG(net_rx), AVG(net_tx), AVG(disk_read), AVG(disk_write)
		FROM samples WHERE ts >= ? AND ts <= ? GROUP BY b ORDER BY b`,
		width, width, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := []models.HistoryPoint{}
	for rows.Next() {
		var ts int64
		var memUsed float64
		var p models.HistoryPoint
		var gpu, gpuMax, cpuT, gpuT, dr, dw sql.NullFloat64
		if err := rows.Scan(&ts, &p.CPU, &p.CPUMax, &p.Memory, &memUsed, &gpu, &gpuMax, &cpuT, &gpuT,
			&p.NetRx, &p.NetTx, &dr, &dw); err != nil {
			return nil, err
		}
		p.Timestamp = time.Unix(ts, 0).UTC()
		p.MemoryUsed = uint64(memUsed)
		p.GPU, p.GPUMax, p.CPUTemp, p.GPUTemp = fromNull(gpu), fromNull(gpuMax), fromNull(cpuT), fromNull(gpuT)
		p.DiskRead, p.DiskWrite = fromNull(dr), fromNull(dw)
		points = append(points, p)
	}
	return points, rows.Err()
}

func (s *Store) InsertDiskUsage(ctx context.Context, at time.Time, disks []models.DiskStats) error {
	if len(disks) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, d := range disks {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO disk_usage (ts, mount, used_percent, free_bytes) VALUES (?, ?, ?, ?)`,
			at.Unix(), d.Mountpoint, d.UsedPercent, int64(d.FreeBytes)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) QueryDiskUsage(ctx context.Context, from, to time.Time, bucket time.Duration) ([]models.DiskUsagePoint, error) {
	width := max(int64(bucket.Seconds()), 1)
	rows, err := s.db.QueryContext(ctx, `SELECT (ts / ?) * ? AS b, mount, AVG(used_percent), MIN(free_bytes)
		FROM disk_usage WHERE ts >= ? AND ts <= ? GROUP BY b, mount ORDER BY b, mount`,
		width, width, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.DiskUsagePoint{}
	for rows.Next() {
		var ts, free int64
		var p models.DiskUsagePoint
		if err := rows.Scan(&ts, &p.Mountpoint, &p.UsedPercent, &free); err != nil {
			return nil, err
		}
		p.Timestamp, p.FreeBytes = time.Unix(ts, 0).UTC(), uint64(max(free, 0))
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SaveAlert(ctx context.Context, a models.Alert) error {
	var resolved any
	if a.ResolvedAt != nil {
		resolved = a.ResolvedAt.Unix()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO alerts
		(id, rule, component, target, severity, title, reason, value, threshold, started_at, updated_at, resolved_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET severity = excluded.severity, reason = excluded.reason,
			value = excluded.value, threshold = excluded.threshold,
			updated_at = excluded.updated_at, resolved_at = excluded.resolved_at`,
		a.ID, a.Rule, a.Component, a.Target, string(a.Severity), a.Title, a.Reason, a.Value, a.Threshold,
		a.StartedAt.Unix(), a.UpdatedAt.Unix(), resolved)
	return err
}

// ListAlerts returns the most recent alerts first.
func (s *Store) ListAlerts(ctx context.Context, since time.Time, limit int) ([]models.Alert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, rule, component, target, severity, title, reason, value, threshold,
			started_at, updated_at, resolved_at
		FROM alerts WHERE started_at >= ? ORDER BY started_at DESC LIMIT ?`, since.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Alert{}
	for rows.Next() {
		var a models.Alert
		var sev string
		var started, updated int64
		var resolved sql.NullInt64
		if err := rows.Scan(&a.ID, &a.Rule, &a.Component, &a.Target, &sev, &a.Title, &a.Reason, &a.Value,
			&a.Threshold, &started, &updated, &resolved); err != nil {
			return nil, err
		}
		a.Severity = models.Severity(sev)
		a.StartedAt, a.UpdatedAt = time.Unix(started, 0).UTC(), time.Unix(updated, 0).UTC()
		if resolved.Valid {
			t := time.Unix(resolved.Int64, 0).UTC()
			a.ResolvedAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CloseDanglingAlerts resolves alerts left open by a previous run that exited
// without resolving them; their state can't be known after a restart.
func (s *Store) CloseDanglingAlerts(ctx context.Context, at time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET resolved_at = ?, updated_at = ?,
		reason = reason || ' (closed at restart)' WHERE resolved_at IS NULL`, at.Unix(), at.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Prune deletes everything older than the retention cutoff.
func (s *Store) Prune(ctx context.Context, before time.Time) (int64, error) {
	cutoff := before.Unix()
	var total int64
	for _, q := range []string{
		`DELETE FROM samples WHERE ts < ?`,
		`DELETE FROM disk_usage WHERE ts < ?`,
		`DELETE FROM alerts WHERE resolved_at IS NOT NULL AND resolved_at < ?`,
	} {
		res, err := s.db.ExecContext(ctx, q, cutoff)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}

var ErrUnavailable = errors.New("history storage is unavailable")

func nullable(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func fromNull(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

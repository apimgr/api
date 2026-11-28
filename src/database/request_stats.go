package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// RequestStatsBucket is one absolute UTC hour of public request activity.
type RequestStatsBucket struct {
	Hour  int64
	Count int64
}

// RequestStatsSnapshot contains the persisted lifetime total and recent hourly
// buckets used to restore the health endpoint's rolling 24-hour view.
type RequestStatsSnapshot struct {
	Total   int64
	Buckets []RequestStatsBucket
}

// LoadRequestStats reads the persisted lifetime request total and all hourly
// buckets at or after oldestHour.
func LoadRequestStats(ctx context.Context, oldestHour int64) (RequestStatsSnapshot, error) {
	db := GetServerDB()
	if db == nil {
		return RequestStatsSnapshot{}, errors.New("request statistics database is unavailable")
	}
	table := TableName("server_stats")
	var snapshot RequestStatsSnapshot
	if err := queryRowContext(ctx, db, fmt.Sprintf(`SELECT requests_total FROM %s WHERE id = 1`, table)).Scan(&snapshot.Total); err != nil {
		return RequestStatsSnapshot{}, fmt.Errorf("load request statistics total: %w", err)
	}
	rows, err := queryContext(ctx, db, fmt.Sprintf(`SELECT hour, count FROM %s WHERE hour >= ? ORDER BY hour`, TableName("server_request_hours")), oldestHour)
	if err != nil {
		return RequestStatsSnapshot{}, fmt.Errorf("load hourly request statistics: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var bucket RequestStatsBucket
		if err := rows.Scan(&bucket.Hour, &bucket.Count); err != nil {
			return RequestStatsSnapshot{}, fmt.Errorf("read hourly request statistics: %w", err)
		}
		snapshot.Buckets = append(snapshot.Buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return RequestStatsSnapshot{}, fmt.Errorf("iterate hourly request statistics: %w", err)
	}
	return snapshot, nil
}

// RecordRequestStats atomically persists a batch of request-count deltas and
// prunes hourly buckets older than the rolling window.
func RecordRequestStats(ctx context.Context, totalDelta int64, buckets []RequestStatsBucket, oldestHour int64) error {
	if totalDelta < 0 {
		return errors.New("request statistics total delta cannot be negative")
	}
	if totalDelta == 0 && len(buckets) == 0 {
		return nil
	}
	db := GetServerDB()
	if db == nil {
		return errors.New("request statistics database is unavailable")
	}
	return withWriteTransaction(ctx, db, func(tx *sql.Tx) error {
		if totalDelta > 0 {
			if _, err := execTxContext(ctx, tx, `UPDATE server_stats SET requests_total = requests_total + ? WHERE id = 1`, totalDelta); err != nil {
				return fmt.Errorf("update request statistics total: %w", err)
			}
		}
		for _, bucket := range buckets {
			if bucket.Hour < 0 || bucket.Count <= 0 {
				return errors.New("invalid hourly request statistics bucket")
			}
			if _, err := execTxContext(ctx, tx, `INSERT INTO server_request_hours (hour, count) VALUES (?, ?)
				ON CONFLICT(hour) DO UPDATE SET count = count + excluded.count`, bucket.Hour, bucket.Count); err != nil {
				return fmt.Errorf("update hourly request statistics: %w", err)
			}
		}
		if _, err := execTxContext(ctx, tx, `DELETE FROM server_request_hours WHERE hour < ?`, oldestHour); err != nil {
			return fmt.Errorf("prune hourly request statistics: %w", err)
		}
		return nil
	})
}

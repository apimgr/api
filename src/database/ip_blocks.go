package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"time"
)

// IPBlock is a persisted temporary address block created by automatic abuse
// detection. Permanent blocks remain in server configuration.
type IPBlock struct {
	IP           string
	Reason       string
	BlockedAt    time.Time
	ExpiresAt    time.Time
	OffenseCount int
	AutoBlocked  bool
}

// SaveTemporaryIPBlock stores an automatic block by canonical client address.
func SaveTemporaryIPBlock(ctx context.Context, block IPBlock) error {
	db := serverWriteDB()
	if db == nil {
		return fmt.Errorf("ip blocks: database is not initialized")
	}
	_, err := execContext(ctx, db, `INSERT INTO `+TableName("ip_blocks")+`
		(ip, reason, blocked_at, expires_at, offense_count, auto_blocked)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(ip) DO UPDATE SET reason=excluded.reason, blocked_at=excluded.blocked_at,
			expires_at=excluded.expires_at, offense_count=excluded.offense_count, auto_blocked=excluded.auto_blocked`,
		block.IP, block.Reason, block.BlockedAt.UTC().Unix(), block.ExpiresAt.UTC().Unix(), block.OffenseCount, block.AutoBlocked)
	return err
}

// PurgeExpiredIPBlocks removes expired automatic blocks during server startup.
func PurgeExpiredIPBlocks(ctx context.Context, now time.Time) error {
	_, err := CleanupTemporaryIPBlocks(ctx, now, nil)
	return err
}

// CleanupTemporaryIPBlocks expires timed blocks and removes any block whose
// address has since entered the configured allowlist. The returned records
// let the scheduler emit one audit event per released address.
func CleanupTemporaryIPBlocks(ctx context.Context, now time.Time, allowlist []string) ([]IPBlock, error) {
	db := serverWriteDB()
	if db == nil {
		return nil, fmt.Errorf("ip blocks: database is not initialized")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := queryContext(ctx, tx, `SELECT ip, reason, blocked_at, expires_at, offense_count, auto_blocked FROM `+TableName("ip_blocks"))
	if err != nil {
		return nil, err
	}
	removed := make([]IPBlock, 0)
	for rows.Next() {
		var block IPBlock
		var blockedAt, expiresAt int64
		if err := rows.Scan(&block.IP, &block.Reason, &blockedAt, &expiresAt, &block.OffenseCount, &block.AutoBlocked); err != nil {
			_ = rows.Close()
			return nil, err
		}
		address, parseErr := netip.ParseAddr(block.IP)
		if expiresAt > now.UTC().Unix() && (parseErr != nil || !ipAddressAllowed(address, allowlist)) {
			continue
		}
		block.BlockedAt = time.Unix(blockedAt, 0).UTC()
		block.ExpiresAt = time.Unix(expiresAt, 0).UTC()
		removed = append(removed, block)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, block := range removed {
		if _, err := execTxContext(ctx, tx, `DELETE FROM `+TableName("ip_blocks")+` WHERE ip = ?`, block.IP); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return removed, nil
}

func ipAddressAllowed(address netip.Addr, entries []string) bool {
	address = address.Unmap()
	for _, entry := range entries {
		if prefix, err := netip.ParsePrefix(entry); err == nil && prefix.Contains(address) {
			return true
		}
		if allowed, err := netip.ParseAddr(entry); err == nil && allowed.Unmap() == address {
			return true
		}
	}
	return false
}

// GetActiveIPBlock returns an unexpired persisted block for an address and
// removes its row when the configured expiry has passed.
func GetActiveIPBlock(ctx context.Context, ip string, now time.Time) (*IPBlock, error) {
	db := GetServerDB()
	if db == nil {
		return nil, fmt.Errorf("ip blocks: database is not initialized")
	}
	var block IPBlock
	var blockedAt, expiresAt int64
	err := queryRowContext(ctx, db, `SELECT ip, reason, blocked_at, expires_at, offense_count, auto_blocked
		FROM `+TableName("ip_blocks")+` WHERE ip = ?`, ip).Scan(
		&block.IP, &block.Reason, &blockedAt, &expiresAt, &block.OffenseCount, &block.AutoBlocked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	block.BlockedAt = time.Unix(blockedAt, 0).UTC()
	block.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	if !block.ExpiresAt.After(now.UTC()) {
		if _, err := execContext(ctx, serverWriteDB(), `DELETE FROM `+TableName("ip_blocks")+` WHERE ip = ? AND expires_at <= ?`, ip, now.UTC().Unix()); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return &block, nil
}

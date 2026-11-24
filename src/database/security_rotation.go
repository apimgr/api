package database

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"
)

// ReencryptSecurityReportData eagerly moves AES-encrypted report bodies and
// every researcher contact blob to the current key in one transaction. PGP
// report bodies remain unchanged.
func ReencryptSecurityReportData(ctx context.Context, db *sql.DB, previousKey, currentKey []byte) error {
	if db == nil || len(previousKey) != 32 || len(currentKey) != 32 {
		return errors.New("security report encryption keys must contain exactly 32 bytes")
	}
	securityReportMu.Lock()
	defer securityReportMu.Unlock()
	return withWriteTransaction(ctx, db, func(tx *sql.Tx) error {
		rows, err := queryTxContext(ctx, tx, `SELECT tracking_id, body_enc, body_alg, contact_enc FROM security_reports`)
		if err != nil {
			return fmt.Errorf("security report key rotation: list reports: %w", err)
		}
		type encryptedReport struct {
			id, algorithm string
			body, contact []byte
		}
		var reports []encryptedReport
		for rows.Next() {
			var report encryptedReport
			if err := rows.Scan(&report.id, &report.body, &report.algorithm, &report.contact); err != nil {
				_ = rows.Close()
				return fmt.Errorf("security report key rotation: read report: %w", err)
			}
			reports = append(reports, report)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("security report key rotation: iterate reports: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("security report key rotation: close report rows: %w", err)
		}
		for _, report := range reports {
			body := report.body
			if report.algorithm == "aes-256-gcm" {
				body, err = reencryptAESValue(report.body, previousKey, currentKey)
				if err != nil {
					return fmt.Errorf("security report key rotation: body %s: %w", report.id, err)
				}
			} else if report.algorithm != "pgp" {
				return fmt.Errorf("security report key rotation: unsupported body algorithm for %s", report.id)
			}
			contact, err := reencryptAESValue(report.contact, previousKey, currentKey)
			if err != nil {
				return fmt.Errorf("security report key rotation: contact %s: %w", report.id, err)
			}
			if _, err := execTxContext(ctx, tx, `UPDATE security_reports SET body_enc = ?, contact_enc = ?, updated_at = ? WHERE tracking_id = ?`,
				body, contact, time.Now().UTC().Format(time.RFC3339), report.id); err != nil {
				return fmt.Errorf("security report key rotation: update %s: %w", report.id, err)
			}
		}
		return nil
	})
}

func reencryptAESValue(value, previousKey, currentKey []byte) ([]byte, error) {
	if len(value) == 0 {
		return nil, errors.New("encrypted value is empty")
	}
	if _, err := decryptAESValue(value, currentKey); err == nil {
		return append([]byte(nil), value...), nil
	}
	plaintext, err := decryptAESValue(value, previousKey)
	if err != nil {
		return nil, errors.New("encrypted value cannot be opened with current or previous key")
	}
	block, err := aes.NewCipher(currentKey)
	if err != nil {
		return nil, errors.New("current encryption key is invalid")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("cannot initialize current encryption key")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate encryption nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptAESValue(value, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("invalid encryption key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(value) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("invalid encrypted value")
	}
	nonce, ciphertext := value[:aead.NonceSize()], value[aead.NonceSize():]
	return aead.Open(nil, nonce, ciphertext, nil)
}

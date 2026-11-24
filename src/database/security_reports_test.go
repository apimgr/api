package database

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sealReportTestValue(t *testing.T, key []byte, plaintext string) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	aead, err := cipher.NewGCM(block)
	require.NoError(t, err)
	nonce := make([]byte, aead.NonceSize())
	_, err = rand.Read(nonce)
	require.NoError(t, err)
	return aead.Seal(nonce, nonce, []byte(plaintext), nil)
}

func openReportTestValue(t *testing.T, key, ciphertext []byte) string {
	t.Helper()
	plaintext, err := decryptAESValue(ciphertext, key)
	require.NoError(t, err)
	return string(plaintext)
}

func TestReencryptSecurityReportAESMovesBodyAndContactToCurrentKey(t *testing.T) {
	ctx := context.Background()
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	input, _ := newSecurityReportTestInput(t, time.Now().UTC())
	input.Body = sealReportTestValue(t, oldKey, "private report")
	input.Contact = sealReportTestValue(t, oldKey, "researcher@example.test")
	require.NoError(t, StoreSecurityReport(ctx, input))

	require.NoError(t, ReencryptSecurityReportData(ctx, GetServerDB(), oldKey, newKey))
	body, algorithm, err := GetSecurityReportEncryptedBody(ctx, input.TrackingID)
	require.NoError(t, err)
	require.Equal(t, "aes-256-gcm", algorithm)
	require.Equal(t, "private report", openReportTestValue(t, newKey, body))
	contact, err := GetSecurityReportContactCiphertext(ctx, input.TrackingID)
	require.NoError(t, err)
	require.Equal(t, "researcher@example.test", openReportTestValue(t, newKey, contact))
	_, err = decryptAESValue(body, oldKey)
	require.Error(t, err)
}

func newSecurityReportTestInput(t *testing.T, now time.Time) (SecurityReportInput, string) {
	t.Helper()
	sum := sha256.Sum256([]byte(t.Name()))
	trackingID := "sec_" + hex.EncodeToString(sum[:8])
	token := "status-token-" + trackingID
	return SecurityReportInput{
		TrackingID: trackingID,
		IDWindow:   now.Unix() / (48 * 60 * 60),
		Severity:   "high",
		Component:  "API",
		Body:       []byte("encrypted-report-ciphertext"),
		BodyAlg:    "aes-256-gcm",
		Contact:    []byte("encrypted-contact-ciphertext"),
		CreditPref: "anon",
		CVE:        true,
		TokenHash:  securityStatusTokenHash(token),
	}, token
}

func TestSecurityReportStoreStatusAndDisclosureLifecycle(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	input, token := newSecurityReportTestInput(t, now)
	require.NoError(t, StoreSecurityReport(ctx, input))

	reports, err := ListSecurityReports(ctx, "received")
	require.NoError(t, err)
	var stored SecurityReport
	for _, report := range reports {
		if report.TrackingID == input.TrackingID {
			stored = report
		}
	}
	require.Equal(t, "received", stored.Status)
	require.Equal(t, "aes-256-gcm", stored.BodyAlg)
	require.NotContains(t, stored.ResearcherComment, "report-ciphertext")
	exportedBody, exportedAlgorithm, err := GetSecurityReportEncryptedBody(ctx, input.TrackingID)
	require.NoError(t, err)
	require.Equal(t, input.BodyAlg, exportedAlgorithm)
	require.Equal(t, input.Body, exportedBody)

	triaged, err := UpdateSecurityReport(ctx, input.TrackingID, SecurityReportUpdate{
		Status:  "triaged",
		Comment: "We are reviewing the report.",
	}, now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, "triaged", triaged.Status)

	status, err := SecurityReportStatusForResearcher(ctx, input.TrackingID, token, now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "triaged", status.Status)
	_, err = SecurityReportStatusForResearcher(ctx, input.TrackingID, token, now.Add(3*time.Hour))
	require.ErrorIs(t, err, ErrSecurityStatusToken)

	_, err = UpdateSecurityReport(ctx, input.TrackingID, SecurityReportUpdate{Status: "received"}, now.Add(4*time.Hour))
	require.Error(t, err)

	disclosed, err := UpdateSecurityReport(ctx, input.TrackingID, SecurityReportUpdate{Status: "disclosed"}, now.Add(5*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "disclosed", disclosed.Status)
	thanks, err := ListSecurityThanks(ctx)
	require.NoError(t, err)
	require.Contains(t, thanks, SecurityThanks{
		TrackingID:  input.TrackingID,
		Credit:      "Anonymous Researcher #1",
		Severity:    "high",
		DisclosedOn: "2026-11-23",
	})
	_, err = UpdateSecurityReport(ctx, input.TrackingID, SecurityReportUpdate{Status: "wont_fix"}, now.Add(6*time.Hour))
	require.Error(t, err)
}

func TestSecurityReportStatusTokenAndRetention(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	input, token := newSecurityReportTestInput(t, now)
	input.CreditPref = "no"
	require.NoError(t, StoreSecurityReport(ctx, input))

	_, err := SecurityReportStatusForResearcher(ctx, input.TrackingID, "wrong-token", now)
	require.ErrorIs(t, err, ErrSecurityStatusToken)
	_, err = UpdateSecurityReport(ctx, input.TrackingID, SecurityReportUpdate{Status: "wont_fix"}, now)
	require.NoError(t, err)
	_, err = SecurityReportStatusForResearcher(ctx, input.TrackingID, token, now.Add(31*24*time.Hour))
	require.ErrorIs(t, err, ErrSecurityReportNotFound)

	deleted, err := PurgeClosedSecurityReports(ctx, now.Add(366*24*time.Hour), 365)
	require.NoError(t, err)
	require.GreaterOrEqual(t, deleted, int64(1))
	_, err = UpdateSecurityReport(ctx, input.TrackingID, SecurityReportUpdate{Status: "wont_fix"}, now.Add(367*24*time.Hour))
	require.True(t, errors.Is(err, ErrSecurityReportNotFound))
}

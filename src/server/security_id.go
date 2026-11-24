package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/apimgr/api/src/database"
)

const securityIDWindow = 48 * time.Hour

// currentSecurityID computes the deterministic identifier published for the
// current 48-hour window without retaining or logging it.
func currentSecurityID(ctx context.Context, now time.Time) (string, int64, error) {
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return "", 0, fmt.Errorf("load installation secret: %w", err)
	}
	window := now.UTC().Unix() / int64(securityIDWindow.Seconds())
	return securityIDForWindow(secret.Value, window), window, nil
}

// validSecurityID accepts the current and preceding 48-hour window.
func validSecurityID(ctx context.Context, supplied string, now time.Time) (int64, bool, error) {
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return 0, false, fmt.Errorf("load installation secret: %w", err)
	}
	current := now.UTC().Unix() / int64(securityIDWindow.Seconds())
	for _, window := range []int64{current, current - 1} {
		if hmac.Equal([]byte(securityIDForWindow(secret.Value, window)), []byte(supplied)) {
			return window, true, nil
		}
	}
	return 0, false, nil
}

func securityIDForWindow(secret []byte, window int64) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(strconv.FormatInt(window, 10)))
	return hex.EncodeToString(mac.Sum(nil)[:8])
}

package tor

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/cretz/bine/torutil"
	bineed25519 "github.com/cretz/bine/torutil/ed25519"
)

// onionAddressAlphabet is the base32 alphabet valid v3 onion addresses are
// built from (lowercase, no padding). Any vanity prefix must be composed
// solely of these characters.
const onionAddressAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

// VanityResult is a matched vanity onion address, along with the ed25519
// private key blob (base64 of the raw 64-byte expanded key) needed to apply
// it via Manager.ApplyKeys. This is the exact on-disk format used by
// hs_ed25519_secret_key elsewhere in this package.
type VanityResult struct {
	Address  string
	Blob     []byte
	Attempts int64
	Elapsed  time.Duration
}

// ValidateVanityPrefix rejects prefixes that could never appear at the start
// of a v3 onion address: empty, too long, or containing characters outside
// the base32 alphabet Tor's onion addresses are encoded with.
func ValidateVanityPrefix(prefix string) error {
	if prefix == "" {
		return fmt.Errorf("prefix must not be empty")
	}
	// 56 chars is the full length of a v3 onion address (without .onion);
	// nothing longer could ever match, and anything beyond a handful of
	// characters is already computationally infeasible.
	if len(prefix) > 24 {
		return fmt.Errorf("prefix too long (max 24 characters)")
	}
	lower := strings.ToLower(prefix)
	for _, c := range lower {
		if !strings.ContainsRune(onionAddressAlphabet, c) {
			return fmt.Errorf("prefix contains invalid character %q (only a-z and 2-7 are valid)", c)
		}
	}
	return nil
}

// GenerateVanityAddress brute-force searches for a v3 onion address whose
// address (excluding the .onion suffix) starts with prefix, generating
// candidate ed25519 keypairs locally (no live Tor process required per
// attempt). It stops on the first match, when maxAttempts is exhausted (if
// > 0), or when ctx is cancelled.
func GenerateVanityAddress(ctx context.Context, prefix string, maxAttempts int64) (*VanityResult, error) {
	if err := ValidateVanityPrefix(prefix); err != nil {
		return nil, err
	}
	prefix = strings.ToLower(prefix)

	start := time.Now()
	var attempts int64

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("vanity search cancelled after %d attempts: %w", attempts, ctx.Err())
		default:
		}

		if maxAttempts > 0 && attempts >= maxAttempts {
			return nil, fmt.Errorf("no match found after %d attempts", attempts)
		}

		kp, err := bineed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("failed to generate candidate key: %w", err)
		}
		attempts++

		address := torutil.OnionServiceIDFromV3PublicKey(kp.PublicKey())
		if strings.HasPrefix(address, prefix) {
			return &VanityResult{
				Address:  address + ".onion",
				Blob:     []byte(base64.StdEncoding.EncodeToString(kp.PrivateKey())),
				Attempts: attempts,
				Elapsed:  time.Since(start),
			}, nil
		}
	}
}

package server

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
)

const securityPublicKeyMaxBytes = 128 << 10

func readSecurityPublicKey() ([]byte, string, error) {
	path := filepath.Join(paths.ConfigDir(), "security", "pgp.pub.asc")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > securityPublicKeyMaxBytes {
		return nil, "", errors.New("public key file is not a valid regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "-----BEGIN PGP PUBLIC KEY BLOCK-----") {
		return nil, "", errors.New("public key file is not ASCII armored")
	}
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(data))
	if err != nil || len(entities) == 0 || entities[0].PrimaryKey == nil {
		return nil, "", errors.New("public key file is invalid")
	}
	return data, strings.ToUpper(hex.EncodeToString(entities[0].PrimaryKey.Fingerprint[:])), nil
}

func securityPublicKeyHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg == nil || !cfg.Web.Security.PublishPGPKey {
			http.NotFound(w, r)
			return
		}
		data, _, err := readSecurityPublicKey()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/pgp-keys; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
	}
}

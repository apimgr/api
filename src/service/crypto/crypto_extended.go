package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Argon2id key-derivation parameters for symmetric encryption (matches
// src/backup/backup.go: password/passphrase -> 256-bit AES-256-GCM key)
const (
	extAESSaltLen   = 32
	extArgon2Time   = 1
	extArgon2Mem    = 64 * 1024
	extArgon2Thrds  = 4
	extArgon2KeyLen = 32
)

// GenerateRSAKeys generates an RSA keypair and returns the PEM-encoded
// PKCS#1 private key and PKIX public key
func GenerateRSAKeys(bits int) (string, string, error) {
	if bits < 2048 {
		bits = 2048
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate RSA key: %w", err)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal RSA public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	return string(privPEM), string(pubPEM), nil
}

// GenerateECDSAKeys generates an ECDSA (P-256) keypair and returns the
// PEM-encoded EC private key and PKIX public key
func GenerateECDSAKeys() (string, string, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate ECDSA key: %w", err)
	}

	privBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal ECDSA private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privBytes,
	})

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal ECDSA public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PUBLIC KEY",
		Bytes: pubBytes,
	})

	return string(privPEM), string(pubPEM), nil
}

// AESEncrypt encrypts plaintext with AES-256-GCM using a key derived from
// the supplied key/passphrase via Argon2id. The random salt and nonce are
// prepended to the ciphertext and the result is base64-encoded.
func AESEncrypt(plaintext, key string) (string, error) {
	salt := make([]byte, extAESSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	derivedKey := argon2.IDKey([]byte(key), salt, extArgon2Time, extArgon2Mem, extArgon2Thrds, extArgon2KeyLen)

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	out := make([]byte, 0, len(salt)+len(nonce)+len(sealed))
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, sealed...)

	return base64.StdEncoding.EncodeToString(out), nil
}

// AESDecrypt decrypts a base64-encoded AES-256-GCM payload produced by
// AESEncrypt, re-deriving the key from the supplied key/passphrase via
// Argon2id using the embedded salt.
func AESDecrypt(ciphertext, key string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid ciphertext encoding: %w", err)
	}

	if len(raw) < extAESSaltLen {
		return "", fmt.Errorf("ciphertext too short")
	}

	salt := raw[:extAESSaltLen]
	rest := raw[extAESSaltLen:]

	derivedKey := argon2.IDKey([]byte(key), salt, extArgon2Time, extArgon2Mem, extArgon2Thrds, extArgon2KeyLen)

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	if len(rest) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce := rest[:gcm.NonceSize()]
	sealed := rest[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}

// RSAEncrypt encrypts plaintext with RSA-OAEP (SHA-256) using a PEM-encoded
// PKIX or PKCS#1 public key, returning base64-encoded ciphertext.
func RSAEncrypt(plaintext, publicKey string) (string, error) {
	pub, err := parseRSAPublicKey(publicKey)
	if err != nil {
		return "", err
	}

	sealed, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, []byte(plaintext), nil)
	if err != nil {
		return "", fmt.Errorf("RSA encryption failed: %w", err)
	}

	return base64.StdEncoding.EncodeToString(sealed), nil
}

// RSADecrypt decrypts a base64-encoded RSA-OAEP (SHA-256) ciphertext using
// a PEM-encoded PKCS#1 private key.
func RSADecrypt(ciphertext, privateKey string) (string, error) {
	priv, err := parseRSAPrivateKey(privateKey)
	if err != nil {
		return "", err
	}

	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid ciphertext encoding: %w", err)
	}

	plaintext, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, raw, nil)
	if err != nil {
		return "", fmt.Errorf("RSA decryption failed: %w", err)
	}

	return string(plaintext), nil
}

// parseRSAPublicKey decodes a PEM-encoded RSA public key, accepting both
// PKIX ("PUBLIC KEY") and PKCS#1 ("RSA PUBLIC KEY") encodings.
func parseRSAPublicKey(publicKey string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(publicKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM public key")
	}

	if pub, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return pub, nil
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	pub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}

	return pub, nil
}

// parseRSAPrivateKey decodes a PEM-encoded RSA private key, accepting both
// PKCS#1 ("RSA PRIVATE KEY") and PKCS#8 ("PRIVATE KEY") encodings.
func parseRSAPrivateKey(privateKey string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM private key")
	}

	if priv, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return priv, nil
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	priv, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA private key")
	}

	return priv, nil
}

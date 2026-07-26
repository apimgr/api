package crypto

import (
	"bytes"
	stdcrypto "crypto"
	"fmt"
	"io"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"
	// Registers RIPEMD160 as an available hash.Hash so that legacy PGP keys
	// with no PreferredHash subpacket (the default emitted by this
	// package's own NewEntity, see openpgp/keys.go) can still be used to
	// encrypt, matching the library's own fallback default.
	_ "golang.org/x/crypto/ripemd160"
)

// pgpConfig pins the self-signature and session-key hash to SHA-256 so
// generated keys never advertise a hash algorithm (e.g. RIPEMD160) that
// this binary's crypto build does not compile in.
func pgpConfig() *packet.Config {
	return &packet.Config{DefaultHash: stdcrypto.SHA256}
}

// GeneratePGPKeys generates an OpenPGP keypair for the given name and email
// and returns the ASCII-armored public and private keys.
func GeneratePGPKeys(name, email string) (string, string, error) {
	if name == "" || email == "" {
		return "", "", fmt.Errorf("name and email are required")
	}

	entity, err := openpgp.NewEntity(name, "", email, pgpConfig())
	if err != nil {
		return "", "", fmt.Errorf("failed to generate PGP key: %w", err)
	}

	var pubBuf bytes.Buffer
	pubWriter, err := armor.Encode(&pubBuf, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to armor public key: %w", err)
	}
	if err := entity.Serialize(pubWriter); err != nil {
		return "", "", fmt.Errorf("failed to serialize public key: %w", err)
	}
	if err := pubWriter.Close(); err != nil {
		return "", "", fmt.Errorf("failed to close public key armor: %w", err)
	}

	var privBuf bytes.Buffer
	privWriter, err := armor.Encode(&privBuf, openpgp.PrivateKeyType, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to armor private key: %w", err)
	}
	if err := entity.SerializePrivate(privWriter, nil); err != nil {
		return "", "", fmt.Errorf("failed to serialize private key: %w", err)
	}
	if err := privWriter.Close(); err != nil {
		return "", "", fmt.Errorf("failed to close private key armor: %w", err)
	}

	return pubBuf.String(), privBuf.String(), nil
}

// PGPEncrypt encrypts plaintext to an ASCII-armored PGP message for the
// holder of the given ASCII-armored public key.
func PGPEncrypt(plaintext, armoredPublicKey string) (string, error) {
	entityList, err := openpgp.ReadArmoredKeyRing(bytes.NewReader([]byte(armoredPublicKey)))
	if err != nil {
		return "", fmt.Errorf("failed to read public key: %w", err)
	}
	if len(entityList) == 0 {
		return "", fmt.Errorf("no public key found in input")
	}

	var outBuf bytes.Buffer
	armorWriter, err := armor.Encode(&outBuf, "PGP MESSAGE", nil)
	if err != nil {
		return "", fmt.Errorf("failed to armor message: %w", err)
	}

	plaintextWriter, err := openpgp.Encrypt(armorWriter, entityList, nil, nil, pgpConfig())
	if err != nil {
		return "", fmt.Errorf("failed to encrypt message: %w", err)
	}
	if _, err := plaintextWriter.Write([]byte(plaintext)); err != nil {
		return "", fmt.Errorf("failed to write plaintext: %w", err)
	}
	if err := plaintextWriter.Close(); err != nil {
		return "", fmt.Errorf("failed to finalize encryption: %w", err)
	}
	if err := armorWriter.Close(); err != nil {
		return "", fmt.Errorf("failed to close message armor: %w", err)
	}

	return outBuf.String(), nil
}

// PGPDecrypt decrypts an ASCII-armored PGP message using the given
// ASCII-armored private key.
func PGPDecrypt(armoredCiphertext, armoredPrivateKey string) (string, error) {
	entityList, err := openpgp.ReadArmoredKeyRing(bytes.NewReader([]byte(armoredPrivateKey)))
	if err != nil {
		return "", fmt.Errorf("failed to read private key: %w", err)
	}
	if len(entityList) == 0 {
		return "", fmt.Errorf("no private key found in input")
	}

	block, err := armor.Decode(bytes.NewReader([]byte(armoredCiphertext)))
	if err != nil {
		return "", fmt.Errorf("failed to decode armored message: %w", err)
	}

	messageDetails, err := openpgp.ReadMessage(block.Body, entityList, nil, nil)
	if err != nil {
		return "", fmt.Errorf("failed to read message: %w", err)
	}

	plaintext, err := io.ReadAll(messageDetails.UnverifiedBody)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt message: %w", err)
	}

	return string(plaintext), nil
}

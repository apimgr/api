package crypto

import (
	"bytes"
	stdcrypto "crypto"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// pgpConfig pins the self-signature and session-key hash to SHA-256 so
// generated keys always advertise a modern, universally-compiled-in hash
// algorithm.
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

// GenerateSecurityPGPKeys creates the project disclosure key with an Ed25519
// signing primary key, Curve25519 encryption subkey, and two-year expiry.
func GenerateSecurityPGPKeys(name, email string, created time.Time) (string, string, string, error) {
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if name == "" || email == "" {
		return "", "", "", fmt.Errorf("name and email are required")
	}
	if created.IsZero() {
		created = time.Now().UTC()
	}
	created = created.UTC().Truncate(time.Second)
	config := &packet.Config{
		DefaultHash:     stdcrypto.SHA256,
		Algorithm:       packet.PubKeyAlgoEd25519,
		Curve:           packet.Curve25519,
		KeyLifetimeSecs: uint32((2 * 365 * 24 * time.Hour) / time.Second),
		Time:            func() time.Time { return created },
	}
	entity, err := openpgp.NewEntity(name, "", email, config)
	if err != nil {
		return "", "", "", fmt.Errorf("generate security PGP key: %w", err)
	}
	publicKey, err := serializePublicEntity(entity)
	if err != nil {
		return "", "", "", err
	}
	privateKey, err := serializePrivateEntity(entity)
	if err != nil {
		return "", "", "", err
	}
	return publicKey, privateKey, strings.ToUpper(fmt.Sprintf("%x", entity.PrimaryKey.Fingerprint)), nil
}

// PGPKeyFingerprint reads the v4 fingerprint from an armored key.
func PGPKeyFingerprint(armored string) (string, error) {
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil {
		return "", fmt.Errorf("read PGP key: %w", err)
	}
	if len(entities) == 0 || len(entities[0].PrimaryKey.Fingerprint) != 20 {
		return "", fmt.Errorf("expected an OpenPGP v4 key")
	}
	return strings.ToUpper(fmt.Sprintf("%x", entities[0].PrimaryKey.Fingerprint)), nil
}

// PGPIdentityMatches reports whether the armored key has the expected
// primary user identity components.
func PGPIdentityMatches(armored, name, email string) (bool, error) {
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil {
		return false, fmt.Errorf("read PGP key: %w", err)
	}
	if len(entities) == 0 {
		return false, fmt.Errorf("no PGP key found")
	}
	for _, identity := range entities[0].Identities {
		if identity.UserId != nil && identity.UserId.Name == name && identity.UserId.Email == email {
			return true, nil
		}
	}
	return false, nil
}

// CrossSignPGPPublicKey embeds an old-key certification on a new primary key.
func CrossSignPGPPublicKey(newPublic, oldPrivate string) (string, error) {
	newEntities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(newPublic))
	if err != nil {
		return "", fmt.Errorf("read new public key: %w", err)
	}
	if len(newEntities) == 0 {
		return "", fmt.Errorf("new public key contains no OpenPGP entity")
	}
	oldEntities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(oldPrivate))
	if err != nil {
		return "", fmt.Errorf("read old private key: %w", err)
	}
	if len(oldEntities) == 0 || oldEntities[0].PrivateKey == nil {
		return "", fmt.Errorf("old private key contains no signing entity")
	}
	signature := &packet.Signature{
		Version:      4,
		SigType:      packet.SigTypeDirectSignature,
		PubKeyAlgo:   oldEntities[0].PrimaryKey.PubKeyAlgo,
		Hash:         stdcrypto.SHA256,
		CreationTime: time.Now().UTC(),
	}
	if err := signature.CrossSignKey(newEntities[0].PrimaryKey, oldEntities[0].PrimaryKey, oldEntities[0].PrivateKey, pgpConfig()); err != nil {
		return "", fmt.Errorf("cross-sign new public key: %w", err)
	}
	newEntities[0].Signatures = append(newEntities[0].Signatures, signature)
	return serializePublicEntity(newEntities[0])
}

func serializePublicEntity(entity *openpgp.Entity) (string, error) {
	var buf bytes.Buffer
	writer, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", fmt.Errorf("armor public key: %w", err)
	}
	if err := entity.Serialize(writer); err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("serialize public key: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close public key armor: %w", err)
	}
	return buf.String(), nil
}

func serializePrivateEntity(entity *openpgp.Entity) (string, error) {
	var buf bytes.Buffer
	writer, err := armor.Encode(&buf, openpgp.PrivateKeyType, nil)
	if err != nil {
		return "", fmt.Errorf("armor private key: %w", err)
	}
	if err := entity.SerializePrivate(writer, nil); err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("serialize private key: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close private key armor: %w", err)
	}
	return buf.String(), nil
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

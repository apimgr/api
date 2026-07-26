package generate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// SSHKeyPair holds a generated Ed25519 SSH key pair in OpenSSH text formats.
type SSHKeyPair struct {
	PrivateKey string
	PublicKey  string
}

// SSHKey generates a new Ed25519 SSH key pair and returns the private key in
// OpenSSH PEM format and the public key in OpenSSH authorized_keys format.
// This is stateless generate-on-demand: nothing is persisted, so both keys
// are returned in full.
func (s *Service) SSHKey() (*SSHKeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ed25519 key: %w", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("failed to convert public key: %w", err)
	}
	authorizedKey := ssh.MarshalAuthorizedKey(sshPub)

	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	privatePEM := pem.EncodeToMemory(pemBlock)

	return &SSHKeyPair{
		PrivateKey: string(privatePEM),
		PublicKey:  string(authorizedKey),
	}, nil
}

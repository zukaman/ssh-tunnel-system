package tunnel

import (
	"crypto/ed25519"
	"crypto/rand"

	"golang.org/x/crypto/ssh"
)

// generateEd25519Key generates a new Ed25519 SSH key pair
func generateEd25519Key() (ssh.Signer, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, err
	}

	_ = publicKey // Public key can be saved for reference if needed

	return signer, nil
}

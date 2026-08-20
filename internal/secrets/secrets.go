// Package secrets encrypts the credentials Mimir has to store.
//
// Only one thing goes through here today — HoYoLAB session cookies — but that
// one thing is a full account credential. AES-256-GCM with a random nonce per
// write, keyed from the machine secret in the data directory. A database
// stolen without that file is inert.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotStored is returned when an account has no secret of the requested kind.
var ErrNotStored = errors.New("secrets: not stored")

// Vault seals and opens secrets with one key.
type Vault struct {
	aead cipher.AEAD
}

// NewVault returns a vault for a 32-byte key.
func NewVault(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}

// Seal encrypts plaintext, returning the nonce and the ciphertext separately
// so the schema can store them in their own columns.
func (v *Vault) Seal(plaintext []byte) (nonce, ciphertext []byte, err error) {
	nonce = make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("secrets: nonce: %w", err)
	}
	return nonce, v.aead.Seal(nil, nonce, plaintext, nil), nil
}

// Open decrypts.
func (v *Vault) Open(nonce, ciphertext []byte) ([]byte, error) {
	out, err := v.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Almost always a rotated or lost machine secret rather than
		// tampering, so the message points at the real cause.
		return nil, fmt.Errorf("secrets: decrypt failed — has the machine secret changed? %w", err)
	}
	return out, nil
}

// Store writes an account secret.
func (v *Vault) Store(conn *sql.DB, accountID int64, kind string, plaintext []byte) error {
	nonce, ct, err := v.Seal(plaintext)
	if err != nil {
		return err
	}
	_, err = conn.Exec(`
		INSERT INTO account_secrets (account_id, kind, nonce, ciphertext)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(account_id, kind) DO UPDATE SET
			nonce = excluded.nonce,
			ciphertext = excluded.ciphertext,
			updated_at = datetime('now')`,
		accountID, kind, nonce, ct)
	return err
}

// Load reads and decrypts an account secret.
func (v *Vault) Load(conn *sql.DB, accountID int64, kind string) ([]byte, error) {
	var nonce, ct []byte
	err := conn.QueryRow(
		`SELECT nonce, ciphertext FROM account_secrets WHERE account_id = ? AND kind = ?`,
		accountID, kind).Scan(&nonce, &ct)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotStored
	}
	if err != nil {
		return nil, err
	}
	return v.Open(nonce, ct)
}

// Delete removes an account secret.
func Delete(conn *sql.DB, accountID int64, kind string) error {
	_, err := conn.Exec(`DELETE FROM account_secrets WHERE account_id = ? AND kind = ?`, accountID, kind)
	return err
}

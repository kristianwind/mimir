package secrets

import (
	"bytes"
	"testing"

	"github.com/kristianwind/mimir/internal/db"
)

func key(b byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = b
	}
	return k
}

func TestSealOpenRoundTrip(t *testing.T) {
	v, err := NewVault(key(1))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("ltoken_v2=example-token; ltuid_v2=000000000")

	nonce, ct, err := v.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("ltoken")) {
		t.Error("ciphertext contains the plaintext")
	}

	got, err := v.Open(nonce, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("round trip gave %q", got)
	}
}

func TestSealUsesAFreshNonce(t *testing.T) {
	v, _ := NewVault(key(2))
	n1, c1, _ := v.Seal([]byte("same"))
	n2, c2, _ := v.Seal([]byte("same"))
	if bytes.Equal(n1, n2) {
		t.Error("nonce reused; GCM leaks with a repeated nonce")
	}
	if bytes.Equal(c1, c2) {
		t.Error("identical ciphertexts for identical plaintexts")
	}
}

func TestOpenRejectsWrongKeyAndTampering(t *testing.T) {
	v, _ := NewVault(key(3))
	nonce, ct, _ := v.Seal([]byte("secret"))

	other, _ := NewVault(key(4))
	if _, err := other.Open(nonce, ct); err == nil {
		t.Error("a different key decrypted the ciphertext")
	}

	ct[0] ^= 0xff
	if _, err := v.Open(nonce, ct); err == nil {
		t.Error("tampered ciphertext was accepted")
	}
}

func TestNewVaultRejectsShortKey(t *testing.T) {
	if _, err := NewVault([]byte("too short")); err == nil {
		t.Error("expected an error for a 9-byte key")
	}
}

func TestStoreLoadDelete(t *testing.T) {
	conn, err := db.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'x')`)
	conn.Exec(`INSERT INTO accounts (id, user_id, uid) VALUES (1, 1, '700000001')`)

	v, _ := NewVault(key(5))
	if err := v.Store(conn, 1, "hoyolab", []byte("cookies")); err != nil {
		t.Fatal(err)
	}

	got, err := v.Load(conn, 1, "hoyolab")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "cookies" {
		t.Errorf("loaded %q", got)
	}

	// Re-storing must overwrite rather than fail on the primary key.
	if err := v.Store(conn, 1, "hoyolab", []byte("newer")); err != nil {
		t.Fatal(err)
	}
	got, _ = v.Load(conn, 1, "hoyolab")
	if string(got) != "newer" {
		t.Errorf("after overwrite, loaded %q", got)
	}

	if err := Delete(conn, 1, "hoyolab"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Load(conn, 1, "hoyolab"); err != ErrNotStored {
		t.Errorf("after delete, got %v", err)
	}
}

package token

import "testing"

func TestNewReturnsPlainAndHash(t *testing.T) {
	plain, hash, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if plain == "" || hash == "" {
		t.Fatal("plain and hash must be populated")
	}
	if plain == hash {
		t.Fatal("hash must not equal plain token")
	}
	if Hash(plain) != hash {
		t.Fatal("Hash(plain) must match returned hash")
	}
}

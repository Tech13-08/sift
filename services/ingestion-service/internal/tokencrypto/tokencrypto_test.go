package tokencrypto

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key, err := ParseKey(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestParseKey(t *testing.T) {
	if _, err := ParseKey(""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParseKey("abcd"); err == nil {
		t.Fatal("expected error")
	}
	key, err := ParseKey(strings.Repeat("0a", 32))
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("len=%d", len(key))
	}
}

func TestRoundtrip(t *testing.T) {
	key := testKey(t)
	plain := "ya29.a0TEST-refresh"
	enc, err := Encrypt(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	if !IsCiphertext(enc) {
		t.Fatal("expected ciphertext prefix")
	}
	if strings.Contains(enc, plain) {
		t.Fatal("plaintext leaked into ciphertext")
	}
	got, err := Decrypt(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("got %q", got)
	}
	again, err := Encrypt(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if again != enc {
		t.Fatal("re-encrypt of ciphertext should be a no-op")
	}
}

func TestPlaintextPassthrough(t *testing.T) {
	key := testKey(t)
	got, err := Decrypt(key, "1//legacy-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1//legacy-refresh" {
		t.Fatalf("got %q", got)
	}
	empty, err := Encrypt(key, "")
	if err != nil || empty != "" {
		t.Fatalf("empty encrypt: %q %v", empty, err)
	}
}

func TestWrongKeyFails(t *testing.T) {
	key := testKey(t)
	other, err := ParseKey(strings.Repeat("cd", 32))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := Encrypt(key, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(other, enc); err == nil {
		t.Fatal("expected decrypt failure")
	}
}

func TestGoldenNonce(t *testing.T) {
	key, err := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{0x11}, 12)
	got, err := encryptWithNonce(key, nonce, "hello")
	if err != nil {
		t.Fatal(err)
	}
	const nodeCipher = "enc:v1:ERERERERERERERER6AX409doC5AV8KR4KqGBoUVhPTJW"
	if got != nodeCipher {
		t.Fatalf("go/node mismatch\ngo=%s\nnode=%s", got, nodeCipher)
	}
	plain, err := Decrypt(key, got)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "hello" {
		t.Fatalf("decrypt=%q", plain)
	}
	if !strings.HasPrefix(got, Prefix) {
		t.Fatalf("prefix: %s", got)
	}
}

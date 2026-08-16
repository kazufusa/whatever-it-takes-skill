package main

import (
	"encoding/hex"
	"os"
	"testing"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := generateKeyPair()
	if err != nil {
		t.Fatalf("generateKeyPair: %v", err)
	}

	data := []byte(`{"timestamp":"20260815T000000Z","verdict":"ok","reason":"test"}`)
	sig := signData(priv, data)

	if !verifyData(pub, data, sig) {
		t.Fatal("expected signature to verify, but it did not")
	}
}

func TestVerifyFailsOnTamperedData(t *testing.T) {
	pub, priv, err := generateKeyPair()
	if err != nil {
		t.Fatalf("generateKeyPair: %v", err)
	}

	data := []byte(`{"verdict":"ok"}`)
	sig := signData(priv, data)

	tampered := []byte(`{"verdict":"TAMPERED"}`)
	if verifyData(pub, tampered, sig) {
		t.Fatal("expected verification to fail on tampered data, but it succeeded")
	}
}

func TestVerifyFailsWithWrongKey(t *testing.T) {
	_, priv, err := generateKeyPair()
	if err != nil {
		t.Fatalf("generateKeyPair: %v", err)
	}
	otherPub, _, err := generateKeyPair()
	if err != nil {
		t.Fatalf("generateKeyPair: %v", err)
	}

	data := []byte(`{"verdict":"ok"}`)
	sig := signData(priv, data)

	if verifyData(otherPub, data, sig) {
		t.Fatal("expected verification to fail with an unrelated public key, but it succeeded")
	}
}

func TestPublicKeyPEMRoundTrip(t *testing.T) {
	pub, priv, err := generateKeyPair()
	if err != nil {
		t.Fatalf("generateKeyPair: %v", err)
	}

	pemBytes, err := publicKeyToPEM(pub)
	if err != nil {
		t.Fatalf("publicKeyToPEM: %v", err)
	}

	dir := t.TempDir()
	path := dir + "/public_key.pem"
	if err := os.WriteFile(path, pemBytes, 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	roundTripped, err := publicKeyFromPEMFile(path)
	if err != nil {
		t.Fatalf("publicKeyFromPEMFile: %v", err)
	}

	data := []byte("round trip check")
	sig := signData(priv, data)
	if !verifyData(roundTripped, data, sig) {
		t.Fatal("signature should verify against the PEM round-tripped public key")
	}
}

func TestPrivateKeyFromHexSeedRoundTrip(t *testing.T) {
	pub, priv, err := generateKeyPair()
	if err != nil {
		t.Fatalf("generateKeyPair: %v", err)
	}

	seedHex := hex.EncodeToString(priv.Seed())
	restored, err := privateKeyFromHexSeed(seedHex)
	if err != nil {
		t.Fatalf("privateKeyFromHexSeed: %v", err)
	}

	data := []byte("seed round trip")
	sig := signData(restored, data)
	if !verifyData(pub, data, sig) {
		t.Fatal("signature from seed-restored key should verify against the original public key")
	}
}

func TestPrivateKeyFromHexSeedRejectsBadInput(t *testing.T) {
	if _, err := privateKeyFromHexSeed("not-hex"); err == nil {
		t.Fatal("expected an error for non-hex input")
	}
	if _, err := privateKeyFromHexSeed("aabb"); err == nil {
		t.Fatal("expected an error for a seed of the wrong length")
	}
}

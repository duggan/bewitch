package api

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	cert, certPEM, keyPEM, err := GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	if len(cert.Certificate) != 1 {
		t.Fatalf("expected 1 certificate in chain, got %d", len(cert.Certificate))
	}
	if len(certPEM) == 0 {
		t.Fatal("certPEM is empty")
	}
	if len(keyPEM) == 0 {
		t.Fatal("keyPEM is empty")
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}

	if leaf.Subject.CommonName != "bewitch-daemon" {
		t.Errorf("CN = %q, want %q", leaf.Subject.CommonName, "bewitch-daemon")
	}
	if !leaf.IsCA {
		t.Error("expected IsCA = true")
	}

	foundLocalhost := false
	for _, name := range leaf.DNSNames {
		if name == "localhost" {
			foundLocalhost = true
		}
	}
	if !foundLocalhost {
		t.Errorf("DNSNames = %v, want to contain 'localhost'", leaf.DNSNames)
	}

	foundLoopback := false
	for _, ip := range leaf.IPAddresses {
		if ip.String() == "127.0.0.1" {
			foundLoopback = true
		}
	}
	if !foundLoopback {
		t.Errorf("IPAddresses = %v, want to contain 127.0.0.1", leaf.IPAddresses)
	}
}

func TestCertFingerprint(t *testing.T) {
	cert, _, _, err := GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() error = %v", err)
	}

	fp := CertFingerprint(cert.Certificate[0])

	// Verify format
	if fp[:7] != "sha256:" {
		t.Errorf("fingerprint should start with 'sha256:', got %q", fp)
	}

	// Verify it matches manual computation
	sum := sha256.Sum256(cert.Certificate[0])
	want := "sha256:" + hex.EncodeToString(sum[:])
	if fp != want {
		t.Errorf("fingerprint = %q, want %q", fp, want)
	}
}

func TestLoadOrGenerateCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	// First call: generates and persists
	cert1, err := LoadOrGenerateCert(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateCert() error = %v", err)
	}
	if len(cert1.Certificate) == 0 {
		t.Fatal("expected non-empty certificate")
	}

	// Verify files were created
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("cert file not created: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("key file not created: %v", err)
	}

	// Key file should have restricted permissions
	info, _ := os.Stat(keyPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("key file permissions = %o, want 0600", info.Mode().Perm())
	}

	// Second call: loads from disk, same cert
	cert2, err := LoadOrGenerateCert(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateCert() second call error = %v", err)
	}

	fp1 := CertFingerprint(cert1.Certificate[0])
	fp2 := CertFingerprint(cert2.Certificate[0])
	if fp1 != fp2 {
		t.Errorf("fingerprints differ after reload: %s vs %s", fp1, fp2)
	}
}

func TestLoadOrGenerateCert_OnlyOneExists(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	// Create only cert file
	os.WriteFile(certPath, []byte("dummy"), 0644)

	_, err := LoadOrGenerateCert(certPath, keyPath)
	if err == nil {
		t.Fatal("expected error when only cert exists")
	}
}

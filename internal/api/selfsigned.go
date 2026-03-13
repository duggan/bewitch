package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

// GenerateSelfSignedCert creates a new ECDSA P-256 self-signed certificate
// suitable for the bewitch daemon TLS listener. Returns the TLS certificate
// and the PEM-encoded certificate and key bytes.
func GenerateSelfSignedCert() (tls.Certificate, []byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("generating key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("generating serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: "bewitch-daemon"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:     []string{"localhost"},
		IsCA:         true,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("creating certificate: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("marshaling key: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("parsing key pair: %w", err)
	}

	return tlsCert, certPEM, keyPEM, nil
}

// LoadOrGenerateCert loads an existing TLS certificate from disk, or generates
// a new self-signed one and persists it. The certificate is reused across daemon
// restarts so the fingerprint remains stable.
func LoadOrGenerateCert(certPath, keyPath string) (tls.Certificate, error) {
	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)

	if certExists && keyExists {
		return tls.LoadX509KeyPair(certPath, keyPath)
	}
	if certExists != keyExists {
		return tls.Certificate{}, fmt.Errorf("only one of cert/key exists: cert=%s key=%s", certPath, keyPath)
	}

	// Generate new cert and persist
	cert, certPEM, keyPEM, err := GenerateSelfSignedCert()
	if err != nil {
		return tls.Certificate{}, err
	}

	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return tls.Certificate{}, fmt.Errorf("writing cert to %s: %w", certPath, err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return tls.Certificate{}, fmt.Errorf("writing key to %s: %w", keyPath, err)
	}

	return cert, nil
}

// CertFingerprint returns the SHA-256 fingerprint of a DER-encoded certificate
// as a hex string (e.g. "sha256:a1b2c3...").
func CertFingerprint(certDER []byte) string {
	sum := sha256.Sum256(certDER)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

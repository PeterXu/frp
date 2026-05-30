package transport

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func generateTestCA(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}

	return key, cert
}

func generateTestCert(t *testing.T, caKey *rsa.PrivateKey, caCert *x509.Certificate, serial int64, cn string) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}

	return key, cert
}

func generateTestCRL(t *testing.T, caKey *rsa.PrivateKey, caCert *x509.Certificate, revokedCerts []*x509.Certificate) []byte {
	t.Helper()

	entries := make([]x509.RevocationListEntry, 0, len(revokedCerts))
	for _, cert := range revokedCerts {
		entries = append(entries, x509.RevocationListEntry{
			SerialNumber:   cert.SerialNumber,
			RevocationTime: time.Now(),
		})
	}

	template := &x509.RevocationList{
		RevokedCertificateEntries: entries,
		Number:                    big.NewInt(1),
		ThisUpdate:                time.Now().Add(-1 * time.Hour),
		NextUpdate:                time.Now().Add(24 * time.Hour),
	}

	crlDER, err := x509.CreateRevocationList(rand.Reader, template, caCert, caKey)
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "X509 CRL",
		Bytes: crlDER,
	})
}

func writeCertAndKey(t *testing.T, dir, name string, cert *x509.Certificate, key *rsa.PrivateKey) (certPath, keyPath string) {
	t.Helper()
	certPath = filepath.Join(dir, name+".crt")
	keyPath = filepath.Join(dir, name+".key")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return
}

func writeCACert(t *testing.T, dir string, cert *x509.Certificate) string {
	t.Helper()
	path := filepath.Join(dir, "ca.crt")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(path, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCRL(t *testing.T, dir string, crlPEM []byte) string {
	t.Helper()
	path := filepath.Join(dir, "ca.crl")
	if err := os.WriteFile(path, crlPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCRL(t *testing.T) {
	caKey, caCert := generateTestCA(t)
	_, clientCert := generateTestCert(t, caKey, caCert, 100, "revoked-client")

	tmpDir := t.TempDir()
	crlPEM := generateTestCRL(t, caKey, caCert, []*x509.Certificate{clientCert})
	crlPath := writeCRL(t, tmpDir, crlPEM)

	revokedSerials, err := loadCRL(crlPath, caCert)
	if err != nil {
		t.Fatalf("loadCRL failed: %v", err)
	}
	if len(revokedSerials) != 1 {
		t.Fatalf("expected 1 revoked serial, got %d", len(revokedSerials))
	}
	if !revokedSerials[big.NewInt(100).String()] {
		t.Fatal("expected serial 100 to be in revoked list")
	}
}

func TestLoadCRLEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	crlPath := filepath.Join(tmpDir, "empty.crl")
	if err := os.WriteFile(crlPath, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := loadCRL(crlPath, nil)
	if err == nil {
		t.Fatal("expected error for file with no CRL PEM blocks, got nil")
	}
}

func TestLoadCRLNoRevocations(t *testing.T) {
	caKey, caCert := generateTestCA(t)

	tmpDir := t.TempDir()
	crlPEM := generateTestCRL(t, caKey, caCert, nil)
	crlPath := writeCRL(t, tmpDir, crlPEM)

	revokedSerials, err := loadCRL(crlPath, caCert)
	if err != nil {
		t.Fatalf("expected CRL with zero entries to be valid, got: %v", err)
	}
	if len(revokedSerials) != 0 {
		t.Fatalf("expected 0 revoked serials, got %d", len(revokedSerials))
	}
}

func TestLoadCRLSignatureMismatch(t *testing.T) {
	trustedCAKey, trustedCACert := generateTestCA(t)
	rogueCAKey, rogueCACert := generateTestCA(t)
	_, clientCert := generateTestCert(t, trustedCAKey, trustedCACert, 100, "client")

	tmpDir := t.TempDir()
	// CRL signed by a different CA than the trusted one
	crlPEM := generateTestCRL(t, rogueCAKey, rogueCACert, []*x509.Certificate{clientCert})
	crlPath := writeCRL(t, tmpDir, crlPEM)

	_, err := loadCRL(crlPath, trustedCACert)
	if err == nil {
		t.Fatal("expected error for CRL signed by untrusted CA, got nil")
	}
}

func TestNewServerTLSConfigWithCRL(t *testing.T) {
	caKey, caCert := generateTestCA(t)
	serverKey, serverCert := generateTestCert(t, caKey, caCert, 1, "server")
	_, badClient := generateTestCert(t, caKey, caCert, 100, "revoked-client")

	tmpDir := t.TempDir()
	serverCertPath, serverKeyPath := writeCertAndKey(t, tmpDir, "server", serverCert, serverKey)
	caPath := writeCACert(t, tmpDir, caCert)
	crlPEM := generateTestCRL(t, caKey, caCert, []*x509.Certificate{badClient})
	crlPath := writeCRL(t, tmpDir, crlPEM)

	cfg, verifier, err := NewServerTLSConfigWithCRL(serverCertPath, serverKeyPath, caPath, crlPath)
	if err != nil {
		t.Fatalf("NewServerTLSConfigWithCRL failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if verifier == nil {
		t.Fatal("expected non-nil TLSVerifier")
	}

	_, _, err = NewServerTLSConfigWithCRL(serverCertPath, serverKeyPath, "", crlPath)
	if err == nil {
		t.Fatal("expected error when crlFile set without trustedCaFile")
	}
}

func TestNewServerTLSConfigWithoutCRL(t *testing.T) {
	caKey, caCert := generateTestCA(t)
	serverKey, serverCert := generateTestCert(t, caKey, caCert, 1, "server")

	tmpDir := t.TempDir()
	serverCertPath, serverKeyPath := writeCertAndKey(t, tmpDir, "server", serverCert, serverKey)
	caPath := writeCACert(t, tmpDir, caCert)

	cfg, verifier, err := NewServerTLSConfigWithCRL(serverCertPath, serverKeyPath, caPath, "")
	if err != nil {
		t.Fatalf("NewServerTLSConfigWithCRL failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if verifier != nil {
		t.Fatal("expected nil TLSVerifier when no CRL path provided")
	}
}

func TestTLSVerifierRejectsRevokedCert(t *testing.T) {
	caKey, caCert := generateTestCA(t)
	_, client1 := generateTestCert(t, caKey, caCert, 100, "client1")
	_, client2 := generateTestCert(t, caKey, caCert, 200, "client2")

	tmpDir := t.TempDir()
	crlPEM := generateTestCRL(t, caKey, caCert, []*x509.Certificate{client1})
	crlPath := writeCRL(t, tmpDir, crlPEM)

	verifier, err := NewTLSVerifier(crlPath, caCert)
	if err != nil {
		t.Fatalf("NewTLSVerifier failed: %v", err)
	}

	vc := verifier.VerifyPeerCertificate()

	err = vc(nil, [][]*x509.Certificate{{client1}})
	if err == nil {
		t.Fatal("expected revoked cert to be rejected")
	}

	err = vc(nil, [][]*x509.Certificate{{client2}})
	if err != nil {
		t.Fatalf("expected non-revoked cert to be accepted, got: %v", err)
	}
}

func TestTLSVerifierReload(t *testing.T) {
	caKey, caCert := generateTestCA(t)
	_, client1 := generateTestCert(t, caKey, caCert, 100, "client1")
	_, client2 := generateTestCert(t, caKey, caCert, 200, "client2")

	tmpDir := t.TempDir()

	crlPEM := generateTestCRL(t, caKey, caCert, []*x509.Certificate{client1})
	crlPath := writeCRL(t, tmpDir, crlPEM)

	verifier, err := NewTLSVerifier(crlPath, caCert)
	if err != nil {
		t.Fatalf("NewTLSVerifier failed: %v", err)
	}

	vc := verifier.VerifyPeerCertificate()

	err = vc(nil, [][]*x509.Certificate{{client1}})
	if err == nil {
		t.Fatal("expected client1 to be revoked")
	}
	err = vc(nil, [][]*x509.Certificate{{client2}})
	if err != nil {
		t.Fatalf("expected client2 to be allowed, got: %v", err)
	}

	crlPEM2 := generateTestCRL(t, caKey, caCert, []*x509.Certificate{client1, client2})
	writeCRL(t, tmpDir, crlPEM2)

	if err := verifier.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	err = vc(nil, [][]*x509.Certificate{{client2}})
	if err == nil {
		t.Fatal("expected reloaded CRL to reject client2")
	}
}

func TestTLSVerifierReloadInvalidFile(t *testing.T) {
	caKey, caCert := generateTestCA(t)
	_, client1 := generateTestCert(t, caKey, caCert, 100, "client1")

	tmpDir := t.TempDir()
	crlPEM := generateTestCRL(t, caKey, caCert, []*x509.Certificate{client1})
	crlPath := writeCRL(t, tmpDir, crlPEM)

	verifier, err := NewTLSVerifier(crlPath, caCert)
	if err != nil {
		t.Fatalf("NewTLSVerifier failed: %v", err)
	}

	if err := os.WriteFile(crlPath, []byte("not a valid crl"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := verifier.Reload(); err == nil {
		t.Fatal("expected reload to fail with corrupted file")
	}

	vc := verifier.VerifyPeerCertificate()
	err = vc(nil, [][]*x509.Certificate{{client1}})
	if err == nil {
		t.Fatal("expected original CRL to still reject client1 after failed reload")
	}
}

func TestTLSVerifierRejectsLeafOnly(t *testing.T) {
	caKey, caCert := generateTestCA(t)
	_, clientCert := generateTestCert(t, caKey, caCert, 100, "client")

	tmpDir := t.TempDir()
	// Revoke the CA's serial number (1) — not the client's
	crlPEM := generateTestCRL(t, caKey, caCert, []*x509.Certificate{caCert})
	crlPath := writeCRL(t, tmpDir, crlPEM)

	verifier, err := NewTLSVerifier(crlPath, caCert)
	if err != nil {
		t.Fatalf("NewTLSVerifier failed: %v", err)
	}

	vc := verifier.VerifyPeerCertificate()
	// Chain includes [clientCert, caCert]. Only the leaf (clientCert, serial 100) is checked.
	// CA cert (serial 1) is revoked but is not the leaf, so it should pass.
	err = vc(nil, [][]*x509.Certificate{{clientCert, caCert}})
	if err != nil {
		t.Fatalf("expected non-revoked leaf to be accepted even when CA serial is in CRL, got: %v", err)
	}
}

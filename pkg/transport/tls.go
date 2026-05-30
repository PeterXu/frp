// Copyright 2023 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package transport

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"
)

func newCustomTLSKeyPair(certfile, keyfile string) (*tls.Certificate, error) {
	tlsCert, err := tls.LoadX509KeyPair(certfile, keyfile)
	if err != nil {
		return nil, err
	}
	return &tlsCert, nil
}

func newRandomTLSKeyPair() (*tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	// Generate a random positive serial number with 128 bits of entropy.
	// RFC 5280 requires serial numbers to be positive integers (not zero).
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}
	// Ensure serial number is positive (not zero)
	if serialNumber.Sign() == 0 {
		serialNumber = big.NewInt(1)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour * 10),
	}

	certDER, err := x509.CreateCertificate(
		rand.Reader,
		&template,
		&template,
		&key.PublicKey,
		key)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tlsCert, nil
}

// Only support one ca file to add
func newCertPool(caPath string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()

	caCrt, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}

	if !pool.AppendCertsFromPEM(caCrt) {
		return nil, fmt.Errorf("failed to parse CA certificate from file %q: no valid PEM certificates found", caPath)
	}

	return pool, nil
}

func loadCACertificate(caPath string) (*x509.Certificate, error) {
	data, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("failed to parse CA certificate from file %q: no PEM data found", caPath)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate from file %q: %w", caPath, err)
	}

	return cert, nil
}

func NewServerTLSConfig(certPath, keyPath, caPath string) (*tls.Config, error) {
	base := &tls.Config{}

	if certPath == "" || keyPath == "" {
		// server will generate tls conf by itself
		cert, err := newRandomTLSKeyPair()
		if err != nil {
			return nil, err
		}
		base.Certificates = []tls.Certificate{*cert}
	} else {
		cert, err := newCustomTLSKeyPair(certPath, keyPath)
		if err != nil {
			return nil, err
		}

		base.Certificates = []tls.Certificate{*cert}
	}

	if caPath != "" {
		pool, err := newCertPool(caPath)
		if err != nil {
			return nil, err
		}

		base.ClientAuth = tls.RequireAndVerifyClientCert
		base.ClientCAs = pool
	}

	return base, nil
}

// NewServerTLSConfigWithCRL extends NewServerTLSConfig with CRL-based certificate revocation checking.
// crlPath is optional; when empty, behaves identically to NewServerTLSConfig.
func NewServerTLSConfigWithCRL(certPath, keyPath, caPath, crlPath string) (*tls.Config, *TLSVerifier, error) {
	cfg, err := NewServerTLSConfig(certPath, keyPath, caPath)
	if err != nil {
		return nil, nil, err
	}

	if crlPath == "" {
		return cfg, nil, nil
	}

	if caPath == "" {
		return nil, nil, fmt.Errorf("crlFile requires trustedCaFile to be set")
	}

	caCert, err := loadCACertificate(caPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load CA certificate for CRL verification: %w", err)
	}

	verifier, err := NewTLSVerifier(crlPath, caCert)
	if err != nil {
		return nil, nil, err
	}
	cfg.VerifyPeerCertificate = verifier.VerifyPeerCertificate()
	return cfg, verifier, nil
}

// TLSClientOption is a functional option for NewClientTLSConfig.
type TLSClientOption func(*tls.Config)

// WithSkipServerNameVerify skips TLS server name verification while still
// verifying the server's certificate chain. Useful when the server
// certificate's CN/SAN doesn't match the connection hostname.
func WithSkipServerNameVerify() TLSClientOption {
	return func(cfg *tls.Config) {
		if cfg.RootCAs == nil {
			return
		}
		cfg.InsecureSkipVerify = true
		cfg.VerifyConnection = func(state tls.ConnectionState) error {
			opts := x509.VerifyOptions{
				Roots:         cfg.RootCAs,
				Intermediates: x509.NewCertPool(),
			}
			for _, cert := range state.PeerCertificates[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err := state.PeerCertificates[0].Verify(opts)
			return err
		}
	}
}

// NewClientTLSConfig creates a client TLS configuration.
// The original 4-parameter signature is preserved for backwards compatibility.
// Use TLSClientOption values for additional configuration.
func NewClientTLSConfig(certPath, keyPath, caPath, serverName string, opts ...TLSClientOption) (*tls.Config, error) {
	base := &tls.Config{}

	if certPath != "" && keyPath != "" {
		cert, err := newCustomTLSKeyPair(certPath, keyPath)
		if err != nil {
			return nil, err
		}

		base.Certificates = []tls.Certificate{*cert}
	}

	base.ServerName = serverName

	if caPath != "" {
		pool, err := newCertPool(caPath)
		if err != nil {
			return nil, err
		}

		base.RootCAs = pool
	} else {
		// No CA provided, skip all verification (including server name)
		base.InsecureSkipVerify = true
	}

	for _, opt := range opts {
		opt(base)
	}

	return base, nil
}

func NewRandomPrivateKey() ([]byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return keyPEM, nil
}

// TLSVerifier holds CRL data and provides certificate verification with revocation checking.
// It supports hot-reload of the CRL file without restarting the server.
type TLSVerifier struct {
	revokedSerials map[string]bool
	mu             sync.RWMutex
	crlPath        string
	caCert         *x509.Certificate
}

// NewTLSVerifier creates a new TLSVerifier by loading the CRL from the given path.
// caCert is used to verify the CRL signature. If crlPath is empty, returns nil.
func NewTLSVerifier(crlPath string, caCert *x509.Certificate) (*TLSVerifier, error) {
	if crlPath == "" {
		return nil, nil
	}

	revokedSerials, err := loadCRL(crlPath, caCert)
	if err != nil {
		return nil, fmt.Errorf("failed to load CRL from %q: %w", crlPath, err)
	}

	return &TLSVerifier{
		revokedSerials: revokedSerials,
		crlPath:        crlPath,
		caCert:         caCert,
	}, nil
}

// VerifyPeerCertificate returns a callback for tls.Config.VerifyPeerCertificate.
// It checks each peer certificate against the loaded CRL.
func (v *TLSVerifier) VerifyPeerCertificate() func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		v.mu.RLock()
		defer v.mu.RUnlock()

		for _, chain := range verifiedChains {
			if len(chain) > 0 {
				cert := chain[0]
				if v.revokedSerials[cert.SerialNumber.String()] {
					return fmt.Errorf("tls: certificate revoked by CRL (serial %s, subject %s)",
						cert.SerialNumber, cert.Subject)
				}
			}
		}
		return nil
	}
}

// Reload re-reads the CRL file and updates the verifier's CRL data.
// If the new CRL file is invalid, the existing CRL data is preserved and an error is returned.
func (v *TLSVerifier) Reload() error {
	newRevokedSerials, err := loadCRL(v.crlPath, v.caCert)
	if err != nil {
		return fmt.Errorf("failed to reload CRL from %q: %w", v.crlPath, err)
	}

	v.mu.Lock()
	v.revokedSerials = newRevokedSerials
	v.mu.Unlock()

	return nil
}

// loadCRL reads and parses a PEM-encoded CRL file, returning a set of revoked serial numbers.
// It verifies the CRL signature against the provided CA certificate.
func loadCRL(crlPath string, caCert *x509.Certificate) (map[string]bool, error) {
	data, err := os.ReadFile(crlPath)
	if err != nil {
		return nil, err
	}

	revokedSerials := make(map[string]bool)
	found := false
	var block *pem.Block
	rest := data
	for {
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "X509 CRL" {
			found = true
			crl, err := x509.ParseRevocationList(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse CRL PEM block: %w", err)
			}
			if err := crl.CheckSignatureFrom(caCert); err != nil {
				return nil, fmt.Errorf("CRL signature verification failed: %w", err)
			}
			for _, entry := range crl.RevokedCertificateEntries {
				if entry.SerialNumber != nil {
					revokedSerials[entry.SerialNumber.String()] = true
				}
			}
		}
	}

	if !found {
		return nil, fmt.Errorf("no X509 CRL PEM blocks found in %q", crlPath)
	}

	return revokedSerials, nil
}

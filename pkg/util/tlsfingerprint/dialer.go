// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Package tlsfingerprint provides TLS fingerprint simulation for HTTPS connections.
// It uses the utls library to create TLS connections that mimic real browsers/clients.
package tlsfingerprint

import (
	"context"
	"fmt"
	"net"
	"sync"

	utls "github.com/refraction-networking/utls"
)

// Profile contains TLS fingerprint configuration.
// All slice fields use built-in defaults when empty.
type Profile struct {
	Name                string // Profile name for identification
	CipherSuites        []uint16
	Curves              []uint16
	PointFormats        []uint16
	EnableGREASE        bool
	SignatureAlgorithms []uint16
	ALPNProtocols       []string
	SupportedVersions   []uint16
	KeyShareGroups      []uint16
	PSKModes            []uint16
	Extensions          []uint16 // Extension type IDs in order
}

// Preset profiles
var (
	presetProfiles = map[string]*Profile{
		"chrome": {
			Name:         "chrome",
			EnableGREASE: true,
			// Uses utls.HelloChrome_Auto preset
		},
		"firefox": {
			Name:         "firefox",
			EnableGREASE: true,
			// Uses utls.HelloFirefox_Auto preset
		},
		"safari": {
			Name:         "safari",
			EnableGREASE: true,
			// Uses utls.HelloSafari_Auto preset
		},
		"node": {
			Name:         "node_v24",
			EnableGREASE: false,
			CipherSuites: defaultCipherSuites,
			Curves:       []uint16{uint16(utls.X25519), uint16(utls.CurveP256), uint16(utls.CurveP384)},
			PointFormats: defaultPointFormats,
			Extensions:   defaultExtensionOrder,
		},
	}

	profileMutex sync.RWMutex
)

// GetProfile returns a profile by name. Returns nil if not found.
func GetProfile(name string) *Profile {
	profileMutex.RLock()
	defer profileMutex.RUnlock()

	p, ok := presetProfiles[name]
	if ok {
		return p
	}
	return nil
}

// RegisterProfile adds a custom profile.
func RegisterProfile(name string, profile *Profile) {
	profileMutex.Lock()
	defer profileMutex.Unlock()
	presetProfiles[name] = profile
}

// Default TLS fingerprint values (Node.js 24.x)
// JA3 Hash: 44f88fca027f27bab4bb08d4af15f23e
// JA4: t13d1714h1_5b57614c22b0_7baf387fc6ff
var (
	defaultCipherSuites = []uint16{
		0x1301, 0x1302, 0x1303, // TLS 1.3
		0xc02b, 0xc02f, 0xc02c, 0xc030, // ECDHE + AES-GCM
		0xcca9, 0xcca8, // ECDHE + ChaCha20
		0xc009, 0xc013, 0xc00a, 0xc014, // ECDHE + AES-CBC
		0x009c, 0x009d, // RSA + AES-GCM
		0x002f, 0x0035, // RSA + AES-CBC
	}

	defaultPointFormats = []uint16{0} // uncompressed

	defaultSignatureAlgorithms = []uint16{
		0x0403, 0x0804, 0x0401, 0x0503, 0x0805, 0x0501, 0x0806, 0x0601, 0x0201,
	}

	// Node.js 24.x extension order
	defaultExtensionOrder = []uint16{
		0,     // server_name
		65037, // encrypted_client_hello
		23,    // extended_master_secret
		65281, // renegotiation_info
		10,    // supported_groups
		11,    // ec_point_formats
		35,    // session_ticket
		16,    // alpn
		5,     // status_request
		13,    // signature_algorithms
		18,    // signed_certificate_timestamp
		51,    // key_share
		45,    // psk_key_exchange_modes
		43,    // supported_versions
	}
)

// Dialer creates TLS connections with custom fingerprints.
type Dialer struct {
	profile    *Profile
	baseDialer func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewDialer creates a new TLS fingerprint dialer.
func NewDialer(profile *Profile, baseDialer func(ctx context.Context, network, addr string) (net.Conn, error)) *Dialer {
	if baseDialer == nil {
		baseDialer = (&net.Dialer{}).DialContext
	}
	return &Dialer{profile: profile, baseDialer: baseDialer}
}

// DialTLSContext establishes a TLS connection with the configured fingerprint.
func (d *Dialer) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := d.baseDialer(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return PerformTLSHandshake(ctx, conn, d.profile, addr)
}

// PerformTLSHandshake performs uTLS handshake on an established connection.
func PerformTLSHandshake(ctx context.Context, conn net.Conn, profile *Profile, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	// Use preset ClientHelloID for known profiles
	var tlsConn *utls.UConn
	if profile != nil && len(profile.CipherSuites) == 0 && len(profile.Extensions) == 0 {
		// Use built-in preset
		switch profile.Name {
		case "chrome":
			tlsConn = utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloChrome_Auto)
		case "firefox":
			tlsConn = utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloFirefox_Auto)
		case "safari":
			tlsConn = utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloSafari_Auto)
		default:
			tlsConn = utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloChrome_Auto)
		}
	} else {
		// Use custom profile
		spec := buildClientHelloSpecFromProfile(profile)
		tlsConn = utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloCustom)
		if err := tlsConn.ApplyPreset(spec); err != nil {
			conn.Close()
			return nil, fmt.Errorf("apply TLS preset: %w", err)
		}
	}

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("TLS handshake failed: %w", err)
	}

	return tlsConn, nil
}

// buildClientHelloSpecFromProfile constructs ClientHelloSpec from a Profile.
func buildClientHelloSpecFromProfile(profile *Profile) *utls.ClientHelloSpec {
	cipherSuites := defaultCipherSuites
	if profile != nil && len(profile.CipherSuites) > 0 {
		cipherSuites = profile.CipherSuites
	}

	curves := []utls.CurveID{utls.X25519, utls.CurveP256, utls.CurveP384}
	if profile != nil && len(profile.Curves) > 0 {
		curves = make([]utls.CurveID, len(profile.Curves))
		for i, c := range profile.Curves {
			curves[i] = utls.CurveID(c)
		}
	}

	pointFormats := defaultPointFormats
	if profile != nil && len(profile.PointFormats) > 0 {
		pointFormats = profile.PointFormats
	}

	signatureAlgorithms := defaultSignatureAlgorithms
	if profile != nil && len(profile.SignatureAlgorithms) > 0 {
		signatureAlgorithms = profile.SignatureAlgorithms
	}

	alpnProtocols := []string{"http/1.1"}
	if profile != nil && len(profile.ALPNProtocols) > 0 {
		alpnProtocols = profile.ALPNProtocols
	}

	supportedVersions := []uint16{utls.VersionTLS13, utls.VersionTLS12}
	if profile != nil && len(profile.SupportedVersions) > 0 {
		supportedVersions = profile.SupportedVersions
	}

	keyShareGroups := []utls.CurveID{utls.X25519}
	if profile != nil && len(profile.KeyShareGroups) > 0 {
		keyShareGroups = make([]utls.CurveID, len(profile.KeyShareGroups))
		for i, g := range profile.KeyShareGroups {
			keyShareGroups[i] = utls.CurveID(g)
		}
	}

	pskModes := []uint16{uint16(utls.PskModeDHE)}
	if profile != nil && len(profile.PSKModes) > 0 {
		pskModes = profile.PSKModes
	}

	extOrder := defaultExtensionOrder
	if profile != nil && len(profile.Extensions) > 0 {
		extOrder = profile.Extensions
	}

	enableGREASE := profile != nil && profile.EnableGREASE

	// Build key shares
	keyShares := make([]utls.KeyShare, len(keyShareGroups))
	for i, g := range keyShareGroups {
		keyShares[i] = utls.KeyShare{Group: g}
	}

	// Build extensions
	extensions := make([]utls.TLSExtension, 0, len(extOrder)+2)
	for _, id := range extOrder {
		if isGREASEValue(id) {
			extensions = append(extensions, &utls.UtlsGREASEExtension{})
			continue
		}
		switch id {
		case 0: // server_name
			extensions = append(extensions, &utls.SNIExtension{})
		case 5: // status_request
			extensions = append(extensions, &utls.StatusRequestExtension{})
		case 10: // supported_groups
			extensions = append(extensions, &utls.SupportedCurvesExtension{Curves: curves})
		case 11: // ec_point_formats
			extensions = append(extensions, &utls.SupportedPointsExtension{SupportedPoints: toUint8s(pointFormats)})
		case 13: // signature_algorithms
			extensions = append(extensions, &utls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: toSignatureSchemes(signatureAlgorithms)})
		case 16: // alpn
			extensions = append(extensions, &utls.ALPNExtension{AlpnProtocols: alpnProtocols})
		case 18: // signed_certificate_timestamp
			extensions = append(extensions, &utls.SCTExtension{})
		case 23: // extended_master_secret
			extensions = append(extensions, &utls.ExtendedMasterSecretExtension{})
		case 35: // session_ticket
			extensions = append(extensions, &utls.SessionTicketExtension{})
		case 43: // supported_versions
			extensions = append(extensions, &utls.SupportedVersionsExtension{Versions: supportedVersions})
		case 45: // psk_key_exchange_modes
			extensions = append(extensions, &utls.PSKKeyExchangeModesExtension{Modes: toUint8s(pskModes)})
		case 51: // key_share
			extensions = append(extensions, &utls.KeyShareExtension{KeyShares: keyShares})
		case 0xfe0d: // encrypted_client_hello (65037)
			extensions = append(extensions, &utls.GREASEEncryptedClientHelloExtension{})
		case 0xff01: // renegotiation_info
			extensions = append(extensions, &utls.RenegotiationInfoExtension{})
		default:
			extensions = append(extensions, &utls.GenericExtension{Id: id})
		}
	}

	if enableGREASE && (profile == nil || len(profile.Extensions) == 0) {
		extensions = append([]utls.TLSExtension{&utls.UtlsGREASEExtension{}}, extensions...)
		extensions = append(extensions, &utls.UtlsGREASEExtension{})
	}

	return &utls.ClientHelloSpec{
		CipherSuites:       cipherSuites,
		CompressionMethods: []uint8{0},
		Extensions:         extensions,
		TLSVersMax:         utls.VersionTLS13,
		TLSVersMin:         utls.VersionTLS10,
	}
}

func isGREASEValue(v uint16) bool {
	return v&0x0f0f == 0x0a0a && v>>8 == v&0xff
}

func toUint8s(vals []uint16) []uint8 {
	out := make([]uint8, len(vals))
	for i, v := range vals {
		out[i] = uint8(v)
	}
	return out
}

func toSignatureSchemes(vals []uint16) []utls.SignatureScheme {
	out := make([]utls.SignatureScheme, len(vals))
	for i, v := range vals {
		out[i] = utls.SignatureScheme(v)
	}
	return out
}

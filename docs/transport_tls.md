# Transport TLS Configuration

TLS is enabled by default since v0.50.0 for all transport protocols (TCP, QUIC, KCP). This document describes how to configure TLS certificates for encrypted communication between frpc and frps, including mutual TLS (mTLS) with client certificate verification.

## Overview

| Protocol | TLS Support | Client Cert Verification |
|----------|-------------|--------------------------|
| TCP      | Yes (default) | Yes, when `trustedCaFile` is set |
| QUIC     | Yes (TLS 1.3) | Yes, when `trustedCaFile` is set |
| KCP      | Yes (default) | Yes, when `trustedCaFile` is set |

When `transport.tls.trustedCaFile` is configured on frps, the server enables `RequireAndVerifyClientCert` — all protocols enforce client certificate verification using the same TLS config. For QUIC, the config is cloned and `NextProtos` is set to `["frp"]` for ALPN negotiation.

## Configuration Reference

### frps (Server)

```toml
# Force TLS-only connections. Automatically enabled when trustedCaFile is set.
transport.tls.force = false

# Server certificate and key. If not set, frps generates a random self-signed certificate.
transport.tls.certFile = "server.crt"
transport.tls.keyFile = "server.key"

# CA certificate for verifying client certificates.
# When set, frps requires all clients to present a valid certificate signed by this CA.
transport.tls.trustedCaFile = "ca.crt"

# CRL (Certificate Revocation List) file in PEM format.
# Requires trustedCaFile to be set. Reload at runtime via POST /api/reload_tls.
transport.tls.crlFile = "crl.pem"
```

### frpc (Client)

```toml
# Enable TLS. Default is true since v0.50.0.
transport.tls.enable = true

# Client certificate and key for mTLS (required when server sets trustedCaFile).
transport.tls.certFile = "client.crt"
transport.tls.keyFile = "client.key"

# CA certificate for verifying the server's certificate.
# If not set, server certificate verification is skipped (InsecureSkipVerify).
transport.tls.trustedCaFile = "ca.crt"

# Server name for SNI. Defaults to serverAddr if not set.
transport.tls.serverName = "example.com"

# Skip server name (SNI) verification while still verifying the certificate chain.
transport.tls.skipServerNameVerify = false

# Disable the custom first byte (0x17) used by frp TLS detection.
# Default is true since v0.50.0 (standard TLS ClientHello).
transport.tls.disableCustomTLSFirstByte = true
```

## Common Scenarios

### 1. Default TLS (No Custom Certificates)

No TLS configuration needed. frps auto-generates a self-signed certificate, and frpc connects with TLS but skips server verification.

```
frps: no config needed
frpc: no config needed (tls enabled by default)
```

### 2. Server Certificate with CA Verification

frps presents a server certificate; frpc verifies it against a CA.

```toml
# frps.toml
transport.tls.certFile = "server.crt"
transport.tls.keyFile = "server.key"

# frpc.toml
transport.tls.trustedCaFile = "ca.crt"
```

### 3. Mutual TLS (mTLS)

Both sides verify each other's certificates. The server requires clients to present a valid certificate.

```toml
# frps.toml
transport.tls.certFile = "server.crt"
transport.tls.keyFile = "server.key"
transport.tls.trustedCaFile = "ca.crt"

# frpc.toml
transport.tls.certFile = "client.crt"
transport.tls.keyFile = "client.key"
transport.tls.trustedCaFile = "ca.crt"
```

When mTLS is enabled, frps logs client certificate info on each connection:

```
[INFO] TLS client cert, remote: 192.168.1.100:54321, CN: client1, OU: [engineering]
[INFO] QUIC TLS client cert, remote: 192.168.1.100:54322, CN: client1, OU: [engineering]
```

### 4. mTLS with Certificate Revocation (CRL)

```toml
# frps.toml
transport.tls.certFile = "server.crt"
transport.tls.keyFile = "server.key"
transport.tls.trustedCaFile = "ca.crt"
transport.tls.crlFile = "crl.pem"
```

Revoked client certificates are rejected during the TLS handshake. Update the CRL file and reload at runtime:

```bash
curl -X POST http://frps-address:port/api/reload_tls
```

### 5. Server Name Mismatch

When the server certificate's CN/SAN doesn't match the connection hostname:

```toml
# frpc.toml
transport.tls.trustedCaFile = "ca.crt"
transport.tls.serverName = "my-server.example.com"
# or skip SNI verification entirely:
transport.tls.skipServerNameVerify = true
```

## Certificate Requirements

- All certificate files must be in PEM format.
- `crlFile` must contain PEM-encoded X509 CRL blocks.
- The CA certificate used in `trustedCaFile` must be the same CA that signed the client certificates.
- Client certificates must have a valid CN and/or OU for identification in server logs.

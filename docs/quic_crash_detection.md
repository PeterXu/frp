# QUIC vs TCP Crash Detection

## Overview

When frps process crashes, TCP and QUIC have different detection behaviors on the client side:

| Protocol | Detection Mechanism | Detection Time |
|----------|---------------------|----------------|
| TCP | Kernel sends FIN/RST when process exits | **Immediate** (milliseconds) |
| QUIC (default) | Timeout-based (no Stateless Reset token match) | **~60 seconds** |
| QUIC (with StatelessResetKey + heartbeat) | Stateless Reset token recognized | **Immediate** (~10-15s) |

## Why QUIC is slower by default

QUIC is a user-space protocol (implemented by quic-go library), not handled by kernel:

1. When frps crashes, no QUIC CONNECTION_CLOSE frame is sent (process dies before library can react)
2. Stateless Reset tokens are generated with a random key at startup
3. After restart, new frps has a new random key → old tokens don't match
4. Clients can't recognize Stateless Reset → must wait for idle timeout (60s)

TCP is handled by kernel:
- When process exits, kernel automatically sends FIN/RST to all connected sockets
- Client receives FIN/RST immediately → detects closure instantly

## Critical: Packet Size Requirement

quic-go only sends Stateless Resets for packets **larger than 42 bytes**. This is defined in `protocol.MinStatelessResetSize`:
```
MinStatelessResetSize = 1 (first byte) + 20 (max conn ID) + 4 (max packet number) + 1 (min payload) + 16 (token) = 42 bytes
```

**This is why QUIC-level PING frames (~7-9 bytes) DON'T trigger Stateless Resets!**

To enable fast crash detection, frpc must send **application-level heartbeat messages** which are larger than 42 bytes and WILL trigger Stateless Resets from the server.

## Solution: StatelessResetKey + Application Heartbeat

### 1. Configure StatelessResetKey on Server

```toml
# frps.toml
transport.quic.statelessResetKey = "ABCDEFGHIJKLMNOPQRSTUVWXYZ012345"
```

**Requirements:**
- 32-byte key, base64-encoded
- Must be **same value** across server restarts (persist in config file)
- Only effective on frps (server side)

### 2. Application Heartbeat is Enabled for QUIC

Starting from this version, frpc automatically enables application-level heartbeat when using QUIC protocol:
- `heartbeatInterval = 10` (default for QUIC)
- `heartbeatTimeout = 90`

This is necessary because:
- QUIC PING frames are too small to trigger Stateless Resets
- Application heartbeat packets (~50+ bytes) WILL trigger Stateless Resets
- Enables fast crash detection (~10-15s instead of ~60s)

### Generate a StatelessResetKey

```bash
# Method 1: openssl
openssl rand -base64 32 | head -c 32 | base64

# Method 2: python
python3 -c "import base64, os; print(base64.b64encode(os.urandom(32)).decode())"
```

## Configuration Example

**frps.toml:**
```toml
bindAddr = "0.0.0.0"
bindPort = 7000

transport.quic.keepalivePeriod = 10
transport.quic.maxIdleTimeout = 30
transport.quic.statelessResetKey = "kR8PA+VqE3wYzN7tH2jF5cB9mX1dS4vL6gU0oI+pJxM="
```

**frpc.toml:**
```toml
serverAddr = "127.0.0.1"
serverPort = 7000
transport.protocol = "quic"
# Application heartbeat is automatically enabled for QUIC (interval=10s)
# Can be customized:
# transport.heartbeatInterval = 10
# transport.heartbeatTimeout = 90
```

## QUIC Configuration Reference

| Option | Default | Description |
|--------|---------|-------------|
| `keepalivePeriod` | 10s | Interval for sending QUIC keepalive PING frames. Capped at half of `maxIdleTimeout`. **Note: PING frames are small and won't trigger Stateless Resets** |
| `maxIdleTimeout` | 30s | Maximum duration without incoming packets before connection closes |
| `maxIncomingStreams` | 100000 | Maximum concurrent bidirectional streams |
| `statelessResetKey` | (random) | Persistent 32-byte key for Stateless Reset tokens. Must be base64-encoded |
| `heartbeatInterval` | 10 (for QUIC) | Application-level heartbeat interval. Necessary for fast crash detection |

## Timing Analysis

### Without StatelessResetKey (default)

- Server crash at time T
- Client sends keepalive PING (~7-9 bytes) → **too small, server won't send Stateless Reset**
- Client waits for `maxIdleTimeout` (60s)
- Connection closed at T + ~60s

### With StatelessResetKey + Application Heartbeat

- Server crash at time T
- Client sends application heartbeat packet (~50+ bytes) after ~10s
- Server sends Stateless Reset with **recognized token**
- Client immediately detects closure at T + ~10-15s

## When to Use

**Recommended:** Configure `statelessResetKey` when:
- Using QUIC protocol for frpc-frps connection
- Need fast recovery from server crashes (production environments)
- Want detection behavior similar to TCP

**Optional:** Without configuration, detection still works but takes ~60s, which may be acceptable for some scenarios.

## Implementation Details

### Why Application Heartbeat is Needed

The quic-go library has a security measure to prevent Stateless Reset loops: it only sends Stateless Resets for packets larger than `MinStatelessResetSize` (42 bytes). This prevents:
- Attackers from triggering floods of Stateless Resets
- Stateless Reset loops between peers

QUIC PING frames are typically 7-9 bytes:
- 1 byte: packet type (short header)
- 4 bytes: destination connection ID
- 1-2 bytes: packet number
- 1 byte: PING frame type

These small packets are ignored by the Stateless Reset mechanism, so clients must rely on timeout-based detection.

Application heartbeat messages sent over QUIC streams are much larger:
- Short header packet (~6 bytes)
- STREAM frame header (~4-8 bytes)
- Serialized Ping message (~50+ bytes JSON)
- Total: ~60-70 bytes → above 42-byte threshold

This triggers Stateless Resets from the server, enabling fast crash detection.

### Code Changes

The client config now automatically enables application heartbeat for QUIC protocol:

```go
// pkg/config/v1/client.go
if c.Protocol == "quic" {
    c.HeartbeatInterval = util.EmptyOr(c.HeartbeatInterval, 10)
    c.HeartbeatTimeout = util.EmptyOr(c.HeartbeatTimeout, 90)
}
```

This ensures that QUIC connections always have application-level heartbeat enabled, regardless of TCPMux setting.
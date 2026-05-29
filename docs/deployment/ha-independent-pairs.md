# High Availability Deployment: Independent Pairs

## Architecture Overview

Each machine runs an independent `frps + frpc` pair. Services on the machine connect to their local `frps` only.

```
Machine A                              Machine B
┌─────────────────────────────────┐   ┌─────────────────────────────────┐
│  [frps1] :7000                   │   │  [frps2] :7000                   │
│       │                          │   │       │                          │
│       │                          │   │       │                          │
│   [frpc1]                        │   │   [frpc2]                        │
│       │                          │   │       │                          │
│  [Backend Services]              │   │  [Backend Services]              │
│    - web1:8080                   │   │    - web3:8080                   │
│    - web2:8081                   │   │    - web4:8081                   │
└─────────────────────────────────┘   └─────────────────────────────────┘
        │                                          │
        │         NO CROSS-MACHINE CONNECTIONS      │
        └──────────────────────────────────────────┘
```

## Features

- ✅ **Zero cross-machine complexity**: No distributed state, no leader election
- ✅ **Independent failure domains**: Machine A failure doesn't affect Machine B
- ✅ **Client-side load balancing**: Multiple backends per machine via `group` feature
- ✅ **Health check**: Automatic removal of failed backends
- ✅ **Simple deployment**: Each machine configured independently

## When to Use

- Each machine serves different users/services
- Machine-level failure is acceptable (admin will fix)
- You want maximum simplicity
- No requirement for automatic cross-machine failover

## Quick Start

### Prerequisites

- Go 1.21+ (for building from source)
- Linux/macOS system with systemd (for production)
- Two or more machines

### Option 1: Use Pre-built Binaries

```bash
# Download from GitHub releases
wget https://github.com/fatedier/frp/releases/download/v0.60.0/frp_0.60.0_linux_amd64.tar.gz
tar -xzf frp_0.60.0_linux_amd64.tar.gz
cd frp_0.60.0_linux_amd64
```

### Option 2: Build from Source

```bash
cd /path/to/frp
make build
# Binaries: bin/frps, bin/frpc
```

## Configuration Templates

### Machine A Configuration

**`frps.toml`** (Server):
```toml
bindPort = 7000

# Dashboard (optional)
webServer.addr = "0.0.0.0"
webServer.port = 7500
webServer.user = "admin"
webServer.password = "admin123"

# Authentication (recommended)
auth.token = "your-secret-token-here"

# Logging
log.to = "./frps.log"
log.level = "info"
log.maxDays = 3
```

**`frpc.toml`** (Client):
```toml
serverAddr = "127.0.0.1"
serverPort = 7000

# Authentication (must match frps)
auth.token = "your-secret-token-here"

# Keep retrying on connection failure
loginFailExit = false

# Logging
log.to = "./frpc.log"
log.level = "info"
log.maxDays = 3

# Backend 1
[[proxies]]
name = "web1"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8080
remotePort = 6000

# Backend 2 (load balancing example)
[[proxies]]
name = "web2"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8081
remotePort = 6000
loadBalancer.group = "web"
loadBalancer.groupKey = "secret-group-key"

# Health check for web2
healthCheck.type = "tcp"
healthCheck.timeoutSeconds = 3
healthCheck.maxFailed = 3
healthCheck.intervalSeconds = 10
```

### Machine B Configuration

Same as Machine A, but adjust local ports as needed:
- `frps.toml`: identical
- `frpc.toml`: adjust `localPort` values

## Deployment Scripts

See `scripts/` directory:
- `deploy-machine-a.sh` - Deploy frps+frpc on Machine A
- `deploy-machine-b.sh` - Deploy frps+frpc on Machine B
- `systemd/` - systemd service files

## Running

### Development (Manual)

```bash
# Terminal 1: Start frps
./frps -c frps.toml

# Terminal 2: Start frpc
./frpc -c frpc.toml
```

### Production (systemd)

```bash
# Install systemd services
sudo cp systemd/frps@.service /etc/systemd/system/
sudo cp systemd/frpc@.service /etc/systemd/system/

# Enable and start services (Machine A)
sudo systemctl enable frps@machine-a
sudo systemctl enable frpc@machine-a
sudo systemctl start frps@machine-a
sudo systemctl start frpc@machine-a

# Check status
sudo systemctl status frps@machine-a
sudo systemctl status frpc@machine-a
```

## Verification

### Check frps Dashboard

Access `http://machine-a:7500` (default credentials: admin/admin123)

### Test Connection

```bash
# From external machine
telnet machine-a-public-ip 6000
# Should connect to either web1:8080 or web2:8081 (round-robin)
```

### Test SOCKS5 Relay (if enabled)

```bash
# Use socks5h:// (remote DNS) — recommended
curl -x socks5h://mygroup:mypassword@machine-a:10800 https://example.com
# Dashboard shows: example.com:443

# Avoid socks5:// (local DNS) — sends IP instead of domain
# curl -x socks5://mygroup:mypassword@machine-a:10800 https://example.com
# Dashboard shows: 93.184.216.34:443, HTTPS may fail with outbound proxy
```

### Check Logs

```bash
# frps logs
tail -f /var/log/frps.log

# frpc logs
tail -f /var/log/frpc.log
```

## Load Balancing with Health Check

### Configuration Example

```toml
# frpc.toml - Multiple backends in same group
[[proxies]]
name = "app-backend-1"
type = "http"
localIP = "127.0.0.1"
localPort = 8080
customDomains = ["app.example.com"]
loadBalancer.group = "app"
loadBalancer.groupKey = "app-secret"
healthCheck.type = "http"
healthCheck.path = "/health"
healthCheck.timeoutSeconds = 3
healthCheck.maxFailed = 3
healthCheck.intervalSeconds = 10

[[proxies]]
name = "app-backend-2"
type = "http"
localIP = "127.0.0.1"
localPort = 8081
customDomains = ["app.example.com"]
loadBalancer.group = "app"
loadBalancer.groupKey = "app-secret"
healthCheck.type = "http"
healthCheck.path = "/health"
healthCheck.timeoutSeconds = 3
healthCheck.maxFailed = 3
healthCheck.intervalSeconds = 10

[[proxies]]
name = "app-backend-3"
type = "http"
localIP = "127.0.0.1"
localPort = 8082
customDomains = ["app.example.com"]
loadBalancer.group = "app"
loadBalancer.groupKey = "app-secret"
healthCheck.type = "http"
healthCheck.path = "/health"
healthCheck.timeoutSeconds = 3
healthCheck.maxFailed = 3
healthCheck.intervalSeconds = 10
```

### How It Works

1. All three proxies register with the same `group` and `remotePort`/`customDomains`
2. `frps` randomly dispatches connections to one of the three backends
3. If a backend fails health check, `frpc` removes it from the group
4. Remaining healthy backends continue to receive traffic

## Monitoring

### Dashboard Access

- Machine A: `http://machine-a:7500`
- Machine B: `http://machine-b:7500`

### Prometheus Metrics (Optional)

Enable in `frps.toml`:
```toml
enablePrometheus = true
```

Metrics available at `http://machine-a:7500/metrics`

## Troubleshooting

### frpc Cannot Connect to frps

```bash
# Check if frps is listening
netstat -tuln | grep 7000

# Check firewall
sudo ufw status
sudo ufw allow 7000/tcp

# Check frps logs
tail -f /var/log/frps.log
```

### Health Check Not Working

```bash
# Check if backend service is running
netstat -tuln | grep 8080

# Test health endpoint manually
curl http://localhost:8080/health

# Increase log level to debug
log.level = "debug"
```

### Proxy Registration Fails

```bash
# Check authentication token matches
# Both frps.toml and frpc.toml must have same token

# Check for duplicate proxy names
# Each proxy must have unique name within same frpc
```

## Security Recommendations

1. **Always use authentication tokens** in production
2. **Change default dashboard credentials**
3. **Limit dashboard access** to internal network only
4. **Use TLS** for external connections (configure `transport.tls`)
5. **Rotate tokens periodically**

## Disaster Recovery

### Machine A Failure

1. `frps1` goes down
2. Local services on Machine A become unavailable
3. **Machine B continues operating normally**
4. Fix Machine A: restart services, no cross-machine impact

### Recovery Steps

```bash
# On Machine A
sudo systemctl restart frps@machine-a
sudo systemctl restart frpc@machine-a

# Verify
sudo systemctl status frps@machine-a
sudo systemctl status frpc@machine-a
curl http://localhost:7500/api/serverinfo
```

## Scaling

### Adding Machine C

1. Copy configuration templates
2. Adjust `frpc.toml` for local backends
3. Update DNS/load balancer to include Machine C
4. No changes needed on Machine A or B

## Advanced Configuration

See full configuration reference:
- Server: `https://github.com/fatedier/frp/blob/dev/pkg/config/v1/server.go`
- Client: `https://github.com/fatedier/frp/blob/dev/pkg/config/v1/client.go`

## Support

- GitHub Issues: https://github.com/fatedier/frp/issues
- Documentation: https://github.com/fatedier/frp/blob/dev/README.md

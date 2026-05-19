# frp Deployment: Independent Pairs Architecture

This directory contains deployment documentation and scripts for running frp in an independent pairs architecture.

## Architecture

Each machine runs an independent `frps + frpc` pair. Services on the machine connect to their local `frps` only.

```
Machine A                              Machine B
┌─────────────────────────────────┐   ┌─────────────────────────────────┐
│  [frps1] :7000                   │   │  [frps2] :7000                   │
│       │                          │   │       │                          │
│   [frpc1]                        │   │   [frpc2]                        │
│       │                          │   │       │                          │
│  [Backend Services]              │   │  [Backend Services]              │
└─────────────────────────────────┘   └─────────────────────────────────┘
```

## Features

- ✅ Zero cross-machine complexity
- ✅ Independent failure domains
- ✅ Client-side load balancing via groups
- ✅ Health check with automatic failover
- ✅ Simple deployment and maintenance

## Quick Start

```bash
# 1. Build frp
make build

# 2. Deploy on Machine A
scp scripts/deploy-machine-a.sh user@machine-a:/tmp/
ssh user@machine-a
sudo /tmp/deploy-machine-a.sh
sudo systemctl start frps@machine-a frpc@machine-a

# 3. Deploy on Machine B
scp scripts/deploy-machine-b.sh user@machine-b:/tmp/
ssh user@machine-b
sudo /tmp/deploy-machine-b.sh
sudo systemctl start frps@machine-b frpc@machine-b
```

See [QUICKSTART.md](QUICKSTART.md) for detailed instructions.

## Documentation

| File | Description |
|------|-------------|
| [ha-independent-pairs.md](ha-independent-pairs.md) | Full documentation with architecture, configuration, and troubleshooting |
| [QUICKSTART.md](QUICKSTART.md) | 5-minute setup guide |

## Scripts

| Script | Description |
|--------|-------------|
| [scripts/deploy-machine-a.sh](scripts/deploy-machine-a.sh) | Deploy frps+frpc on Machine A |
| [scripts/deploy-machine-b.sh](scripts/deploy-machine-b.sh) | Deploy frps+frpc on Machine B |

## Configuration Templates

| Template | Description |
|----------|-------------|
| [templates/production-frps.toml](templates/production-frps.toml) | Production frps configuration with security best practices |
| [templates/production-frpc.toml](templates/production-frpc.toml) | Production frpc configuration with examples |

## File Structure After Deployment

```
/opt/frp/
├── config/
│   ├── machine-a/
│   │   ├── frps.toml
│   │   └── frpc.toml
│   └── machine-b/
│       ├── frps.toml
│       └── frpc.toml
/var/log/frp/
├── frps.log
└── frpc.log
/etc/systemd/system/
├── frps@machine-a.service
├── frpc@machine-a.service
├── frps@machine-b.service
└── frpc@machine-b.service
```

## Common Operations

### View Status
```bash
sudo systemctl status frps@machine-a frpc@machine-a
```

### View Logs
```bash
sudo journalctl -u frps@machine-a -f
```

### Restart Services
```bash
sudo systemctl restart frps@machine-a frpc@machine-a
```

### Reload Configuration
```bash
# NOTE: frps/frpc do not support hot reload
sudo systemctl restart frps@machine-a
sudo systemctl restart frpc@machine-a
```

### Access Dashboard
```bash
# Machine A (local access only by default)
http://127.0.0.1:7500

# Machine B (local access only by default)
http://127.0.0.1:7500

# For external access, edit frps.toml and restart:
# webServer.addr = "0.0.0.0"
```

### Uninstall
```bash
# Stop and disable services
sudo systemctl stop frps@machine-a frpc@machine-a
sudo systemctl disable frps@machine-a frpc@machine-a

# Remove systemd service files
sudo rm /etc/systemd/system/frps@machine-a.service
sudo rm /etc/systemd/system/frpc@machine-a.service
sudo systemctl daemon-reload

# Remove binaries and configs
sudo rm /usr/local/bin/frps /usr/local/bin/frpc
sudo rm -rf /opt/frp/config/machine-a

# Optionally remove logs
sudo rm -rf /var/log/frp
```

## Security Checklist

- [ ] Change default authentication tokens (both frps.toml and frpc.toml)
- [ ] Change default dashboard credentials
- [ ] Dashboard bound to 127.0.0.1 (local only) - edit to 0.0.0.0 if external access needed
- [ ] Enable TLS for production connections
- [ ] Configure firewall rules (ufw configured for frps port only)
- [ ] Open additional firewall ports for each proxy as needed
- [ ] Set up log monitoring and rotation
- [ ] Rotate credentials regularly

## Load Balancing Example

```toml
# frpc.toml - Multiple backends with health checks
[[proxies]]
name = "app-backend-1"
type = "http"
localPort = 8080
customDomains = ["app.example.com"]
loadBalancer.group = "app"
loadBalancer.groupKey = "app-secret"

[proxies.healthCheck]
type = "http"
path = "/health"
timeoutSeconds = 3
maxFailed = 3
intervalSeconds = 10

[[proxies]]
name = "app-backend-2"
type = "http"
localPort = 8081
customDomains = ["app.example.com"]
loadBalancer.group = "app"
loadBalancer.groupKey = "app-secret"

[proxies.healthCheck]
type = "http"
path = "/health"
timeoutSeconds = 3
maxFailed = 3
intervalSeconds = 10
```

## Troubleshooting

### Service Won't Start
```bash
sudo systemctl status frps@machine-a
sudo journalctl -u frps@machine-a -n 50
```

### Connection Refused
```bash
sudo netstat -tuln | grep 7000
sudo ufw status
```

### Authentication Error
```bash
# Verify tokens match
grep "auth.token" /opt/frp/config/machine-a/frps.toml
grep "auth.token" /opt/frp/config/machine-a/frpc.toml
```

## Support

- GitHub Issues: https://github.com/fatedier/frp/issues
- Full Documentation: https://github.com/fatedier/frp/blob/dev/README.md
- frp Home: https://github.com/fatedier/frp

## License

Apache License 2.0

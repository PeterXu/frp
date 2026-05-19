# Quick Start Guide: Independent Pairs Deployment

## 5-Minute Setup

### Prerequisites
- Two Linux machines (Machine A, Machine B)
- SSH access with sudo privileges
- Go 1.21+ installed (for building)

### Step 1: Build frp

```bash
cd /path/to/frp
make build
```

### Step 2: Deploy on Machine A

```bash
# Copy to Machine A
scp doc/deployment/scripts/deploy-machine-a.sh user@machine-a:/tmp/

# SSH to Machine A
ssh user@machine-a

# Run deployment
sudo /tmp/deploy-machine-a.sh

# Start services
sudo systemctl start frps@machine-a frpc@machine-a
```

### Step 3: Deploy on Machine B

```bash
# Copy to Machine B
scp doc/deployment/scripts/deploy-machine-b.sh user@machine-b:/tmp/

# SSH to Machine B
ssh user@machine-b

# Run deployment
sudo /tmp/deploy-machine-b.sh

# Start services
sudo systemctl start frps@machine-b frpc@machine-b
```

### Step 4: Verify

```bash
# Machine A
sudo systemctl status frps@machine-a frpc@machine-a
curl http://127.0.0.1:7500/api/serverinfo

# Machine B
sudo systemctl status frps@machine-b frpc@machine-b
curl http://127.0.0.1:7500/api/serverinfo
```

### Step 5: Test Connection

```bash
# From Machine A - SSH proxy
ssh -p 6000 localhost

# From external machine - if ports are forwarded
ssh -p 6000 machine-a-public-ip
```

## Configuration Files Location

```
/opt/frp/config/
├── machine-a/
│   ├── frps.toml
│   └── frpc.toml
└── machine-b/
    ├── frps.toml
    └── frpc.toml
```

## Common Commands

### View Logs
```bash
# systemd journal
sudo journalctl -u frps@machine-a -f
sudo journalctl -u frpc@machine-a -f

# Log files
tail -f /var/log/frp/frps.log
tail -f /var/log/frp/frpc.log
```

### Restart Services
```bash
sudo systemctl restart frps@machine-a frpc@machine-a
```

### Reload Configuration
```bash
# Edit config files
sudo nano /opt/frp/config/machine-a/frpc.toml

# NOTE: frps/frpc do not support hot reload. Restart required.
sudo systemctl restart frps@machine-a
sudo systemctl restart frpc@machine-a
```

## Customization

### Change Authentication Token

Edit both `frps.toml` and `frpc.toml`:
```toml
auth.token = "your-new-secret-token"
```

Then restart (reload not supported):
```bash
sudo systemctl restart frps@machine-a frpc@machine-a
```

### Add New Proxies

Edit `frpc.toml` and add:
```toml
[[proxies]]
name = "my-service"
type = "tcp"
localIP = "127.0.0.1"
localPort = 3000
remotePort = 7000
```

Then restart frpc (reload not supported):
```bash
sudo systemctl restart frpc@machine-a
```

### Enable Load Balancing

Add multiple proxies with same `remotePort`/`customDomains` and `group`:
```toml
[[proxies]]
name = "app-1"
type = "http"
localPort = 8080
customDomains = ["app.example.com"]
loadBalancer.group = "app"
loadBalancer.groupKey = "app-key"

[[proxies]]
name = "app-2"
type = "http"
localPort = 8081
customDomains = ["app.example.com"]
loadBalancer.group = "app"
loadBalancer.groupKey = "app-key"

[[proxies]]
name = "app-3"
type = "http"
localPort = 8082
customDomains = ["app.example.com"]
loadBalancer.group = "app"
loadBalancer.groupKey = "app-key"
```

### Add Health Check

```toml
[[proxies]]
name = "app-1"
type = "http"
localPort = 8080
customDomains = ["app.example.com"]
loadBalancer.group = "app"
loadBalancer.groupKey = "app-key"

healthCheck.type = "http"
healthCheck.path = "/health"
healthCheck.timeoutSeconds = 3
healthCheck.maxFailed = 3
healthCheck.intervalSeconds = 10
```

## Troubleshooting

### Service Won't Start

```bash
# Check status
sudo systemctl status frps@machine-a

# View logs
sudo journalctl -u frps@machine-a -n 50

# Check port availability
netstat -tuln | grep 7000
```

### Connection Refused

```bash
# Check firewall
sudo ufw status
sudo ufw allow 7000/tcp

# Check if frps is listening
sudo netstat -tuln | grep 7000
```

### Authentication Error

Ensure `auth.token` matches in both `frps.toml` and `frpc.toml`:
```bash
grep "auth.token" /opt/frp/config/machine-a/frps.toml
grep "auth.token" /opt/frp/config/machine-a/frpc.toml
```

## Security Checklist

- [ ] Change default dashboard password
- [ ] Set strong auth token
- [ ] Restrict dashboard to internal network
- [ ] Enable TLS for external connections
- [ ] Configure firewall rules
- [ ] Set up log rotation
- [ ] Monitor service health

## Next Steps

- Read full documentation: `doc/deployment/ha-independent-pairs.md`
- Configure TLS for production: `README.md` - TLS section
- Set up monitoring: Enable Prometheus in `frps.toml`
- Configure DNS for your domains

## Support

- GitHub: https://github.com/fatedier/frp
- Documentation: https://github.com/fatedier/frp/blob/dev/README.md

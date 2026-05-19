#!/bin/bash
# deploy-machine-b.sh - Deploy frps + frpc on Machine B
# Usage: sudo ./deploy-machine-b.sh

set -e

# Configuration
MACHINE_NAME="machine-b"
FRPS_PORT=7000
DASHBOARD_PORT=7500
AUTH_TOKEN="change-me-in-production"
BASE_DIR="/opt/frp"
LOG_DIR="/var/log/frp"
BIN_DIR="/usr/local/bin"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   log_error "This script must be run as root (use sudo)"
   exit 1
fi

# Create directories with proper permissions
log_info "Creating directories..."
mkdir -p "$BASE_DIR/config/$MACHINE_NAME"
mkdir -p "$LOG_DIR"
mkdir -p "$BIN_DIR"

# Set log directory permissions for systemd service (runs as nobody)
chmod 755 "$LOG_DIR"

# Detect binary location with fallback
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

if [[ -f "$PROJECT_ROOT/bin/frps" && -f "$PROJECT_ROOT/bin/frpc" ]]; then
    log_info "Using binaries from project build..."
    FRPS_BIN="$PROJECT_ROOT/bin/frps"
    FRPC_BIN="$PROJECT_ROOT/bin/frpc"
elif [[ -f "./bin/frps" && -f "./bin/frpc" ]]; then
    log_info "Using binaries from ./bin..."
    FRPS_BIN="./bin/frps"
    FRPC_BIN="./bin/frpc"
elif [[ -f "/usr/local/bin/frps" && -f "/usr/local/bin/frpc" ]]; then
    log_info "Using already installed binaries..."
    FRPS_BIN="/usr/local/bin/frps"
    FRPC_BIN="/usr/local/bin/frpc"
else
    log_error "Binaries not found. Please run 'make build' first or ensure binaries are in ./bin or /usr/local/bin"
    exit 1
fi

# Check version
log_info "Checking frp version..."
FRP_VERSION=$($FRPS_BIN --version 2>&1 || echo "unknown")
log_info "Using frp version: $FRP_VERSION"

# Backup existing configurations if they exist
if [[ -f "$BASE_DIR/config/$MACHINE_NAME/frps.toml" ]]; then
    log_warn "Backing up existing frps.toml..."
    cp "$BASE_DIR/config/$MACHINE_NAME/frps.toml" \
       "$BASE_DIR/config/$MACHINE_NAME/frps.toml.bak.$(date +%Y%m%d_%H%M%S)"
fi

if [[ -f "$BASE_DIR/config/$MACHINE_NAME/frpc.toml" ]]; then
    log_warn "Backing up existing frpc.toml..."
    cp "$BASE_DIR/config/$MACHINE_NAME/frpc.toml" \
       "$BASE_DIR/config/$MACHINE_NAME/frpc.toml.bak.$(date +%Y%m%d_%H%M%S)"
fi

# Install binaries
log_info "Installing binaries..."
cp "$FRPS_BIN" "$BIN_DIR/frps"
cp "$FRPC_BIN" "$BIN_DIR/frpc"
chmod +x "$BIN_DIR/frps" "$BIN_DIR/frpc"

# Generate frps.toml with secure defaults
log_info "Generating frps.toml..."
cat > "$BASE_DIR/config/$MACHINE_NAME/frps.toml" << EOF
bindPort = $FRPS_PORT

# Dashboard - binds to localhost by default for security
# Change to 0.0.0.0 if external access is needed
webServer.addr = "127.0.0.1"
webServer.port = $DASHBOARD_PORT
webServer.user = "admin"
webServer.password = "admin123"

# Authentication
auth.token = "$AUTH_TOKEN"

# Logging
log.to = "$LOG_DIR/frps.log"
log.level = "info"
log.maxDays = 3

# Optional: Enable Prometheus metrics
enablePrometheus = true
EOF

# Generate frpc.toml
log_info "Generating frpc.toml..."
cat > "$BASE_DIR/config/$MACHINE_NAME/frpc.toml" << EOF
serverAddr = "127.0.0.1"
serverPort = $FRPS_PORT

# Authentication (must match frps)
auth.token = "$AUTH_TOKEN"

# Keep retrying on connection failure
loginFailExit = false

# Logging
log.to = "$LOG_DIR/frpc.log"
log.level = "info"
log.maxDays = 3

# Example: TCP proxy to local service
[[proxies]]
name = "ssh"
type = "tcp"
localIP = "127.0.0.1"
localPort = 22
remotePort = 6000

# Example: HTTP proxy with load balancing group
[[proxies]]
name = "web-backend-1"
type = "http"
localIP = "127.0.0.1"
localPort = 8080
customDomains = ["app-machine-b.example.com"]

# Example: Another HTTP proxy in same load balancing group
[[proxies]]
name = "web-backend-2"
type = "http"
localIP = "127.0.0.1"
localPort = 8081
customDomains = ["app-machine-b.example.com"]
loadBalancer.group = "web"
loadBalancer.groupKey = "web-secret"

# Health check for web-backend-2
healthCheck.type = "http"
healthCheck.path = "/health"
healthCheck.timeoutSeconds = 3
healthCheck.maxFailed = 3
healthCheck.intervalSeconds = 10
EOF

# Create systemd service files
log_info "Creating systemd services..."

# frps service - NOTE: frps does not support config reload, requires restart
cat > "/etc/systemd/system/frps@$MACHINE_NAME.service" << EOF
[Unit]
Description=frps Service (%i)
After=network.target

[Service]
Type=simple
User=nobody
Restart=on-failure
RestartSec=5s
ExecStart=$BIN_DIR/frps -c $BASE_DIR/config/%i/frps.toml
# NOTE: frps does not support hot reload. Use restart instead.
# ExecReload=/bin/kill -HUP \$MAINPID
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

# frpc service - NOTE: frpc does not support config reload, requires restart
cat > "/etc/systemd/system/frpc@$MACHINE_NAME.service" << EOF
[Unit]
Description=frpc Service (%i)
After=network.target frps@%i.service

[Service]
Type=simple
User=nobody
Restart=on-failure
RestartSec=5s
ExecStart=$BIN_DIR/frpc -c $BASE_DIR/config/%i/frpc.toml
# NOTE: frpc does not support hot reload. Use restart instead.
# ExecReload=/bin/kill -HUP \$MAINPID
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

# Configure firewall (if ufw is available)
if command -v ufw &> /dev/null; then
    log_info "Configuring firewall..."
    ufw allow $FRPS_PORT/tcp || log_warn "Failed to configure firewall for port $FRPS_PORT"
    # Dashboard port only if you need external access
    ufw allow from 127.0.0.1 to any port $DASHBOARD_PORT proto tcp || log_warn "Failed to configure firewall for dashboard"
    log_warn "Firewall configured. Open additional proxy ports as needed."
else
    log_warn "ufw not found, skipping firewall configuration"
fi

# Reload systemd
log_info "Reloading systemd..."
systemctl daemon-reload

# Enable services (but don't start them yet)
log_info "Enabling services..."
systemctl enable "frps@$MACHINE_NAME" 2>/dev/null || true
systemctl enable "frpc@$MACHINE_NAME" 2>/dev/null || true

# Verify installation
log_info "Verifying installation..."
if [[ -x "$BIN_DIR/frps" && -x "$BIN_DIR/frpc" ]]; then
    log_info "Binaries installed successfully"
else
    log_error "Binary verification failed"
    exit 1
fi

if [[ -f "$BASE_DIR/config/$MACHINE_NAME/frps.toml" && -f "$BASE_DIR/config/$MACHINE_NAME/frpc.toml" ]]; then
    log_info "Configuration files created successfully"
else
    log_error "Configuration verification failed"
    exit 1
fi

# Print summary
echo ""
log_info "Deployment complete!"
echo ""
echo "Configuration files:"
echo "  frps: $BASE_DIR/config/$MACHINE_NAME/frps.toml"
echo "  frpc: $BASE_DIR/config/$MACHINE_NAME/frpc.toml"
echo ""
echo "Commands:"
echo "  Start services:   sudo systemctl start frps@$MACHINE_NAME frpc@$MACHINE_NAME"
echo "  Stop services:    sudo systemctl stop frps@$MACHINE_NAME frpc@$MACHINE_NAME"
echo "  Restart services: sudo systemctl restart frps@$MACHINE_NAME frpc@$MACHINE_NAME"
echo "  Check status:     sudo systemctl status frps@$MACHINE_NAME frpc@$MACHINE_NAME"
echo "  View logs:        sudo journalctl -u frps@$MACHINE_NAME -f"
echo "  Dashboard:        http://127.0.0.1:$DASHBOARD_PORT (local only)"
echo ""
log_warn "IMPORTANT SECURITY NOTES:"
echo "  1. Change the default passwords and tokens!"
echo "  2. Edit: $BASE_DIR/config/$MACHINE_NAME/frps.toml"
echo "  3. Edit: $BASE_DIR/config/$MACHINE_NAME/frpc.toml"
echo "  4. Dashboard is bound to 127.0.0.1 for security"
echo "  5. Open additional firewall ports for your proxies as needed"
echo ""
echo "To start services now, run:"
echo "  sudo systemctl start frps@$MACHINE_NAME frpc@$MACHINE_NAME"

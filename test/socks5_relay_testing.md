# SOCKS5/HTTP CONNECT Relay Testing Guide

This guide covers testing the SOCKS5 relay proxy feature and the dashboard's real-time connection monitoring.

## Quick Start

### 1. Start Services

```bash
# Start frps
./bin/frps --config test-config.toml

# Start frpc  
./bin/frpc --config client-config.toml
```

### 2. Test SOCKS5 Proxy

```bash
# HTTP through SOCKS5
curl -x socks5://testgroup:testpass@127.0.0.1:10800 http://example.com

# HTTPS through SOCKS5
curl -x socks5://testgroup:testpass@127.0.0.1:10800 https://example.com
```

### 3. Test HTTP CONNECT Proxy

```bash
# HTTPS through HTTP CONNECT (correct usage)
curl -x http://testgroup:testpass@127.0.0.1:10801 https://example.com

# Note: HTTP through HTTP CONNECT will fail with "only CONNECT method supported"
# This is expected - HTTP CONNECT proxies only tunnel HTTPS traffic
```

### 4. Dashboard Testing

Access dashboard: `http://127.0.0.1:17500/static/`

- **Connections View** (`#/connections`) - Real-time connection table
- **Topology View** (`#/topology`) - Architecture diagram

## Testing Real-Time Events

### Slow Connection Test (for Dashboard Visibility)

HTTP requests complete in ~100ms, which is too fast to see in the dashboard. Use slow requests:

```bash
# Using httpbin (2 second delay)
curl -x socks5://testgroup:testpass@127.0.0.1:10800 \
  --max-time 5 \
  http://httpbin.org/delay/2
```

### Create Local Slow Server

```python
# slow_server.py
import http.server
import time

class SlowHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-type', 'text/html')
        self.end_headers()
        for i in range(3):
            self.wfile.write(f'Line {i}\n'.encode())
            self.wfile.flush()
            time.sleep(1)

if __name__ == '__main__':
    with http.server.HTTPServer(('', 8766), SlowHandler) as httpd:
        httpd.serve_forever()
```

```bash
# Run slow server
python3 slow_server.py &

# Test through proxy
curl -x socks5://testgroup:testpass@127.0.0.1:10800 \
  --max-time 5 \
  http://127.0.0.1:8766/
```

## SSE Endpoint Testing

### Monitor SSE Events with curl

```bash
# Listen for 10 seconds
timeout 10 curl -s -N \
  http://127.0.0.1:17500/api/socks5relay/events \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" &

# Make test connection while listening
curl -x socks5://testgroup:testpass@127.0.0.1:10800 http://example.com
```

Expected output:
```
event: connected
data: {}

event: created
data: {"type":"created","conn":{"ID":"conn-1",...}}

event: updated
data: {"type":"updated","conn":{"ID":"conn-1",...}}

event: deleted
data: {"type":"deleted","conn":{"ID":"conn-1",...}}
```

### Test SSE in Browser Console

On the Connections page (`http://127.0.0.1:17500/static/#/connections`):

```javascript
// Test SSE manually
const es = new EventSource('../api/socks5relay/events');

es.addEventListener('connected', () => {
  console.log('✓ SSE Connected');
});

es.addEventListener('created', (e) => {
  console.log('✓ Created:', JSON.parse(e.data));
});

es.addEventListener('updated', (e) => {
  console.log('✓ Updated:', JSON.parse(e.data));
});

es.addEventListener('deleted', (e) => {
  console.log('✓ Deleted:', JSON.parse(e.data));
});

es.onerror = (e) => {
  console.error('✗ Error:', e);
};
```

## API Endpoints

### Server Info
```bash
curl http://127.0.0.1:17500/api/serverinfo \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq
```

### SOCKS5 Relay Groups
```bash
curl http://127.0.0.1:17500/api/socks5relay/groups \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq
```

### Active Sessions
```bash
curl http://127.0.0.1:17500/api/socks5relay/sessions \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq
```

### Current Connections
```bash
curl http://127.0.0.1:17500/api/socks5relay/connections \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq
```

### Connection Stats
```bash
curl http://127.0.0.1:17500/api/socks5relay/stats \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq
```

### Retention Settings
```bash
# Get current retention
curl http://127.0.0.1:17500/api/socks5relay/retention \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq

# Set retention to 5 minutes
curl -X PUT http://127.0.0.1:17500/api/socks5relay/retention \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" \
  -H "Content-Type: application/json" \
  -d '{"retentionSeconds": 300}' | jq

# Disable retention (no closed connections stored)
curl -X PUT http://127.0.0.1:17500/api/socks5relay/retention \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" \
  -H "Content-Type: application/json" \
  -d '{"retentionSeconds": 0}' | jq
```

## Concurrent Connection Testing

```bash
# Multiple concurrent connections
for i in {1..10}; do
  curl -x socks5://testgroup:testpass@127.0.0.1:10800 \
    http://example.com &
done

# Wait and check stats
sleep 2
curl http://127.0.0.1:17500/api/socks5relay/stats \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq
```

## New Features Testing

### Recent Connections (Closed Connections)

Test that closed connections are retained and displayed:

```bash
# Set retention to 1 minute for quick testing
curl -X PUT http://127.0.0.1:17500/api/socks5relay/retention \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" \
  -H "Content-Type: application/json" \
  -d '{"retentionSeconds": 60}'

# Make a connection (completes quickly)
curl -x socks5://testgroup:testpass@127.0.0.1:10800 http://example.com

# Check connections - should show in "Recent" tab
curl http://127.0.0.1:17500/api/socks5relay/connections \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq '.[] | {id, isActive, endTime}'
```

Expected output: Connection with `isActive: false` and `endTime` timestamp.

### Retention Adjustment

Test runtime retention changes:

```bash
# Make some connections
for i in {1..3}; do
  curl -x socks5://testgroup:testpass@127.0.0.1:10800 http://example.com &
done
wait

# Check count
echo "Connections before cleanup:"
curl http://127.0.0.1:17500/api/socks5relay/connections \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq '. | length'

# Set retention to 0 (disable)
curl -X PUT http://127.0.0.1:17500/api/socks5relay/retention \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" \
  -H "Content-Type: application/json" \
  -d '{"retentionSeconds": 0}'

# Check count again - should be 0 (or only active)
echo "Connections after disabling retention:"
curl http://127.0.0.1:17500/api/socks5relay/connections \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq '. | length'
```

### IPv6 Address Formatting

Test IPv6 addresses are properly formatted:

```bash
# Use IPv6-only domain (resolves to IPv6)
curl -x socks5://testgroup:testpass@127.0.0.1:10800 \
  --ipv6 \
  http://example.com

# Check connection - should show [addr]:port format
curl http://127.0.0.1:17500/api/socks5relay/connections \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq '.[-1] | {dstAddr, dstPort}'
```

Expected output for IPv6: `"dstAddr": "2606:2800:220:1:248:1893:25c8:1946"` (displayed as `[2606:2800:220:1:248:1893:25c8:1946]:80` in dashboard)

### Domain vs IP Display

Test difference between local DNS resolution and proxy-side DNS:

```bash
# Mode 1: Local DNS resolution (curl default)
curl -x socks5://testgroup:testpass@127.0.0.1:10800 http://example.com
# Dashboard shows: IP address like 93.184.216.34:80

# Mode 2: Proxy-side DNS resolution
curl --socks5-hostname testgroup:testpass@127.0.0.1:10800 http://example.com
# Dashboard shows: example.com:80

# Verify
curl http://127.0.0.1:17500/api/socks5relay/connections \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" | jq '.[-1].dstAddr'
```

### Retention Validation

Test invalid retention values are rejected:

```bash
# Test negative value (should fail)
curl -X PUT http://127.0.0.1:17500/api/socks5relay/retention \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" \
  -H "Content-Type: application/json" \
  -d '{"retentionSeconds": -1}'

# Test over 1 hour (should fail)
curl -X PUT http://127.0.0.1:17500/api/socks5relay/retention \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" \
  -H "Content-Type: application/json" \
  -d '{"retentionSeconds": 4000}'
```

Expected: Both return `{"Code":500,"Msg":"retentionSeconds must be between 0 and 3600"}`

**Reason:** Connections complete too fast (~100ms)

**Solution:** Use slow requests (see "Slow Connection Test" above)

### SSE Not Working

**Check browser console:**
1. Press F12 → Console tab
2. Look for errors like "Failed to connect to EventSource"
3. Check Network tab for `/api/socks5relay/events` request status

**Verify endpoint:**
```bash
curl -v http://127.0.0.1:17500/api/socks5relay/events \
  -H "Authorization: Basic YWRtaW46YWRtaW4="
```

Should return `200 OK` with `Content-Type: text/event-stream`

### HTTP CONNECT Returns "only CONNECT method supported"

**This is correct behavior.** HTTP CONNECT proxies only support HTTPS tunneling.

Use HTTPS:
```bash
curl -x http://testgroup:testpass@127.0.0.1:10801 https://example.com
```

Or use SOCKS5 proxy for both HTTP and HTTPS.

## Configuration Examples

### frps.toml
```toml
bindAddr = "127.0.0.1"
bindPort = 17000

webServer.addr = "127.0.0.1"
webServer.port = 17500
webServer.assetsDir = "./web/frps/dist"

auth.token = "test"

socks5ProxyPort = 10800
socks5ProxyAuthPassword = "testpass"

httpConnectProxyPort = 10801
httpConnectProxyAuthPassword = "testpass"
```

### frpc.toml
```toml
serverAddr = "127.0.0.1"
serverPort = 17000
auth.token = "test"

[[proxies]]
name = "relay-test"
type = "socks5_relay"
group = "testgroup"
```

## Test Checklist

### Basic Functionality
- [ ] SOCKS5 proxy with HTTP
- [ ] SOCKS5 proxy with HTTPS
- [ ] HTTP CONNECT proxy with HTTPS
- [ ] Both protocols visible in dashboard

### Dashboard Features
- [ ] Dashboard Connections view shows real-time updates
- [ ] Dashboard Topology view displays correctly
- [ ] SSE endpoint streams events
- [ ] Multiple concurrent connections
- [ ] Connection stats update correctly

### New Features (Recent Connections & Retention)
- [ ] Recent connections tab displays closed connections
- [ ] Retention adjustment via UI works
- [ ] Retention API (GET/PUT) works correctly
- [ ] Retention validates range (0-3600 seconds)
- [ ] Retention = 0 clears closed connections immediately
- [ ] Retention resets to default (10min) on restart

### Address Display
- [ ] IPv4 addresses display correctly (addr:port)
- [ ] IPv6 addresses display correctly ([addr]:port)
- [ ] Domain names display when using --socks5-hostname
- [ ] IP addresses display when using -x (local DNS)

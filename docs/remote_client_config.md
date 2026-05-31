# Remote Client Config View

frps operators can view a connected frpc's running configuration through the frps dashboard API.

## API Endpoints

All endpoints are behind the frps dashboard auth middleware.

**Global client config:**

```
GET  /api/clients/{key}/config                     # view
```

**Per-proxy config:**

```
GET  /api/clients/{key}/proxies/{name}/config      # view
```

`{key}` is the composite client key, same one used by `/api/clients/{key}`.

## Examples

```bash
# View frpc global config
curl http://frps:7500/api/clients/myclient/config

# View a proxy's config
curl http://frps:7500/api/clients/myclient/proxies/myproxy/config
```

## Control Flow

### Client Config (round-trip to frpc)

```
  frps API Handler            server.Control           [control conn]          client.Control
       |                           |                         |                       |
  1. lookupClientControl(key)     |                         |                       |
     +- clientRegistry.GetByKey   |                         |                       |
        -> ctlManager.GetByID     |                         |                       |
             |                    |                         |                       |
  2. ctl.MsgTransporter().Do()  --+--- GetClientConfig --->|---------------------->|
     (10s timeout, waits by      |                         |    3. handler reads    |
      txID + responseType)       |                         |       sessionCtx      |
             |                    |                         |                       |
             |                    |<--- GetClientConfigResp-|<----------------------|
  4. response dispatched          |    5. dispatcher routes |   4. handler sends    |
     via DispatchWithType()       |       by txID match     |       response back   |
             |                    |                         |                       |
  6. strip txID, return JSON      |                         |                       |
```

**Key steps:**

1. **Lookup** -- `lookupClientControl()` resolves `{key}` to `ClientInfo` to `RunID` to `*Control`.
2. **Send request** -- `MessageTransporter.Do()` sends the message over the control connection and registers a wait channel keyed by `transactionID` + `responseType`.
3. **Client handles** -- The client handler reads the current config from `sessionCtx` and sends a response back.
4. **Response flows back** -- The server dispatcher receives the response and calls `DispatchWithType()`, which matches the transaction ID and delivers the response to the waiting `Do()` call.
5. **API returns** -- The server strips the internal `transaction_id` field and returns JSON to the caller.

### Proxy Config (served directly by frps)

```
  frps API Handler            server.Control
       |                           |
  1. lookupClientControl(key)     |
     +- clientRegistry.GetByKey   |
        -> ctlManager.GetByID     |
             |                    |
  2. ctl.GetProxy(proxyName)      |
     -> pxy.GetConfigurer()       |
             |                    |
  3. return JSON                  |
```

**Key steps:**

1. **Lookup** -- `lookupClientControl()` resolves `{key}` to `*Control`.
2. **Get proxy** -- `ctl.GetProxy(proxyName)` looks up the proxy in the control's proxy map. The proxy's full config is already stored server-side from the `NewProxy` registration.
3. **Return** -- The configurer is serialized directly to JSON. No round-trip to frpc needed.

## Response Fields

### Global Client Config (`GET /api/clients/{key}/config`)

| Field                        | Type                | Description                              |
|------------------------------|---------------------|------------------------------------------|
| `user`                       | string              | Client user name.                        |
| `client_id`                  | string              | Client unique ID.                        |
| `group`                      | string              | Client group.                            |
| `server_addr`                | string              | Server address the client connects to.   |
| `server_port`                | int                 | Server port.                             |
| `version`                    | string              | frpc version.                            |
| `protocol`                   | string              | Transport protocol (e.g. `tcp`, `ws`).   |
| `wire_protocol`              | string              | Wire protocol version (e.g. `v1`, `v2`). |
| `pool_count`                 | int                 | Connection pool size.                    |
| `heartbeat_interval`         | int64               | Seconds between heartbeats.              |
| `heartbeat_timeout`          | int64               | Seconds before heartbeat timeout.        |
| `dial_server_timeout`        | int64               | Dial timeout in seconds.                 |
| `dial_server_keepalive`      | int64               | Dial keepalive interval in seconds.      |
| `tcp_mux`                    | bool                | TCP stream multiplexing enabled.         |
| `tcp_mux_keepalive_interval` | int64               | TCP mux keepalive interval in seconds.   |
| `tls_enabled`                | bool                | TLS enabled for server connection.       |
| `proxy_url`                  | string              | Proxy URL (or env `http_proxy`).         |
| `log_to`                     | string              | Log destination (`console` or file path).|
| `log_level`                  | string              | Current log level.                       |
| `log_max_days`               | int64               | Log retention days.                      |
| `dns_server`                 | string              | Custom DNS server address.               |
| `start`                      | string[]            | List of enabled proxy names.             |
| `udp_packet_size`            | int64               | UDP packet size.                         |
| `metadatas`                  | map[string]string   | Client metadata key-value pairs.         |
| `web_server_addr`            | string              | frpc local web server address.           |
| `web_server_port`            | int                 | frpc local web server port.              |
| `login_fail_exit`            | bool                | Exit on login failure.                   |

### Per-Proxy Config (`GET /api/clients/{key}/proxies/{name}/config`)

Returns proxy type and config as JSON. Different proxy types have different fields. The `config` object contains the full proxy configuration as stored by the server.

| Field        | Type   | Description                          |
|--------------|--------|--------------------------------------|
| `proxy_name` | string | Proxy name.                          |
| `proxy_type` | string | Proxy type (e.g. `tcp`, `socks5_relay`). |
| `config`     | object | Full proxy config as JSON.           |

## Error Handling

| Scenario               | HTTP Status | Message                               |
|------------------------|-------------|---------------------------------------|
| Client key not found   | 404         | `client {key} not found`              |
| Client offline         | 404         | `client {key} is offline`             |
| Do() timeout (10s)     | 504         | `timeout waiting for client response` |
| Proxy not found        | 404         | `proxy {name} not found`              |

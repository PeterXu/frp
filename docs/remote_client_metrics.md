# Remote Client Metrics

frps operators can view a connected frpc's runtime resource metrics (CPU, memory, goroutines, GC) through the frps dashboard API.

## API Endpoint

```
GET  /api/clients/{key}/metrics
```

The endpoint is behind the frps dashboard auth middleware.

`{key}` is the composite client key, same one used by `/api/clients/{key}`.

## Example

```bash
# View frpc resource metrics
curl http://frps:7500/api/clients/myclient/metrics
```

## Control Flow

```
  frps API Handler            server.Control           [control conn]          client.Control
       |                           |                         |                       |
  1. lookupClientControl(key)     |                         |                       |
     +- clientRegistry.GetByKey   |                         |                       |
        -> ctlManager.GetByID     |                         |                       |
             |                    |                         |                       |
  2. ctl.SupportsFeature() -------+                         |                       |
     (check "metrics" feature)    |                         |                       |
             |                    |                         |                       |
  3. ctl.MsgTransporter().Do()  --+--- ReqClientMetrics --->|---------------------->|
     (3s timeout, waits by       |                         |    4. handler reads    |
      txID + responseType)       |                         |       runtime.MemStats |
             |                    |                         |       + getCPUSeconds()|
             |                    |<--- ClientMetricsResp---|<----------------------|
  5. response dispatched          |    6. dispatcher routes |   5. handler sends    |
     via DispatchWithType()       |       by txID match     |       response back   |
             |                    |                         |                       |
  6. strip txID, return JSON      |                         |                       |
```

**Key steps:**

1. **Lookup** -- `lookupClientControl()` resolves `{key}` to `ClientInfo` to `RunID` to `*Control`.
2. **Feature check** -- `SupportsFeature("metrics")` verifies the frpc version supports metrics. Older clients return `false` and receive a 400 error immediately instead of waiting for a timeout.
3. **Send request** -- `MessageTransporter.Do()` sends the message over the control connection and registers a wait channel keyed by `transactionID` + `responseType`.
4. **Client collects** -- The client handler reads `runtime.MemStats()` and calls `getCPUSeconds()` to collect current metrics.
5. **Response flows back** -- The server dispatcher receives the response and calls `DispatchWithType()`, which matches the transaction ID and delivers the response to the waiting `Do()` call.
6. **API returns** -- The server strips the internal `transaction_id` field and returns JSON to the caller.

## Response Fields

### Client Metrics (`GET /api/clients/{key}/metrics`)

| Field            | Type    | Description                                                  |
|------------------|---------|--------------------------------------------------------------|
| `cpu_usage`      | float64 | Cumulative CPU seconds consumed by the frpc process.        |
| `mem_alloc`      | uint64  | Bytes of allocated heap objects.                            |
| `mem_sys`        | uint64  | Bytes of memory obtained from OS.                           |
| `num_gc`         | uint32  | Number of completed garbage collection cycles.              |
| `num_goroutine`  | int     | Number of active goroutines.                                |

The internal `transaction_id` field is stripped before returning to the caller.

## Metrics Collection

**CPU:** Cumulative CPU seconds via `syscall.Getrusage(RUSAGE_SELF)` on Unix platforms. Includes both user time (`Utime`) and system time (`Stime`). On Windows, returns 0.

**Memory:** Collected via `runtime.ReadMemStats()`:
- `Alloc` — bytes of allocated heap objects
- `Sys` — bytes of memory obtained from OS

**GC:** Number of completed GC cycles from `runtime.ReadMemStats().NumGC`.

**Goroutines:** Current count via `runtime.NumGoroutine()`.

## Feature Advertisement

frpc advertises supported features during login via the `SupportedFeatures` field in the `Login` message:

```go
loginMsg.SupportedFeatures = []string{
    msg.FeatureConfig,   // "config"
    msg.FeatureMetrics,  // "metrics"
    msg.FeatureExit,     // "exit"
}
```

frps uses `Control.SupportsFeature()` to check before sending metrics requests, avoiding timeouts with older clients that don't recognize the message type.

## Dashboard Integration

The ClientDetail page displays metrics in a **Resources** section with:

- **Load metrics** button to initiate collection
- Auto-refresh every 5 seconds after initial load
- **Stop** button to halt polling
- Human-readable memory formatting (KB/MB/GB)
- CPU time displayed in seconds with 1 decimal place

## Error Handling

| Scenario                       | HTTP Status | Message                                     |
|--------------------------------|-------------|---------------------------------------------|
| Client key not found           | 404         | `client {key} not found`                    |
| Client offline                 | 404         | `client {key} is offline`                   |
| Client version too old         | 400         | `client version does not support metrics`   |
| Do() timeout (3s)              | 504         | `timeout waiting for client response`       |
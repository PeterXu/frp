# Remote Client Exit

frps operators can remotely command a connected frpc to exit gracefully through the frps dashboard API. frpc exits with code 0; the system service manager (systemd, supervisord, etc.) handles restart if desired.

## API Endpoint

```
PUT  /api/clients/{key}/exit
```

The endpoint is behind the frps dashboard auth middleware.

`{key}` is the composite client key, same one used by `/api/clients/{key}`.

## Example

```bash
# Tell frpc to exit gracefully
curl -X PUT http://frps:7500/api/clients/myclient/exit
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
     (check "exit" feature)       |                         |                       |
             |                    |                         |                       |
  3. ctl.MsgTransporter().Do()  --+--- ReqClientExit ----->|---------------------->|
     (3s timeout, waits by       |                         |    4. atomic guard:    |
      txID + responseType)       |                         |       first wins       |
             |                    |                         |    5. send exit resp   |
             |                    |<--- ClientExitResp -----|<----------------------|
  6. response dispatched          |    7. dispatcher routes |   8. closeSession()   |
     via DispatchWithType()       |       by txID match     |      <- doneCh         |
             |                    |                         |      os.Exit(0)        |
  9. return {"status":"ok"}       |                         |                       |
```

**Key steps:**

1. **Lookup** -- `lookupClientControl()` resolves `{key}` to `ClientInfo` to `RunID` to `*Control`.
2. **Feature check** -- `SupportsFeature("exit")` verifies the frpc version supports remote exit. Older clients return `false` and receive a 400 error immediately instead of waiting for a timeout.
3. **Send request** -- `MessageTransporter.Do()` sends the message over the control connection and registers a wait channel keyed by `transactionID` + `responseType`.
4. **Atomic guard** -- `exitInProgress.CompareAndSwap` ensures only one exit proceeds; duplicate requests are logged and dropped.
5. **Client acknowledges** -- The client sends `ClientExitResp` back before shutting down.
6. **Response flows back** -- The server dispatcher receives the response and delivers it to the waiting `Do()` call.
7. **API returns** -- The server returns `{"status":"ok"}` to the caller.
8. **Client exits** -- After the response is flushed, the client closes the control connection, waits for `worker()` to clean up proxies and visitors, then calls `os.Exit(0)`.

## Exit Behavior

| Aspect             | Behavior                                                    |
|--------------------|-------------------------------------------------------------|
| Exit code          | 0 (graceful, intentional)                                   |
| Restart            | Not handled by frpc; system service manager responsibility  |
| Graceful shutdown  | Closes proxies, visitors, control connection                |
| Duplicate requests | Ignored with a warning log; only the first request runs     |
| Logging            | Both frpc and frps log the exit event                       |
| Unsupported client | frps returns HTTP 400 with clear error message              |

## Feature Advertisement

frpc advertises the `exit` feature during login via the `SupportedFeatures` field in the `Login` message:

```go
loginMsg.SupportedFeatures = []string{
    msg.FeatureConfig,   // "config"
    msg.FeatureMetrics,  // "metrics"
    msg.FeatureExit,     // "exit"
}
```

frps uses `Control.SupportsFeature()` to check before sending exit requests, avoiding timeouts with older clients that don't recognize the message type.

## Error Handling

| Scenario                       | HTTP Status | Message                                           |
|--------------------------------|-------------|---------------------------------------------------|
| Client key not found           | 404         | `client {key} not found`                          |
| Client offline                 | 404         | `client {key} is offline`                         |
| Client version too old         | 400         | `client version does not support remote exit`     |
| Do() timeout (3s)              | 504         | `timeout waiting for client response`             |

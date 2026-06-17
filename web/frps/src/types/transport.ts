// Per-client transport connection counts attributed to a single frpc.
// WorkInUse attribution is best-effort by clientID (fallback runID/user).
export interface TransportClientRow {
  key: string
  user?: string
  clientID?: string
  runID?: string
  ip?: string
  version?: string
  wireProtocol?: string
  online: boolean
  control: number
  workIdle: number
  workInUse: number
  socks5: number
  connectedAt: number
}

// /api/transport/stats — frpc<->frps connection counts broken down by type.
export interface TransportStatsResp {
  control: number // login (control) connections = online clients
  workInUse: number // currently relaying a proxied connection
  workIdle: number // pooled work connections buffered for reuse
  visitorListeners: number // registered visitor endpoints (not active streams)
  socks5: number // active SOCKS5/HTTP-CONNECT relay connections
  clients: TransportClientRow[]
}

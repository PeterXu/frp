export interface ClientInfoData {
  key: string
  user: string
  clientID: string
  runID: string
  version?: string
  wireProtocol?: string
  hostname: string
  clientIP?: string
  metas?: Record<string, string>
  firstConnectedAt: number
  lastConnectedAt: number
  disconnectedAt?: number
  online: boolean
}

export interface ClientMetricsData {
  cpu_usage: number
  mem_alloc: number
  mem_sys: number
  num_gc: number
  num_goroutine: number
}

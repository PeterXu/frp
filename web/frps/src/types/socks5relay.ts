export interface Socks5RelayGroupMember {
  runID: string
  proxyName: string
  online: boolean
}

export interface Socks5RelayGroupInfo {
  name: string
  members: Socks5RelayGroupMember[]
}

export interface Socks5RelaySessionInfo {
  username: string
  runID: string
}

export interface RelayConnectionInfo {
  id: string
  sourceIP: string
  protocol: 'socks5' | 'http_connect'
  group: string
  dstAddr: string
  dstPort: number
  proxyName: string
  runID: string
  startTime: number
  bytesIn: number
  bytesOut: number
}

export interface RelayConnectionStats {
  totalConnections: number
  totalBytesIn: number
  totalBytesOut: number
}

export type RelayEventType = 'created' | 'updated' | 'deleted' | 'connected'

export interface RelayConnectionEvent {
  type: RelayEventType
  conn: RelayConnectionInfo
}

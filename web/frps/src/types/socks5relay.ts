export interface Socks5RelayGroupMember {
  runID: string
  proxyName: string
  online: boolean
  disabled: boolean
  key: string
}

export interface Socks5RelayGroupInfo {
  name: string
  members: Socks5RelayGroupMember[]
  disabled: boolean
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
  userID?: string
  dstAddr: string
  dstPort: number
  proxyName: string
  runID: string
  startTime: number
  endTime?: number   // unix timestamp, undefined for active connections
  bytesIn: number
  bytesOut: number
  isActive: boolean  // true for active connections, false for closed
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

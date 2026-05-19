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

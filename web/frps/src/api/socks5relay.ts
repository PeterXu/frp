import { http } from './http'
import type {
  Socks5RelayGroupInfo,
  Socks5RelaySessionInfo,
  RelayConnectionInfo,
  RelayConnectionStats,
} from '../types/socks5relay'

export const getSocks5RelayGroups = () => {
  return http.get<Socks5RelayGroupInfo[]>('../api/socks5relay/groups')
}

export const getSocks5RelaySessions = () => {
  return http.get<Socks5RelaySessionInfo[]>('../api/socks5relay/sessions')
}

export const getSocks5RelayConnections = () => {
  return http.get<RelayConnectionInfo[]>('../api/socks5relay/connections')
}

export const getSocks5RelayStats = () => {
  return http.get<RelayConnectionStats>('../api/socks5relay/stats')
}

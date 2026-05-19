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

export interface RetentionSetting {
  retentionSeconds: number
}

export const getSocks5RelayRetention = () => {
  return http.get<RetentionSetting>('../api/socks5relay/retention')
}

export const setSocks5RelayRetention = (seconds: number) => {
  return http.put<RetentionSetting>('../api/socks5relay/retention', { retentionSeconds: seconds })
}

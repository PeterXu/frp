import { http } from './http'
import type { Socks5RelayGroupInfo, Socks5RelaySessionInfo } from '../types/socks5relay'

export const getSocks5RelayGroups = () => {
  return http.get<Socks5RelayGroupInfo[]>('../api/socks5relay/groups')
}

export const getSocks5RelaySessions = () => {
  return http.get<Socks5RelaySessionInfo[]>('../api/socks5relay/sessions')
}

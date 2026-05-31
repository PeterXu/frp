import { http } from './http'
import type { ClientInfoData } from '../types/client'

export const getClients = () => {
  return http.get<ClientInfoData[]>('../api/clients')
}

export const getClient = (key: string) => {
  return http.get<ClientInfoData>(`../api/clients/${key}`)
}

export const getClientConfig = (key: string) => {
  return http.get<any>(`../api/clients/${key}/config`)
}

export const getProxyConfig = (key: string, name: string) => {
  return http.get<any>(`../api/clients/${key}/proxies/${name}/config`)
}

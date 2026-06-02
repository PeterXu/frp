import { http } from './http'
import type { ClientInfoData, ClientMetricsData } from '../types/client'

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

export const getClientMetrics = (key: string) => {
  return http.get<ClientMetricsData>(`../api/clients/${key}/metrics`)
}

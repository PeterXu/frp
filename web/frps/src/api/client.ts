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

export const getClientMetrics = (key: string) => {
  return http.get<ClientMetricsData>(`../api/clients/${key}/metrics`)
}

export const exitClient = (key: string) => {
  return http.put<{ status: string }>(`../api/clients/${key}/exit`)
}

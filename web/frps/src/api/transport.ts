import { http } from './http'
import type { TransportStatsResp } from '../types/transport'

export const getTransportStats = () => {
  return http.get<TransportStatsResp>('../api/transport/stats')
}

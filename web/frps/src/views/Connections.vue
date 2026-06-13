<template>
  <div class="connections-page">
    <div class="page-header">
      <div class="header-top">
        <div class="title-section">
          <h1 class="page-title">Connections</h1>
          <p class="page-subtitle">Active and recent SOCKS5/HTTP CONNECT relay connections</p>
        </div>

        <div class="actions-section">
          <el-tag v-if="autoRefresh" type="success" size="small">Live</el-tag>
          <ActionButton variant="outline" size="small" @click="fetchData">
            Refresh
          </ActionButton>
        </div>
      </div>
    </div>

    <!-- Stats Cards -->
    <div class="section">
      <el-row :gutter="16">
        <el-col :xs="24" :sm="6">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">Active Connections</div>
              <div class="status-value">{{ connectionStats.activeCount }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="6">
          <el-card class="status-card clickable" shadow="hover" @click="showRetentionDialog = true">
            <div class="status-item">
              <div class="status-label">Recent ({{ formatRetention(retentionSeconds) }})</div>
              <div class="status-value">{{ connectionStats.recentCount }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="6">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">Total Bytes In</div>
              <div class="status-value">{{ formatFileSize(connectionStats.totalBytesIn) }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="6">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">Total Bytes Out</div>
              <div class="status-value">{{ formatFileSize(connectionStats.totalBytesOut) }}</div>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </div>

    <!-- Connections Tabs -->
    <div class="section">
      <el-card v-loading="loading" shadow="hover">
        <el-tabs v-model="activeTab">
          <el-tab-pane label="Active Connections" name="active">
            <div v-if="activeConnections.length > 0">
              <el-table :data="paginatedActiveConnections">
                <el-table-column prop="sourceIP" label="Source" width="180" />
                <el-table-column prop="protocol" label="Protocol" width="130">
                  <template #default="{ row }">
                    <el-tag :type="protocolTagType(row.protocol, true)" size="small">
                      {{ protocolLabel(row.protocol) }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="group" label="Group" width="120" />
                <el-table-column prop="userID" label="UserID" width="120">
                  <template #default="{ row }">
                    <span v-if="row.userID">{{ row.userID }}</span>
                    <span v-else class="text-muted">-</span>
                  </template>
                </el-table-column>
                <el-table-column label="Destination" width="200">
                  <template #default="{ row }">
                    {{ formatDestination(row.dstAddr, row.dstPort) }}
                  </template>
                </el-table-column>
                <el-table-column prop="proxyName" label="Proxy" width="150" />
                <el-table-column label="Duration" width="120">
                  <template #default="{ row }">
                    {{ formatDuration(row.startTime, null) }}
                  </template>
                </el-table-column>
                <el-table-column label="Traffic">
                  <template #default="{ row }">
                    ↓ {{ formatFileSize(row.bytesIn) }} / ↑ {{ formatFileSize(row.bytesOut) }}
                  </template>
                </el-table-column>
              </el-table>
              <div class="pagination-container">
                <el-pagination
                  v-model:current-page="activeCurrentPage"
                  v-model:page-size="pageSize"
                  :page-sizes="[20, 50, 100, 200]"
                  :total="activeConnections.length"
                  layout="total, sizes, prev, pager, next"
                  background
                  small
                />
              </div>
            </div>
            <div v-else-if="!loading">
              <el-empty description="No active connections" />
            </div>
          </el-tab-pane>

          <el-tab-pane name="recent">
            <template #label>
              Recent Connections <el-badge v-if="recentConnections.length > 0" :value="recentConnections.length" />
            </template>
            <div v-if="recentConnections.length > 0">
              <el-table :data="paginatedRecentConnections">
                <el-table-column prop="sourceIP" label="Source" width="180" />
                <el-table-column prop="protocol" label="Protocol" width="130">
                  <template #default="{ row }">
                    <el-tag :type="protocolTagType(row.protocol, false)" size="small">
                      {{ protocolLabel(row.protocol) }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="group" label="Group" width="120" />
                <el-table-column prop="userID" label="UserID" width="120">
                  <template #default="{ row }">
                    <span v-if="row.userID">{{ row.userID }}</span>
                    <span v-else class="text-muted">-</span>
                  </template>
                </el-table-column>
                <el-table-column label="Destination" width="200">
                  <template #default="{ row }">
                    {{ formatDestination(row.dstAddr, row.dstPort) }}
                  </template>
                </el-table-column>
                <el-table-column prop="proxyName" label="Proxy" width="150" />
                <el-table-column label="Duration" width="150">
                  <template #default="{ row }">
                    {{ formatDuration(row.startTime, row.endTime) }}
                  </template>
                </el-table-column>
                <el-table-column label="Closed" width="120">
                  <template #default="{ row }">
                    {{ formatTimeAgo(row.endTime!) }}
                  </template>
                </el-table-column>
                <el-table-column label="Traffic">
                  <template #default="{ row }">
                    ↓ {{ formatFileSize(row.bytesIn) }} / ↑ {{ formatFileSize(row.bytesOut) }}
                  </template>
                </el-table-column>
              </el-table>
              <div class="pagination-container">
                <el-pagination
                  v-model:current-page="recentCurrentPage"
                  v-model:page-size="pageSize"
                  :page-sizes="[20, 50, 100, 200]"
                  :total="recentConnections.length"
                  layout="total, sizes, prev, pager, next"
                  background
                  small
                />
              </div>
            </div>
            <div v-else-if="!loading">
              <el-empty description="No recent connections" />
            </div>
          </el-tab-pane>
        </el-tabs>
      </el-card>
    </div>

    <!-- Retention Settings Dialog -->
    <el-dialog v-model="showRetentionDialog" title="Connection Retention Settings" width="400px">
      <el-form label-width="120px">
        <el-form-item label="Retention Time">
          <el-select v-model="tempRetention" placeholder="Select retention time">
            <el-option label="Disabled (0s)" :value="0" />
            <el-option label="1 minute" :value="60" />
            <el-option label="5 minutes" :value="300" />
            <el-option label="10 minutes (default)" :value="600" />
            <el-option label="15 minutes" :value="900" />
            <el-option label="30 minutes" :value="1800" />
            <el-option label="1 hour" :value="3600" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-text type="info" size="small">
            Closed connections are kept in memory for this duration. Resets to 10min on restart.
          </el-text>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showRetentionDialog = false">Cancel</el-button>
        <el-button type="primary" @click="updateRetention">Apply</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import ActionButton from '@shared/components/ActionButton.vue'
import { getSocks5RelayConnections, getSocks5RelayRetention, setSocks5RelayRetention } from '../api/socks5relay'
import type { RelayConnectionInfo } from '../types/socks5relay'
import { formatFileSize } from '../utils/format'

const allConnections = ref<RelayConnectionInfo[]>([])
const loading = ref(false)
const autoRefresh = ref(true)
const activeTab = ref('active')

// Pagination settings
const pageSize = ref(50)
const activeCurrentPage = ref(1)
const recentCurrentPage = ref(1)

// Connection index map for fast SSE event lookup (id -> array index)
const connectionIndexMap = new Map<string, number>()

// Retention settings
const retentionSeconds = ref(600) // Default 10 minutes
const tempRetention = ref(600)
const showRetentionDialog = ref(false)

// Computed properties for splitting connections
const activeConnections = computed(() => {
  return allConnections.value.filter(c => c.isActive)
})

const recentConnections = computed(() => {
  return allConnections.value.filter(c => !c.isActive)
})

// Paginated connections for display
const paginatedActiveConnections = computed(() => {
  const start = (activeCurrentPage.value - 1) * pageSize.value
  const end = start + pageSize.value
  return activeConnections.value.slice(start, end)
})

const paginatedRecentConnections = computed(() => {
  const start = (recentCurrentPage.value - 1) * pageSize.value
  const end = start + pageSize.value
  return recentConnections.value.slice(start, end)
})

// Stats based on all connections (active + recent)
const connectionStats = computed(() => {
  return {
    activeCount: activeConnections.value.length,
    recentCount: recentConnections.value.length,
    totalBytesIn: allConnections.value.reduce((sum, c) => sum + c.bytesIn, 0),
    totalBytesOut: allConnections.value.reduce((sum, c) => sum + c.bytesOut, 0),
  }
})

const protocolLabel = (protocol: string): string => {
  if (protocol === 'socks5') return 'SOCKS5'
  if (protocol === 'socks5_visitor') return 'SOCKS5 VISITOR'
  return 'HTTP CONNECT'
}

// Tag color palette differs between the active tab (primary/success) and the
// recent tab (info/warning); `active` selects which palette to draw from.
const protocolTagType = (protocol: string, active: boolean): string => {
  if (protocol === 'socks5') return active ? 'primary' : 'info'
  if (protocol === 'socks5_visitor') return active ? 'success' : 'warning'
  return active ? 'success' : 'warning'
}

const formatDuration = (startTime: number, endTime: number | null): string => {
  const start = startTime * 1000
  const end = endTime ? endTime * 1000 : Date.now()
  const diff = end - start
  const seconds = Math.floor(diff / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  return `${hours}h`
}

const formatTimeAgo = (timestamp: number): string => {
  const now = Date.now()
  const diff = now - timestamp * 1000
  const seconds = Math.floor(diff / 1000)

  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

const fetchData = async () => {
  loading.value = true
  try {
    const connections = await getSocks5RelayConnections()
    // Sort once on fetch: active connections first, then closed by endTime descending
    connections.sort((a, b) => {
      if (a.isActive !== b.isActive) {
        return a.isActive ? -1 : 1 // active first
      }
      if (!a.isActive && !b.isActive) {
        // Closed connections: most recently closed first (endTime descending)
        return (b.endTime || 0) - (a.endTime || 0)
      }
      return 0
    })
    allConnections.value = connections
    // Rebuild index map for fast SSE lookup
    connectionIndexMap.clear()
    allConnections.value.forEach((conn, index) => {
      connectionIndexMap.set(conn.id, index)
    })
    await fetchRetention()
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to fetch connections: ' + error.message,
      type: 'error',
    })
  } finally {
    loading.value = false
  }
}

const fetchRetention = async () => {
  try {
    const data = await getSocks5RelayRetention()
    retentionSeconds.value = data.retentionSeconds
    tempRetention.value = data.retentionSeconds
  } catch (error: any) {
    console.error('Failed to fetch retention setting:', error)
  }
}

const formatRetention = (seconds: number): string => {
  if (seconds === 0) return 'disabled'
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}min`
  const hours = Math.floor(minutes / 60)
  return `${hours}h`
}

const formatDestination = (addr: string, port: number): string => {
  // IPv6 addresses contain colons, need to wrap in brackets
  if (addr.includes(':')) {
    return `[${addr}]:${port}`
  }
  return `${addr}:${port}`
}

const updateRetention = async () => {
  try {
    await setSocks5RelayRetention(tempRetention.value)
    retentionSeconds.value = tempRetention.value
    showRetentionDialog.value = false
    ElMessage({
      showClose: true,
      message: 'Retention setting updated',
      type: 'success',
    })
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to update retention: ' + error.message,
      type: 'error',
    })
  }
}

// Handle SSE connection events using Map for O(1) lookup
const handleConnectionEvent = (event: { type: string; conn: RelayConnectionInfo }) => {
  const conn = event.conn

  if (event.type === 'created') {
    const newIndex = allConnections.value.length
    allConnections.value.push(conn)
    connectionIndexMap.set(conn.id, newIndex)
  } else if (event.type === 'updated') {
    const index = connectionIndexMap.get(conn.id)
    if (index !== undefined && index < allConnections.value.length) {
      allConnections.value[index] = conn
    }
  } else if (event.type === 'deleted') {
    const index = connectionIndexMap.get(conn.id)
    if (index !== undefined && index < allConnections.value.length) {
      connectionIndexMap.delete(conn.id)
      allConnections.value.splice(index, 1)

      // Find insert position: front of closed section
      const firstClosedIndex = allConnections.value.findIndex(c => !c.isActive)
      const insertIndex = firstClosedIndex === -1 ? allConnections.value.length : firstClosedIndex
      allConnections.value.splice(insertIndex, 0, conn)
      connectionIndexMap.set(conn.id, insertIndex)

      // Only update indices that shifted: from original removal point to insert point
      const updateStart = Math.min(index, insertIndex)
      const updateEnd = Math.max(index, insertIndex)
      for (let i = updateStart; i <= updateEnd && i < allConnections.value.length; i++) {
        connectionIndexMap.set(allConnections.value[i].id, i)
      }
    }
  }
}

let eventSource: EventSource | null = null
let refreshInterval: ReturnType<typeof setInterval> | null = null

onMounted(async () => {
  await fetchData()

  // Set up SSE connection
  const eventsUrl = '../api/socks5relay/events'
  eventSource = new EventSource(eventsUrl)

  eventSource.addEventListener('connected', () => {
    console.log('SSE connected')
  })

  eventSource.addEventListener('created', (e) => {
    try {
      const event = JSON.parse(e.data)
      handleConnectionEvent({ ...event, type: 'created' })
    } catch (err) {
      console.error('Failed to parse created event:', err)
    }
  })

  eventSource.addEventListener('updated', (e) => {
    try {
      const event = JSON.parse(e.data)
      handleConnectionEvent({ ...event, type: 'updated' })
    } catch (err) {
      console.error('Failed to parse updated event:', err)
    }
  })

  eventSource.addEventListener('deleted', (e) => {
    try {
      const event = JSON.parse(e.data)
      handleConnectionEvent({ ...event, type: 'deleted' })
    } catch (err) {
      console.error('Failed to parse deleted event:', err)
    }
  })

  eventSource.onerror = (err) => {
    console.error('SSE error:', err)
  }

  // Periodic refresh every 2 minutes as fallback for SSE connection issues
  refreshInterval = setInterval(fetchData, 120000)
})

onUnmounted(() => {
  if (eventSource) {
    eventSource.close()
  }
  if (refreshInterval) {
    clearInterval(refreshInterval)
  }
})
</script>

<style scoped>
.connections-page {
  display: flex;
  flex-direction: column;
  gap: 32px;
}

.page-header {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.header-top {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 20px;
}

.title-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.page-title {
  font-size: 28px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0;
  line-height: 1.2;
}

.page-subtitle {
  font-size: 14px;
  color: var(--el-text-color-secondary);
  margin: 0;
}

.actions-section {
  display: flex;
  gap: 12px;
  align-items: center;
}

.section {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.status-card {
  border-radius: 12px;
  border: 1px solid #e4e7ed;
}

html.dark .status-card {
  border-color: #3a3d5c;
  background: #27293d;
}

.status-item {
  padding: 8px 0;
}

.status-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
  font-weight: 500;
}

.status-value {
  font-size: 24px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.clickable {
  cursor: pointer;
  transition: transform 0.1s;
}

.clickable:hover {
  transform: translateY(-2px);
}

.clickable:active {
  transform: translateY(0);
}

.pagination-container {
  display: flex;
  justify-content: flex-end;
  padding: 16px 0 0;
}

@media (max-width: 768px) {
  .header-top {
    flex-direction: column;
  }

  .actions-section {
    width: 100%;
    flex-direction: column;
  }
}

.text-muted {
  color: var(--el-text-color-secondary);
}
</style>

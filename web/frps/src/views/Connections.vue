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
              <div class="status-value">{{ activeConnections.length }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="6">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">Recent (10min)</div>
              <div class="status-value">{{ recentConnections.length }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="6">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">Total Bytes In</div>
              <div class="status-value">{{ formatFileSize(activeStats.totalBytesIn) }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="6">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">Total Bytes Out</div>
              <div class="status-value">{{ formatFileSize(activeStats.totalBytesOut) }}</div>
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
              <el-table :data="activeConnections">
                <el-table-column prop="sourceIP" label="Source" width="180" />
                <el-table-column prop="protocol" label="Protocol" width="130">
                  <template #default="{ row }">
                    <el-tag :type="row.protocol === 'socks5' ? 'primary' : 'success'" size="small">
                      {{ row.protocol === 'socks5' ? 'SOCKS5' : 'HTTP CONNECT' }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="group" label="Group" width="120" />
                <el-table-column label="Destination" width="200">
                  <template #default="{ row }">
                    {{ row.dstAddr }}:{{ row.dstPort }}
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
              <el-table :data="recentConnections">
                <el-table-column prop="sourceIP" label="Source" width="180" />
                <el-table-column prop="protocol" label="Protocol" width="130">
                  <template #default="{ row }">
                    <el-tag :type="row.protocol === 'socks5' ? 'info' : 'warning'" size="small">
                      {{ row.protocol === 'socks5' ? 'SOCKS5' : 'HTTP CONNECT' }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="group" label="Group" width="120" />
                <el-table-column label="Destination" width="200">
                  <template #default="{ row }">
                    {{ row.dstAddr }}:{{ row.dstPort }}
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
            </div>
            <div v-else-if="!loading">
              <el-empty description="No recent connections" />
            </div>
          </el-tab-pane>
        </el-tabs>
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import ActionButton from '@shared/components/ActionButton.vue'
import { getSocks5RelayConnections } from '../api/socks5relay'
import type { RelayConnectionInfo } from '../types/socks5relay'
import { formatFileSize } from '../utils/format'

const allConnections = ref<RelayConnectionInfo[]>([])
const loading = ref(false)
const autoRefresh = ref(true)
const activeTab = ref('active')

// Computed properties for splitting connections
const activeConnections = computed(() => {
  return allConnections.value.filter(c => c.isActive)
})

const recentConnections = computed(() => {
  return allConnections.value
    .filter(c => !c.isActive)
    .sort((a, b) => (b.endTime || 0) - (a.endTime || 0))
})

// Stats based on active connections only
const activeStats = computed(() => {
  return {
    totalConnections: activeConnections.value.length,
    totalBytesIn: activeConnections.value.reduce((sum, c) => sum + c.bytesIn, 0),
    totalBytesOut: activeConnections.value.reduce((sum, c) => sum + c.bytesOut, 0),
  }
})

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
    allConnections.value = await getSocks5RelayConnections()
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

// Handle SSE connection events
const handleConnectionEvent = (event: any) => {
  const conn = event.conn

  if (event.type === 'created') {
    // Add new connection
    const index = allConnections.value.findIndex(c => c.id === conn.id)
    if (index === -1) {
      allConnections.value.push(conn)
    }
  } else if (event.type === 'updated') {
    // Update existing connection
    const index = allConnections.value.findIndex(c => c.id === conn.id)
    if (index !== -1) {
      allConnections.value[index] = conn
    }
  } else if (event.type === 'deleted') {
    // Move connection from active to recent
    const index = allConnections.value.findIndex(c => c.id === conn.id)
    if (index !== -1) {
      allConnections.value[index] = conn
    }
  }
}

let eventSource: EventSource | null = null

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

  // Periodic refresh every 30 seconds as fallback
  setInterval(fetchData, 30000)
})

onUnmounted(() => {
  if (eventSource) {
    eventSource.close()
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

@media (max-width: 768px) {
  .header-top {
    flex-direction: column;
  }

  .actions-section {
    width: 100%;
    flex-direction: column;
  }
}
</style>

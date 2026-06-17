<template>
  <div class="transport-page">
    <div class="page-header">
      <div class="header-top">
        <div class="title-section">
          <h1 class="page-title">Transport Connections</h1>
          <p class="page-subtitle">
            Live frpc&#8203;&harr;frps connections by type (control / work / visitor / socks5)
          </p>
        </div>

        <div class="actions-section">
          <el-tag v-if="autoRefresh" type="success" size="small">Live</el-tag>
          <ActionButton variant="outline" size="small" @click="fetchData">
            Refresh
          </ActionButton>
        </div>
      </div>
    </div>

    <!-- Summary tiles -->
    <div class="section">
      <el-row :gutter="16">
        <el-col v-for="card in statCards" :key="card.key" :xs="12" :sm="8" :md="4" :lg="4">
          <el-card class="stat-tile" shadow="hover">
            <div class="stat-tile-content">
              <div class="stat-icon" :class="`icon-${card.color}`">
                <el-icon><component :is="card.icon" /></el-icon>
              </div>
              <div class="stat-info">
                <div class="stat-value">{{ card.value }}</div>
                <div class="stat-label">{{ card.label }}</div>
              </div>
            </div>
            <div class="stat-subtitle">{{ card.subtitle }}</div>
          </el-card>
        </el-col>
      </el-row>
      <el-text type="info" size="small" class="legend">
        "Visitor" counts registered visitor endpoints (stcp/xtcp/sudp), not active streams.
        "Work" = frpc&#8203;&harr;frps data tunnels (in-use relaying + idle pooled).
      </el-text>
    </div>

    <!-- Per-client breakdown -->
    <div class="section">
      <el-card v-loading="loading" shadow="hover">
        <template #header>
          <div class="card-header">
            <span class="card-title">Per-Client Breakdown</span>
            <el-tag size="small" type="info">{{ sortedClients.length }} clients</el-tag>
          </div>
        </template>

        <div v-if="sortedClients.length > 0">
          <el-table :data="sortedClients" size="small">
            <el-table-column label="Client" min-width="200">
              <template #default="{ row }">
                <div class="client-cell">
                  <span class="client-name">{{ clientName(row) }}</span>
                  <span v-if="row.user" class="client-user">{{ row.user }}</span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="Status" width="110">
              <template #default="{ row }">
                <el-tag :type="row.online ? 'success' : 'info'" size="small">
                  {{ row.online ? 'Online' : 'Offline' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="ip" label="IP" width="160">
              <template #default="{ row }">
                <span v-if="row.ip">{{ row.ip }}</span>
                <span v-else class="text-muted">-</span>
              </template>
            </el-table-column>
            <el-table-column prop="version" label="Version" width="120">
              <template #default="{ row }">
                <span v-if="row.version">{{ row.version }}</span>
                <span v-else class="text-muted">-</span>
              </template>
            </el-table-column>
            <el-table-column prop="wireProtocol" label="Wire" width="90">
              <template #default="{ row }">
                <span v-if="row.wireProtocol">{{ row.wireProtocol }}</span>
                <span v-else class="text-muted">-</span>
              </template>
            </el-table-column>
            <el-table-column prop="control" label="Control" width="90" align="right" />
            <el-table-column label="Work" width="120" align="right">
              <template #default="{ row }">
                <span>{{ row.workInUse }} in-use / {{ row.workIdle }} idle</span>
              </template>
            </el-table-column>
            <el-table-column prop="socks5" label="SOCKS5" width="90" align="right" />
          </el-table>
        </div>
        <div v-else-if="!loading">
          <el-empty description="No clients" />
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import {
  User,
  Connection,
  DataAnalysis,
  Share,
  Promotion,
} from '@element-plus/icons-vue'
import ActionButton from '@shared/components/ActionButton.vue'
import { getTransportStats } from '../api/transport'
import type { TransportStatsResp, TransportClientRow } from '../types/transport'

const stats = ref<TransportStatsResp>({
  control: 0,
  workInUse: 0,
  workIdle: 0,
  visitorListeners: 0,
  socks5: 0,
  clients: [],
})
const loading = ref(false)
const autoRefresh = ref(true)

let refreshTimer: number | null = null

const statCards = computed(() => [
  { key: 'control', label: 'Control', value: stats.value.control, subtitle: 'Login connections', color: 'purple', icon: User },
  { key: 'workInUse', label: 'Work In-Use', value: stats.value.workInUse, subtitle: 'Active proxy tunnels', color: 'blue', icon: DataAnalysis },
  { key: 'workIdle', label: 'Work Idle', value: stats.value.workIdle, subtitle: 'Pooled work conns', color: 'cyan', icon: Connection },
  { key: 'visitor', label: 'Visitor', value: stats.value.visitorListeners, subtitle: 'stcp/xtcp/sudp endpoints', color: 'pink', icon: Share },
  { key: 'socks5', label: 'SOCKS5', value: stats.value.socks5, subtitle: 'Active relay conns', color: 'green', icon: Promotion },
])

// Online clients first, then by name.
const sortedClients = computed(() => {
  return [...stats.value.clients].sort((a, b) => {
    if (a.online !== b.online) {
      return a.online ? -1 : 1
    }
    return clientName(a).localeCompare(clientName(b))
  })
})

const clientName = (row: TransportClientRow): string => {
  return row.clientID || row.runID || row.key
}

const fetchData = async () => {
  loading.value = true
  try {
    stats.value = await getTransportStats()
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to fetch transport stats: ' + error.message,
      type: 'error',
    })
  } finally {
    loading.value = false
  }
}

const startAutoRefresh = () => {
  refreshTimer = window.setInterval(() => {
    fetchData()
  }, 5000)
}

const stopAutoRefresh = () => {
  if (refreshTimer !== null) {
    window.clearInterval(refreshTimer)
    refreshTimer = null
  }
}

onMounted(() => {
  fetchData()
  startAutoRefresh()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<style scoped>
.transport-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.page-header {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.header-top {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 20px;
  flex-wrap: wrap;
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
  gap: 12px;
}

.legend {
  line-height: 1.5;
}

.stat-tile {
  border-radius: 12px;
  border: 1px solid #e4e7ed;
  margin-bottom: 16px;
}

html.dark .stat-tile {
  border-color: #3a3d5c;
  background: #27293d;
}

.stat-tile-content {
  display: flex;
  align-items: center;
  gap: 14px;
}

.stat-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: white;
}

.stat-icon .el-icon {
  font-size: 24px;
}

.icon-purple {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}
.icon-blue {
  background: linear-gradient(135deg, #4facfe 0%, #00f2fe 100%);
}
.icon-cyan {
  background: linear-gradient(135deg, #00c6ff 0%, #0072ff 100%);
}
.icon-pink {
  background: linear-gradient(135deg, #f093fb 0%, #f5576c 100%);
}
.icon-green {
  background: linear-gradient(135deg, #43e97b 0%, #38f9d7 100%);
}

html.dark .icon-purple {
  background: linear-gradient(135deg, #818cf8 0%, #a78bfa 100%);
}
html.dark .icon-blue {
  background: linear-gradient(135deg, #60a5fa 0%, #3b82f6 100%);
}
html.dark .icon-pink {
  background: linear-gradient(135deg, #fb7185 0%, #f43f5e 100%);
}
html.dark .icon-cyan {
  background: linear-gradient(135deg, #38bdf8 0%, #3b82f6 100%);
}

.stat-info {
  flex: 1;
  min-width: 0;
}

.stat-value {
  font-size: 24px;
  font-weight: 500;
  line-height: 1.2;
  color: var(--el-text-color-primary);
}

.stat-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.stat-subtitle {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid #e4e7ed;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

html.dark .stat-subtitle {
  border-top-color: #3a3d5c;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-title {
  font-size: 16px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.client-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.client-name {
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.client-user {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.text-muted {
  color: var(--el-text-color-secondary);
}
</style>

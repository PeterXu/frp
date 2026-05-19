<template>
  <div class="connections-page">
    <div class="page-header">
      <div class="header-top">
        <div class="title-section">
          <h1 class="page-title">Active Connections</h1>
          <p class="page-subtitle">Real-time SOCKS5/HTTP CONNECT relay connections</p>
        </div>

        <div class="actions-section">
          <el-tag v-if="autoRefresh" type="success" size="small">Auto-refreshing</el-tag>
          <ActionButton variant="outline" size="small" @click="fetchData">
            Refresh
          </ActionButton>
        </div>
      </div>
    </div>

    <!-- Stats Cards -->
    <div class="section">
      <el-row :gutter="16">
        <el-col :xs="24" :sm="8">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">Total Connections</div>
              <div class="status-value">{{ stats.totalConnections }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="8">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">Total Bytes In</div>
              <div class="status-value">{{ formatFileSize(stats.totalBytesIn) }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="8">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">Total Bytes Out</div>
              <div class="status-value">{{ formatFileSize(stats.totalBytesOut) }}</div>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </div>

    <!-- Connections Table -->
    <div class="section">
      <el-card v-loading="loading" shadow="hover">
        <div v-if="connections.length > 0">
          <el-table :data="connections">
            <el-table-column prop="sourceIP" label="Source" width="180" />
            <el-table-column prop="protocol" label="Protocol" width="120">
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
                {{ formatDuration(row.startTime) }}
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
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import ActionButton from '@shared/components/ActionButton.vue'
import { getSocks5RelayConnections, getSocks5RelayStats } from '../api/socks5relay'
import type { RelayConnectionInfo, RelayConnectionStats } from '../types/socks5relay'
import { formatFileSize } from '../utils/format'

const connections = ref<RelayConnectionInfo[]>([])
const stats = ref<RelayConnectionStats>({
  totalConnections: 0,
  totalBytesIn: 0,
  totalBytesOut: 0,
})
const loading = ref(false)
const autoRefresh = ref(true)

const formatDuration = (startTime: number): string => {
  const now = Date.now()
  const diff = now - startTime * 1000
  const seconds = Math.floor(diff / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  return `${hours}h`
}

const fetchStats = async () => {
  try {
    stats.value = await getSocks5RelayStats()
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to fetch stats: ' + error.message,
      type: 'error',
    })
  }
}

const fetchConnections = async () => {
  loading.value = true
  try {
    connections.value = await getSocks5RelayConnections()
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

const fetchData = async () => {
  await Promise.all([fetchStats(), fetchConnections()])
}

let refreshTimer: number | null = null

onMounted(() => {
  fetchData()
  refreshTimer = window.setInterval(() => {
    fetchData()
  }, 3000) // 3 second refresh
})

onUnmounted(() => {
  if (refreshTimer !== null) {
    window.clearInterval(refreshTimer)
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

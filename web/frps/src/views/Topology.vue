<template>
  <div class="topology-page">
    <div class="page-header">
      <div class="header-top">
        <div class="title-section">
          <h1 class="page-title">Topology</h1>
          <p class="page-subtitle">Visual architecture of SOCKS5/HTTP CONNECT relay</p>
        </div>

        <div class="actions-section">
          <el-tag v-if="autoRefresh" type="success" size="small">Auto-refreshing</el-tag>
          <ActionButton variant="outline" size="small" @click="fetchData">
            Refresh
          </ActionButton>
        </div>
      </div>
    </div>

    <!-- Topology Diagram -->
    <el-card v-loading="loading" shadow="hover" class="topology-card">
      <div class="topology-container">
        <!-- External Clients Column -->
        <div class="topology-column">
          <h3 class="column-title">External Clients</h3>
          <div class="column-content">
            <div class="info-item">
              <span class="info-label">SOCKS5 Port</span>
              <span v-if="serverInfo.socks5ProxyPort > 0" class="info-value">
                :{{ serverInfo.socks5ProxyPort }}
              </span>
              <el-tag v-else size="small" type="info">Not configured</el-tag>
            </div>
            <div class="info-item">
              <span class="info-label">HTTP CONNECT Port</span>
              <span v-if="serverInfo.httpConnectProxyPort > 0" class="info-value">
                :{{ serverInfo.httpConnectProxyPort }}
              </span>
              <el-tag v-else size="small" type="info">Not configured</el-tag>
            </div>
            <div class="info-item">
              <span class="info-label">Active Connections</span>
              <span class="info-value">{{ relayStats.totalConnections }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">Traffic</span>
              <span class="info-value">{{ formatFileSize(relayStats.totalBytesIn + relayStats.totalBytesOut) }}</span>
            </div>
          </div>
        </div>

        <!-- Arrow -->
        <div class="topology-arrow">→</div>

        <!-- frps Server Column -->
        <div class="topology-column center-column">
          <h3 class="column-title">frps Server</h3>
          <div class="column-content">
            <div class="info-item">
              <span class="info-label">Version</span>
              <span class="info-value">v{{ serverInfo.version }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">Bind Port</span>
              <span class="info-value">:{{ serverInfo.bindPort }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">Dashboard</span>
              <span class="info-value">:{{ serverInfo.webServerPort }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">Total Traffic</span>
              <span class="info-value">{{ formatFileSize(serverInfo.totalTrafficIn + serverInfo.totalTrafficOut) }}</span>
            </div>
          </div>
        </div>

        <!-- Arrow -->
        <div class="topology-arrow">→</div>

        <!-- frpc Instances Column -->
        <div class="topology-column">
          <h3 class="column-title">Internal Network (frpc)</h3>
          <div class="column-content">
            <div v-if="groups.length === 0" class="empty-text">No groups configured</div>
            <div v-else>
              <div v-for="group in groups" :key="group.name" class="group-section">
                <div class="group-header">
                  <span class="group-name">{{ group.name }}</span>
                  <el-tag size="small" type="info">
                    {{ group.members.length }} member{{ group.members.length !== 1 ? 's' : '' }}
                    ({{ onlineCount(group.members) }} online)
                  </el-tag>
                </div>
                <div v-for="member in group.members" :key="member.runID" class="member-item">
                  <div class="member-info">
                    <el-tag :type="member.online ? 'success' : 'danger'" size="small">
                      {{ member.online ? 'Online' : 'Offline' }}
                    </el-tag>
                    <span class="member-name">{{ member.proxyName }}</span>
                  </div>
                  <div class="member-runid">{{ member.runID }}</div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import ActionButton from '@shared/components/ActionButton.vue'
import { getServerInfo } from '../api/server'
import { getSocks5RelayGroups, getSocks5RelayStats } from '../api/socks5relay'
import type { Socks5RelayGroupInfo, RelayConnectionStats } from '../types/socks5relay'
import type { ServerInfo } from '../types/server'
import { formatFileSize } from '../utils/format'

const serverInfo = ref<ServerInfo>({
  version: '',
  bindPort: 0,
  vhostHTTPPort: 0,
  vhostHTTPSPort: 0,
  tcpmuxHTTPConnectPort: 0,
  kcpBindPort: 0,
  quicBindPort: 0,
  subdomainHost: '',
  maxPoolCount: 0,
  maxPortsPerClient: 0,
  heartbeatTimeout: 0,
  allowPortsStr: '',
  tlsForce: false,
  totalTrafficIn: 0,
  totalTrafficOut: 0,
  curConns: 0,
  clientCounts: 0,
  proxyTypeCount: {},
  socks5ProxyPort: 0,
  httpConnectProxyPort: 0,
  webServerPort: 0,
})

const groups = ref<Socks5RelayGroupInfo[]>([])
const relayStats = ref<RelayConnectionStats>({
  totalConnections: 0,
  totalBytesIn: 0,
  totalBytesOut: 0,
})
const loading = ref(false)
const autoRefresh = ref(true)

const onlineCount = (members: any[]) => {
  return members.filter((m) => m.online).length
}

const fetchServerInfo = async () => {
  try {
    serverInfo.value = await getServerInfo()
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to fetch server info: ' + error.message,
      type: 'error',
    })
  }
}

const fetchGroups = async () => {
  try {
    groups.value = await getSocks5RelayGroups()
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to fetch groups: ' + error.message,
      type: 'error',
    })
  }
}

const fetchRelayStats = async () => {
  try {
    relayStats.value = await getSocks5RelayStats()
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to fetch relay stats: ' + error.message,
      type: 'error',
    })
  }
}

const fetchData = async () => {
  loading.value = true
  try {
    await Promise.all([fetchServerInfo(), fetchGroups(), fetchRelayStats()])
  } finally {
    loading.value = false
  }
}

let refreshTimer: number | null = null

onMounted(() => {
  fetchData()
  refreshTimer = window.setInterval(() => {
    fetchData()
  }, 5000) // 5 second refresh
})

onUnmounted(() => {
  if (refreshTimer !== null) {
    window.clearInterval(refreshTimer)
  }
})
</script>

<style scoped>
.topology-page {
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

.topology-card {
  border-radius: 12px;
  border: 1px solid #e4e7ed;
}

html.dark .topology-card {
  border-color: #3a3d5c;
  background: #27293d;
}

.topology-container {
  display: flex;
  align-items: stretch;
  gap: 24px;
  padding: 20px 0;
}

.topology-column {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}

.column-title {
  font-size: 16px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  margin: 0;
  text-align: center;
  padding-bottom: 12px;
  border-bottom: 2px solid var(--el-border-color);
}

.center-column .column-title {
  color: #409eff;
}

.column-content {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.info-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: 6px;
}

.info-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.info-value {
  font-size: 14px;
  color: var(--el-text-color-primary);
  font-weight: 500;
}

.topology-arrow {
  display: flex;
  align-items: center;
  font-size: 32px;
  color: var(--el-text-color-secondary);
  padding: 0 8px;
}

.empty-text {
  text-align: center;
  color: var(--el-text-color-secondary);
  padding: 20px;
}

.group-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  background: var(--el-fill-color-light);
  border-radius: 6px;
}

.group-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.group-name {
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.member-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px;
  background: var(--el-bg-color);
  border-radius: 4px;
}

.member-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.member-name {
  font-size: 13px;
  color: var(--el-text-color-primary);
  font-weight: 500;
}

.member-runid {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  font-family: monospace;
}

@media (max-width: 768px) {
  .topology-container {
    flex-direction: column;
  }

  .topology-arrow {
    transform: rotate(90deg);
    justify-content: center;
    padding: 8px 0;
  }

  .header-top {
    flex-direction: column;
  }

  .actions-section {
    width: 100%;
    flex-direction: column;
  }
}
</style>

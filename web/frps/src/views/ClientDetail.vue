<template>
  <div class="client-detail-page">
    <!-- Breadcrumb -->
    <nav class="breadcrumb">
      <a class="breadcrumb-link" @click="goBack">
        <el-icon><ArrowLeft /></el-icon>
      </a>
      <router-link to="/clients" class="breadcrumb-item">Clients</router-link>
      <span class="breadcrumb-separator">/</span>
      <span class="breadcrumb-current">{{
        client?.displayName || route.params.key
      }}</span>
    </nav>

    <div v-loading="loading" class="detail-content">
      <template v-if="client">
        <!-- Header Card -->
        <div class="header-card">
          <div class="header-main">
            <div class="header-left">
              <div class="client-avatar">
                {{ client.displayName.charAt(0).toUpperCase() }}
              </div>
              <div class="client-info">
                <div class="client-name-row">
                  <h1 class="client-name">{{ client.displayName }}</h1>
                  <el-tag v-if="client.version" size="small" type="success"
                    >v{{ client.version }}</el-tag
                  >
                  <el-tag v-if="client.wireProtocolLabel" size="small" type="info">
                    {{ client.wireProtocolLabel }}
                  </el-tag>
                </div>
                <div class="client-meta">
                  <span v-if="client.ip" class="meta-item">{{
                    client.ip
                  }}</span>
                  <span v-if="client.hostname" class="meta-item">{{
                    client.hostname
                  }}</span>
                </div>
              </div>
            </div>
            <div class="header-right">
              <span
                class="status-badge"
                :class="client.online ? 'online' : 'offline'"
              >
                {{ client.online ? 'Online' : 'Offline' }}
              </span>
            </div>
          </div>

          <!-- Info Section -->
          <div class="info-section">
            <div class="info-item">
              <span class="info-label">Connections</span>
              <span class="info-value">{{ totalConnections }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">Run ID</span>
              <span class="info-value">{{ client.runID }}</span>
            </div>
            <div v-if="client.wireProtocol" class="info-item">
              <span class="info-label">Protocol</span>
              <span class="info-value">{{ client.wireProtocol }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">First Connected</span>
              <span class="info-value">{{ client.firstConnectedAgo }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">{{
                client.online ? 'Connected' : 'Disconnected'
              }}</span>
              <span class="info-value">{{
                client.online ? client.lastConnectedAgo : client.disconnectedAgo
              }}</span>
            </div>
          </div>
        </div>

        <!-- Client Config Card -->
        <div v-if="client.online && clientConfig" class="config-card">
          <div class="config-header">
            <div class="config-title">
              <h2>Client Config</h2>
            </div>
          </div>
          <div v-loading="loadingConfig" class="config-body">
            <div class="config-columns">
              <div class="config-column">
                <div class="config-item">
                  <span class="config-label">Version</span>
                  <span class="config-value">{{ clientConfig.version || '-' }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Protocol</span>
                  <span class="config-value">{{ clientConfig.protocol || '-' }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Wire Protocol</span>
                  <span class="config-value">{{ clientConfig.wire_protocol || '-' }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">User</span>
                  <span class="config-value">{{ clientConfig.user || '-' }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Client ID</span>
                  <span class="config-value">{{ clientConfig.client_id || '-' }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Group</span>
                  <span class="config-value">{{ clientConfig.group || '-' }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Server</span>
                  <span class="config-value">
                    {{ clientConfig.server_addr || '-' }}:{{ clientConfig.server_port || '-' }}
                  </span>
                </div>
                <div class="config-item">
                  <span class="config-label">TLS</span>
                  <span class="config-value">{{ boolLabel(clientConfig.tls_enabled) }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">TCP Mux</span>
                  <span class="config-value">{{ boolLabel(clientConfig.tcp_mux) }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Proxy URL</span>
                  <span class="config-value">{{ clientConfig.proxy_url || '-' }}</span>
                </div>
              </div>
              <div class="config-column">
                <div class="config-item">
                  <span class="config-label">Pool Count</span>
                  <span class="config-value">{{ clientConfig.pool_count }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Heartbeat Interval</span>
                  <span class="config-value">{{ clientConfig.heartbeat_interval }}s</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Heartbeat Timeout</span>
                  <span class="config-value">{{ clientConfig.heartbeat_timeout }}s</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Dial Timeout</span>
                  <span class="config-value">{{ clientConfig.dial_server_timeout }}s</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Dial Keepalive</span>
                  <span class="config-value">{{ clientConfig.dial_server_keepalive }}s</span>
                </div>
                <div class="config-item">
                  <span class="config-label">UDP Packet Size</span>
                  <span class="config-value">{{ clientConfig.udp_packet_size }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">DNS Server</span>
                  <span class="config-value">{{ clientConfig.dns_server || '-' }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Log</span>
                  <span class="config-value">{{ clientConfig.log_level }}{{ clientConfig.log_to && clientConfig.log_to !== 'console' ? ' → ' + clientConfig.log_to : '' }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Web Server</span>
                  <span class="config-value">{{ clientConfig.web_server_port ? clientConfig.web_server_addr + ':' + clientConfig.web_server_port : '-' }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">Login Fail Exit</span>
                  <span class="config-value">{{ boolLabel(clientConfig.login_fail_exit) }}</span>
                </div>
              </div>
            </div>
            <div v-if="clientConfig.metadatas && Object.keys(clientConfig.metadatas).length" class="config-section">
              <div class="config-section-title">Metadatas</div>
              <div class="config-columns">
                <div class="config-column">
                  <div v-for="(val, key) in clientConfig.metadatas" :key="key" class="config-item">
                    <span class="config-label">{{ key }}</span>
                    <span class="config-value">{{ val }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div v-if="clientConfig.start && clientConfig.start.length" class="config-section">
              <div class="config-section-title">Start</div>
              <div class="config-tags">
                <el-tag v-for="name in clientConfig.start" :key="name" size="small" class="config-tag">{{ name }}</el-tag>
              </div>
            </div>
          </div>
        </div>

        <!-- Resources Card -->
        <div v-if="client?.online" class="resources-card">
          <div class="config-header">
            <div class="config-title">
              <h2>Resources</h2>
            </div>
            <div class="resources-actions">
              <el-tag v-if="pollingActive" size="small" type="warning">Polling 5s</el-tag>
              <el-button
                v-if="!showMetrics"
                size="small"
                type="primary"
                :loading="loadingMetrics"
                @click="fetchMetrics"
              >
                Load metrics
              </el-button>
              <el-button
                v-else
                size="small"
                @click="stopPolling"
              >
                Stop
              </el-button>
            </div>
          </div>
          <div v-if="showMetrics" class="config-body">
            <div v-loading="loadingMetrics" class="config-columns">
              <div class="config-column">
                <div class="config-item">
                  <span class="config-label">Allocated Memory</span>
                  <span class="config-value">{{ formatBytes(metricsData?.mem_alloc) }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">System Memory</span>
                  <span class="config-value">{{ formatBytes(metricsData?.mem_sys) }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">GC Cycles</span>
                  <span class="config-value">{{ metricsData?.num_gc ?? '-' }}</span>
                </div>
              </div>
              <div class="config-column">
                <div class="config-item">
                  <span class="config-label">Goroutines</span>
                  <span class="config-value">{{ metricsData?.num_goroutine ?? '-' }}</span>
                </div>
                <div class="config-item">
                  <span class="config-label">CPU Time</span>
                  <span class="config-value">{{ formatCPUTime(metricsData?.cpu_usage) }}</span>
                </div>
              </div>
            </div>
          </div>
          <div v-if="metricsError" class="metrics-error">
            {{ metricsError }}
          </div>
        </div>

        <!-- Proxies Card -->
        <div class="proxies-card">
          <div class="proxies-header">
            <div class="proxies-title">
              <h2>Proxies</h2>
              <span class="proxies-count">{{ filteredProxies.length }}</span>
            </div>
            <el-input
              v-model="proxySearch"
              placeholder="Search proxies..."
              :prefix-icon="Search"
              clearable
              class="proxy-search"
            />
          </div>
          <div class="proxies-body">
            <div v-if="proxiesLoading" class="loading-state">
              <el-icon class="is-loading"><Loading /></el-icon>
              <span>Loading...</span>
            </div>
            <div v-else-if="filteredProxies.length > 0" class="proxies-list">
              <div
                v-for="proxy in filteredProxies"
                :key="proxy.name"
                class="proxy-item"
              >
                <ProxyCard :proxy="proxy" show-type />
                <el-button
                  v-if="client?.online"
                  class="proxy-config-btn"
                  size="small"
                  circle
                  @click.stop.prevent="openProxyConfig(proxy)"
                >
                  <el-icon><Setting /></el-icon>
                </el-button>
              </div>
            </div>
            <div v-else-if="clientProxies.length > 0" class="empty-state">
              <p>No proxies match "{{ proxySearch }}"</p>
            </div>
            <div v-else class="empty-state">
              <p>No proxies found</p>
            </div>
          </div>
        </div>
      </template>

      <div v-else-if="!loading" class="not-found">
        <h2>Client not found</h2>
        <p>The client doesn't exist or has been removed.</p>
        <router-link to="/clients">
          <el-button type="primary">Back to Clients</el-button>
        </router-link>
      </div>
    </div>

    <!-- Proxy Config Dialog (view-only) -->
    <BaseDialog
      v-model="showProxyConfigDialog"
      :title="'Proxy Config: ' + proxyConfigName"
      width="600px"
    >
      <div v-loading="proxyConfigLoading">
        <template v-if="proxyConfigData">
          <div class="proxy-config-type">
            <el-tag size="small" type="info">{{ proxyConfigData.proxy_type }}</el-tag>
          </div>
          <div class="proxy-config-fields">
            <div
              v-for="item in proxyConfigFields"
              :key="item.key"
              class="config-item"
            >
              <span class="config-label">{{ item.key }}</span>
              <span class="config-value" :title="item.value">{{ item.value }}</span>
            </div>
          </div>
        </template>
      </div>
      <template #footer>
        <el-button size="small" @click="showProxyConfigDialog = false">Close</el-button>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Loading, Search, Setting } from '@element-plus/icons-vue'
import { Client } from '../utils/client'
import { getClient, getClientConfig, getProxyConfig, getClientMetrics } from '../api/client'
import { getProxiesByType } from '../api/proxy'
import {
  BaseProxy,
  TCPProxy,
  UDPProxy,
  HTTPProxy,
  HTTPSProxy,
  TCPMuxProxy,
  STCPProxy,
  SUDPProxy,
  Socks5RelayProxy,
} from '../utils/proxy'
import { getServerInfo } from '../api/server'
import ProxyCard from '../components/ProxyCard.vue'
import BaseDialog from '@shared/components/BaseDialog.vue'
import type { ClientMetricsData } from '../types/client'

const route = useRoute()
const router = useRouter()
const client = ref<Client | null>(null)
const loading = ref(true)

const boolLabel = (val: boolean | null | undefined) => {
  if (val === null || val === undefined) return '-'
  return val ? 'On' : 'Off'
}

const goBack = () => {
  if (window.history.length > 1) {
    router.back()
  } else {
    router.push('/clients')
  }
}
const proxiesLoading = ref(false)
const allProxies = ref<BaseProxy[]>([])
const proxySearch = ref('')

let serverInfo: {
  vhostHTTPPort: number
  vhostHTTPSPort: number
  tcpmuxHTTPConnectPort: number
  subdomainHost: string
} | null = null

const clientProxies = computed(() => {
  if (!client.value) return []
  return allProxies.value.filter(
    (p) =>
      p.clientID === client.value!.clientID && p.user === client.value!.user,
  )
})

const filteredProxies = computed(() => {
  if (!proxySearch.value) return clientProxies.value
  const search = proxySearch.value.toLowerCase()
  return clientProxies.value.filter(
    (p) =>
      p.name.toLowerCase().includes(search) ||
      p.type.toLowerCase().includes(search),
  )
})

const totalConnections = computed(() => {
  return clientProxies.value.reduce((sum, p) => sum + p.conns, 0)
})

const fetchServerInfo = async () => {
  if (serverInfo) return serverInfo
  const res = await getServerInfo()
  serverInfo = res
  return serverInfo
}

const fetchClient = async () => {
  const key = route.params.key as string
  if (!key) {
    loading.value = false
    return
  }
  try {
    const data = await getClient(key)
    client.value = new Client(data)
  } catch (error: any) {
    ElMessage.error('Failed to fetch client: ' + error.message)
  } finally {
    loading.value = false
  }
}

// Client config state (view-only)
const clientConfig = ref<any>(null)
const loadingConfig = ref(false)

// Resource metrics state
const metricsData = ref<ClientMetricsData | null>(null)
const loadingMetrics = ref(false)
const showMetrics = ref(false)
const pollingActive = ref(false)
const metricsError = ref('')
let pollTimer: ReturnType<typeof setInterval> | null = null

const formatBytes = (bytes: number | undefined): string => {
  if (bytes === undefined || bytes === null) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
  return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB'
}

const formatCPUTime = (seconds: number | undefined): string => {
  if (seconds === undefined || seconds === null) return '-'
  if (seconds < 60) return seconds.toFixed(1) + 's'
  if (seconds < 3600) return (seconds / 60).toFixed(1) + 'm'
  return (seconds / 3600).toFixed(1) + 'h'
}

const fetchMetrics = async () => {
  if (!client.value?.online) return
  if (loadingMetrics.value) return
  loadingMetrics.value = true
  metricsError.value = ''
  try {
    const data = await getClientMetrics(route.params.key as string)
    metricsData.value = data
    showMetrics.value = true
    if (!pollingActive.value) {
      startPolling()
    }
  } catch (error: any) {
    metricsError.value = 'Failed to fetch metrics: ' + (error.message || error)
    stopPolling()
  } finally {
    loadingMetrics.value = false
  }
}

const startPolling = () => {
  stopPolling()
  pollingActive.value = true
  pollTimer = setInterval(fetchMetrics, 5000)
}

const stopPolling = () => {
  pollingActive.value = false
  showMetrics.value = false
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

const fetchClientConfigData = async () => {
  if (!client.value?.online) return
  loadingConfig.value = true
  try {
    clientConfig.value = await getClientConfig(route.params.key as string)
  } catch (error: any) {
    ElMessage.error('Failed to fetch client config: ' + error.message)
  } finally {
    loadingConfig.value = false
  }
}

// Proxy config state (view-only)
const showProxyConfigDialog = ref(false)
const proxyConfigLoading = ref(false)
const proxyConfigData = ref<any>(null)
const proxyConfigName = ref('')

const proxyConfigFields = computed(() => {
  const cfg = proxyConfigData.value?.config
  if (!cfg || typeof cfg !== 'object') return []
  return Object.entries(cfg).map(([key, val]) => ({
    key,
    value: typeof val === 'object' ? JSON.stringify(val) : String(val ?? '-'),
  }))
})

const openProxyConfig = async (proxy: BaseProxy) => {
  proxyConfigName.value = proxy.name
  proxyConfigData.value = null
  showProxyConfigDialog.value = true
  proxyConfigLoading.value = true
  try {
    const resp = await getProxyConfig(route.params.key as string, proxy.name)
    proxyConfigData.value = resp
  } catch (error: any) {
    ElMessage.error('Failed to fetch proxy config: ' + error.message)
  } finally {
    proxyConfigLoading.value = false
  }
}

const fetchProxies = async () => {
  proxiesLoading.value = true
  const proxyTypes = ['tcp', 'udp', 'http', 'https', 'tcpmux', 'stcp', 'sudp', 'socks5_relay']
  const proxies: BaseProxy[] = []
  try {
    const info = await fetchServerInfo()
    for (const type of proxyTypes) {
      try {
        const json = await getProxiesByType(type)
        if (!json.proxies) continue
        if (type === 'tcp') {
          proxies.push(...json.proxies.map((p: any) => new TCPProxy(p)))
        } else if (type === 'udp') {
          proxies.push(...json.proxies.map((p: any) => new UDPProxy(p)))
        } else if (type === 'http' && info?.vhostHTTPPort) {
          proxies.push(
            ...json.proxies.map(
              (p: any) =>
                new HTTPProxy(p, info.vhostHTTPPort, info.subdomainHost),
            ),
          )
        } else if (type === 'https' && info?.vhostHTTPSPort) {
          proxies.push(
            ...json.proxies.map(
              (p: any) =>
                new HTTPSProxy(p, info.vhostHTTPSPort, info.subdomainHost),
            ),
          )
        } else if (type === 'tcpmux' && info?.tcpmuxHTTPConnectPort) {
          proxies.push(
            ...json.proxies.map(
              (p: any) =>
                new TCPMuxProxy(
                  p,
                  info.tcpmuxHTTPConnectPort,
                  info.subdomainHost,
                ),
            ),
          )
        } else if (type === 'stcp') {
          proxies.push(...json.proxies.map((p: any) => new STCPProxy(p)))
        } else if (type === 'sudp') {
          proxies.push(...json.proxies.map((p: any) => new SUDPProxy(p)))
        } else if (type === 'socks5_relay') {
          proxies.push(...json.proxies.map((p: any) => new Socks5RelayProxy(p)))
        }
      } catch {
        // Ignore
      }
    }
    allProxies.value = proxies
  } catch {
    // Ignore
  } finally {
    proxiesLoading.value = false
  }
}

onMounted(async () => {
  await fetchClient()
  fetchProxies()
  fetchClientConfigData()
})

onUnmounted(() => {
  stopPolling()
})
</script>

<style scoped>
.client-detail-page {
}

/* Breadcrumb */
.breadcrumb {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  margin-bottom: 24px;
}

.breadcrumb-link {
  display: flex;
  align-items: center;
  color: var(--text-secondary);
  cursor: pointer;
  transition: color 0.2s;
  margin-right: 4px;
}

.breadcrumb-link:hover {
  color: var(--text-primary);
}

.breadcrumb-item {
  color: var(--text-secondary);
  text-decoration: none;
  transition: color 0.2s;
}

.breadcrumb-item:hover {
  color: var(--el-color-primary);
}

.breadcrumb-separator {
  color: var(--el-border-color);
}

.breadcrumb-current {
  color: var(--text-primary);
  font-weight: 500;
}

/* Card Base */
.header-card,
.proxies-card,
.config-card {
  background: var(--el-bg-color);
  border: 1px solid var(--header-border);
  border-radius: 12px;
  margin-bottom: 16px;
}

/* Header Card */
.header-main {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  padding: 24px;
}

.header-left {
  display: flex;
  gap: 16px;
  align-items: center;
}

.client-avatar {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  font-weight: 500;
  flex-shrink: 0;
}

.client-info {
  min-width: 0;
}

.client-name-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 4px;
}

.client-name {
  font-size: 20px;
  font-weight: 500;
  color: var(--text-primary);
  margin: 0;
  line-height: 1.3;
}

.client-meta {
  display: flex;
  gap: 12px;
  font-size: 14px;
  color: var(--text-secondary);
}

.status-badge {
  padding: 6px 12px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
}

.status-badge.online {
  background: rgba(34, 197, 94, 0.1);
  color: #16a34a;
}

.status-badge.offline {
  background: var(--hover-bg);
  color: var(--text-secondary);
}

html.dark .status-badge.online {
  background: rgba(34, 197, 94, 0.15);
  color: #4ade80;
}

/* Info Section */
.info-section {
  display: flex;
  flex-wrap: wrap;
  gap: 16px 32px;
  padding: 16px 24px;
}

.info-item {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.info-label {
  font-size: 13px;
  color: var(--text-secondary);
}

.info-label::after {
  content: ':';
}

.info-value {
  font-size: 13px;
  color: var(--text-primary);
  font-weight: 500;
  word-break: break-all;
}

/* Config Card */
.config-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 24px;
  border-bottom: 1px solid var(--header-border);
}

.config-title h2 {
  font-size: 15px;
  font-weight: 500;
  color: var(--text-primary);
  margin: 0;
}

.config-body {
  padding: 16px 24px;
  min-height: 80px;
}

.config-columns {
  display: flex;
  gap: 40px;
}

.config-column {
  display: flex;
  flex-direction: column;
  gap: 12px;
  flex: 1;
  min-width: 0;
}

.config-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.config-label {
  font-size: 13px;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.config-value {
  font-size: 13px;
  color: var(--text-primary);
  font-weight: 500;
  word-break: break-all;
  text-align: right;
}

.config-section {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--header-border);
}

.config-section-title {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary);
  margin-bottom: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.config-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.config-tag {
  font-size: 12px;
}

/* Proxies Card */
.proxies-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  gap: 16px;
}

.proxies-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.proxies-title h2 {
  font-size: 15px;
  font-weight: 500;
  color: var(--text-primary);
  margin: 0;
}

.proxies-count {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-secondary);
  background: var(--hover-bg);
  padding: 4px 10px;
  border-radius: 6px;
}

.proxy-search {
  width: 200px;
}

.proxy-search :deep(.el-input__wrapper) {
  border-radius: 6px;
}

.proxies-body {
  padding: 16px;
}

.proxies-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.proxy-item {
  position: relative;
}

.proxy-config-btn {
  position: absolute;
  top: 50%;
  right: 12px;
  transform: translateY(-50%);
  z-index: 1;
  opacity: 0;
  transition: opacity 0.2s;
}

.proxy-item:hover .proxy-config-btn {
  opacity: 1;
}

.loading-state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 40px;
  color: var(--text-secondary);
}

.empty-state {
  text-align: center;
  padding: 40px;
  color: var(--text-secondary);
}

.empty-state p {
  margin: 0;
}

/* Proxy config dialog */
.proxy-config-type {
  margin-bottom: 12px;
}

.proxy-config-fields {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 400px;
  overflow-y: auto;
}

/* Not Found */
.not-found {
  text-align: center;
  padding: 60px 20px;
}

.not-found h2 {
  font-size: 18px;
  font-weight: 500;
  color: var(--text-primary);
  margin: 0 0 8px;
}

.not-found p {
  font-size: 14px;
  color: var(--text-secondary);
  margin: 0 0 20px;
}

/* Resources Card */
.resources-card {
  background: var(--el-bg-color);
  border: 1px solid var(--header-border);
  border-radius: 12px;
  margin-bottom: 16px;
}

.resources-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.metrics-error {
  margin-top: 8px;
  font-size: 13px;
  color: var(--el-color-danger);
}

/* Responsive */
@media (max-width: 640px) {
  .header-main {
    flex-direction: column;
    gap: 16px;
  }

  .header-right {
    align-self: flex-start;
  }

  .config-columns {
    flex-direction: column;
    gap: 20px;
  }
}
</style>

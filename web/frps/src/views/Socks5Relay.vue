<template>
  <div class="socks5relay-page">
    <div class="page-header">
      <div class="header-top">
        <div class="title-section">
          <h1 class="page-title">SOCKS5 Relay</h1>
          <p class="page-subtitle">Manage SOCKS5 relay groups and sessions</p>
        </div>

        <div class="actions-section">
          <ActionButton variant="outline" size="small" @click="fetchData">
            Refresh
          </ActionButton>
        </div>
      </div>
    </div>

    <!-- Section 1: Listener Status -->
    <div class="section">
      <h2 class="section-title">Listener Status</h2>
      <el-row :gutter="16">
        <el-col :xs="24" :sm="12">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">SOCKS5 Proxy Port</div>
              <div v-if="serverInfo.socks5ProxyPort > 0" class="status-value">
                {{ serverInfo.socks5ProxyPort }}
              </div>
              <el-tag v-else size="small" type="info">Not configured</el-tag>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="12">
          <el-card class="status-card" shadow="hover">
            <div class="status-item">
              <div class="status-label">HTTP CONNECT Proxy Port</div>
              <div v-if="serverInfo.httpConnectProxyPort > 0" class="status-value">
                {{ serverInfo.httpConnectProxyPort }}
              </div>
              <el-tag v-else size="small" type="info">Not configured</el-tag>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </div>

    <!-- Section 2: Groups -->
    <div class="section">
      <h2 class="section-title">Groups</h2>
      <div v-loading="loadingGroups" class="groups-content">
        <div v-if="groups.length > 0" class="groups-list">
          <el-card
            v-for="group in groups"
            :key="group.name"
            class="group-card"
            shadow="hover"
          >
            <template #header>
              <div class="group-header">
                <span class="group-name">{{ group.name }}</span>
                <el-tag size="small" type="info">
                  {{ group.members.length }} member{{ group.members.length !== 1 ? 's' : '' }}
                  ({{ onlineCount(group.members) }} online)
                </el-tag>
              </div>
            </template>
            <el-table :data="group.members" class="members-table">
              <el-table-column prop="proxyName" label="Proxy Name" />
              <el-table-column prop="runID" label="Run ID" />
              <el-table-column label="Online" width="80">
                <template #default="{ row }">
                  <el-tag :type="row.online ? 'success' : 'danger'" size="small">
                    {{ row.online ? 'Yes' : 'No' }}
                  </el-tag>
                </template>
              </el-table-column>
            </el-table>
          </el-card>
        </div>
        <div v-else-if="!loadingGroups" class="empty-state">
          <el-empty description="No groups found" />
        </div>
      </div>
    </div>

    <!-- Section 3: Sessions -->
    <div class="section">
      <h2 class="section-title">Sessions</h2>
      <el-card v-loading="loadingSessions" shadow="hover">
        <div v-if="sessions.length > 0">
          <el-table :data="sessions">
            <el-table-column prop="username" label="Username (Group)" />
            <el-table-column prop="runID" label="Run ID" />
          </el-table>
        </div>
        <div v-else-if="!loadingSessions">
          <el-empty description="No active sessions" />
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import ActionButton from '@shared/components/ActionButton.vue'
import { getServerInfo } from '../api/server'
import { getSocks5RelayGroups, getSocks5RelaySessions } from '../api/socks5relay'
import type { Socks5RelayGroupInfo, Socks5RelaySessionInfo } from '../types/socks5relay'

const serverInfo = ref({
  socks5ProxyPort: 0,
  httpConnectProxyPort: 0,
})
const groups = ref<Socks5RelayGroupInfo[]>([])
const sessions = ref<Socks5RelaySessionInfo[]>([])
const loadingGroups = ref(false)
const loadingSessions = ref(false)

const onlineCount = (members: any[]) => {
  return members.filter((m) => m.online).length
}

const fetchServerInfo = async () => {
  try {
    const json = await getServerInfo()
    serverInfo.value.socks5ProxyPort = json.socks5ProxyPort || 0
    serverInfo.value.httpConnectProxyPort = json.httpConnectProxyPort || 0
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to fetch server info: ' + error.message,
      type: 'error',
    })
  }
}

const fetchGroups = async () => {
  loadingGroups.value = true
  try {
    groups.value = await getSocks5RelayGroups()
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to fetch groups: ' + error.message,
      type: 'error',
    })
  } finally {
    loadingGroups.value = false
  }
}

const fetchSessions = async () => {
  loadingSessions.value = true
  try {
    sessions.value = await getSocks5RelaySessions()
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to fetch sessions: ' + error.message,
      type: 'error',
    })
  } finally {
    loadingSessions.value = false
  }
}

const fetchData = async () => {
  await Promise.all([fetchServerInfo(), fetchGroups(), fetchSessions()])
}

onMounted(() => {
  fetchData()
})
</script>

<style scoped>
.socks5relay-page {
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
}

.section {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.section-title {
  font-size: 18px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  margin: 0;
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

.groups-content {
  min-height: 100px;
}

.groups-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.group-card {
  border-radius: 12px;
  border: 1px solid #e4e7ed;
}

html.dark .group-card {
  border-color: #3a3d5c;
  background: #27293d;
}

.group-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.group-name {
  font-size: 16px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.members-table {
  width: 100%;
}

.empty-state {
  padding: 40px 0;
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

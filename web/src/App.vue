<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import StatusView from './views/StatusView.vue'
import ConfigView from './views/ConfigView.vue'
import SettingsView from './views/SettingsView.vue'
import Icon from './components/Icon.vue'
import { fetchStatus, modeAction, subscribeStatus, type Status } from './api'

type View = 'status' | 'config' | 'settings'
const view = ref<View>('status')
const status = ref<Status | null>(null)
// null = 尚未确定，true = 可达，false = 不可达
const connected = ref<boolean | null>(null)
const loading = ref(false)
const message = ref<{ ok: boolean; text: string } | null>(null)
let messageTimer: ReturnType<typeof setTimeout> | null = null

let unsubscribe: (() => void) | null = null

onMounted(() => {
  refresh()
  unsubscribe = subscribeStatus(
    (s) => {
      status.value = s
      connected.value = true
    },
    (ok) => (connected.value = ok),
  )
})

onUnmounted(() => {
  unsubscribe?.()
  if (messageTimer) clearTimeout(messageTimer)
})

async function refresh() {
  try {
    status.value = await fetchStatus()
    connected.value = true
  } catch {
    connected.value = false
  }
}

async function retry() {
  connected.value = null
  await refresh()
}

async function onModeAction(mode: string, action: 'start' | 'stop') {
  loading.value = true
  try {
    await modeAction(mode, action)
    await refresh()
    // 依据刷新后的真实状态反馈结果，而非只报「指令已执行」
    const ms = status.value?.modes[mode]
    const label = ms?.label ?? mode
    let text: string
    let ok = true
    if (action === 'start') {
      if (ms?.active) text = `${label} 已启动`
      else if (ms?.unit_state === 'failed') {
        text = `${label} 启动失败`
        ok = false
      } else text = `${label} 启动指令已下发，守护进程编排中`
    } else {
      if (!ms || !ms.active) text = `${label} 已停止`
      else text = `${label} 停止指令已下发，守护进程编排中`
    }
    showMessage(ok, text)
  } catch (e) {
    showMessage(false, (e as Error).message)
  } finally {
    loading.value = false
  }
}

function showMessage(ok: boolean, text: string) {
  message.value = { ok, text }
  if (messageTimer) clearTimeout(messageTimer)
  messageTimer = setTimeout(() => (message.value = null), 5000)
}

const nav: { key: View; label: string; icon: string }[] = [
  { key: 'status', label: '状态', icon: 'activity' },
  { key: 'config', label: '配置', icon: 'file' },
  { key: 'settings', label: '设置', icon: 'sliders' },
]

const activeLabel = () => {
  const s = status.value
  if (!s || !s.active_mode) return '未运行'
  const label = s.modes[s.active_mode]?.label ?? s.active_mode
  return `正在运行 ${label}`
}
</script>

<template>
  <div class="app">
    <header class="topnav">
      <div class="logo">
        <div class="logo-mark">
          <img src="/logo.svg" alt="LeanClash" class="logo-img" />
        </div>
        <div class="logo-text">
          <b>LeanClash</b>
        </div>
      </div>

      <nav class="seg">
        <button
          v-for="item in nav"
          :key="item.key"
          class="seg-item"
          :class="{ active: view === item.key }"
          @click="view = item.key"
        >
          <Icon :name="item.icon" :size="15" />
          {{ item.label }}
        </button>
      </nav>

      <div class="topnav-chip">
        <span class="dot" :class="{ on: !!status?.active_mode }"></span>
        <span>{{ activeLabel() }}</span>
      </div>
    </header>

    <main class="main">
      <!-- KeepAlive：切换页签不销毁视图，配置编辑器内容与表单状态得以保留 -->
      <div class="content">
        <KeepAlive>
          <StatusView
            v-if="view === 'status'"
            :status="status"
            :connected="connected"
            :loading="loading"
            @action="onModeAction"
            @refresh="retry"
          />
          <ConfigView v-else-if="view === 'config'" />
          <SettingsView v-else />
        </KeepAlive>
      </div>

      <transition name="fade">
        <div v-if="message" class="toast" :class="message.ok ? 'ok' : 'err'">
          <Icon :name="message.ok ? 'check-circle' : 'alert-triangle'" :size="16" />
          {{ message.text }}
        </div>
      </transition>
    </main>
  </div>
</template>

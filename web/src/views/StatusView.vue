<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../components/Icon.vue'
import type { Status, ModeStatus } from '../api'

const props = defineProps<{
  status: Status | null
  // null = 尚未确定，true = 可达，false = 不可达
  connected: boolean | null
  loading: boolean
}>()

defineEmits<{ (e: 'action', mode: string, action: 'start' | 'stop'): void; (e: 'refresh'): void }>()

const modeOrder = ['tun', 'tproxy', 'redir-tproxy', 'socks']

const modeIcons: Record<string, string> = {
  tun: 'activity',
  tproxy: 'shuffle',
  'redir-tproxy': 'git-merge',
  socks: 'zap',
}

const entries = computed(() => {
  if (!props.status) return []
  return modeOrder
    .filter((n) => props.status!.modes[n])
    .map((n, i) => ({ name: n, index: i, icon: modeIcons[n] ?? 'zap', ...props.status!.modes[n] }))
})

const tiles = computed(() => entries.value)

// 首页 hero 以 active_mode 为准：systemd 探测到的 mihomo@i 也会显示为「正在运行 i 模式」
const active = computed(() => {
  const st = props.status
  if (!st?.active_mode) return null
  const name = st.active_mode
  const found = entries.value.find((m) => m.name === name)
  if (found) return found
  const ms = st.modes[name]
  if (!ms?.active) return null
  return { name, index: -1, icon: modeIcons[name] ?? 'zap', ...ms }
})

const rulesText: Record<string, string> = {
  present: '规则就位',
  missing: '规则缺失',
  clean: '无残留',
  leftover: '有残留',
  na: '不适用',
}

function rulesClass(m: ModeStatus): string {
  if (m.rules === 'present') return 'active'
  if (m.rules === 'missing' || m.rules === 'leftover') return 'partial'
  return ''
}

const unitText: Record<string, string> = {
  active: '运行中',
  activating: '启动中',
  deactivating: '停止中',
  inactive: '已停止',
  failed: '失败',
  unknown: '未知',
}

function unitClass(s: string): string {
  if (s === 'active' || s === 'activating' || s === 'deactivating') return 'active'
  if (s === 'failed') return 'failed'
  return ''
}
</script>

<template>
  <div class="status-page">
    <div class="page-head">
      <h1><Icon name="activity" :size="18" /> 运行状态</h1>
      <p class="sub">选择并切换 mihomo 透明代理模式</p>
    </div>

    <!-- ─── hero：不可达 ─── -->
    <section v-if="!status && connected === false" class="hero error">
      <div class="hero-icon"><Icon name="alert-triangle" :size="24" /></div>
      <div class="hero-info">
        <div class="hero-title"><b>守护进程不可达</b></div>
        <div class="hero-unit">无法连接后端服务，请检查 leanclash 守护进程是否运行</div>
      </div>
      <div class="hero-actions">
        <button class="btn primary" @click="$emit('refresh')">
          <Icon name="refresh-cw" :size="14" /> 重试
        </button>
      </div>
    </section>

    <!-- ─── hero：连接中 ─── -->
    <section v-else-if="!status" class="hero idle">
      <div class="hero-icon"><Icon name="power" :size="24" /></div>
      <div class="hero-info"><div class="hero-title"><b>连接中…</b></div></div>
    </section>

    <!-- ─── Phase 3.1: 活跃时显示紧凑状态条 ─── -->
    <section
      v-else-if="active"
      class="hero"
      :class="`c-${active.name}`"
    >
      <div class="hero-icon"><Icon :name="active.icon" :size="24" /></div>
      <div class="hero-info">
        <div class="hero-title">
          <b>正在运行 {{ active.label }} 模式</b>
          <small>{{ active.unit || active.name }}</small>
        </div>
        <div class="hero-badges">
          <span class="badge" :class="unitClass(active.unit_state)">
            <span class="dot" :class="{ on: active.active }"></span>
            {{ unitText[active.unit_state] ?? active.unit_state }}
          </span>
          <span class="badge" :class="rulesClass(active)">
            {{ rulesText[active.rules] ?? active.rules }}
          </span>
        </div>
      </div>
      <div class="hero-actions">
        <!-- 活跃时：次要「停止」按钮（非满宽） -->
        <button class="btn danger" :disabled="loading" @click="$emit('action', active.name, 'stop')">
          <Icon name="stop" :size="14" /> 停止
        </button>
      </div>
    </section>

    <section v-else class="hero idle">
      <div class="hero-icon"><Icon name="power" :size="24" /></div>
      <div class="hero-info">
        <div class="hero-title"><b>无活跃模式</b></div>
      </div>
    </section>

    <!-- ─── Phase 3.1: 所有模式网格（同等权重），活跃态用顶部 accent 线区分 ─── -->
    <section class="section">
      <h2 class="section-title">模式</h2>
      <div class="tiles">
      <div
        v-for="m in tiles"
        :key="m.name"
        class="tile"
        :class="[`c-${m.name}`, { 'tile-active': m.active }]"
      >
        <div class="tile-head">
          <div class="tile-icon"><Icon :name="m.icon" :size="17" /></div>
          <div class="tile-title">
            <b>{{ m.label }}</b>
            <small>{{ m.name }}</small>
          </div>
          <span class="tile-state" :class="{ on: m.active }" :title="unitText[m.unit_state] ?? m.unit_state"></span>
        </div>

        <div class="tile-meta">
          <span class="badge" :class="unitClass(m.unit_state)">
            {{ unitText[m.unit_state] ?? m.unit_state }}
          </span>
          <span v-if="m.active" class="badge" :class="rulesClass(m)">
            {{ rulesText[m.rules] ?? m.rules }}
          </span>
          <span v-else-if="m.rules === 'leftover'" class="badge partial">
            {{ rulesText[m.rules] ?? m.rules }}
          </span>
        </div>

        <!-- Phase 3.1: 活跃模式显示「停止」，非活跃显示「启动」 -->
        <div class="tile-actions">
          <button
            v-if="m.active"
            class="btn danger"
            :disabled="loading"
            @click="$emit('action', m.name, 'stop')"
          >
            <Icon name="stop" :size="14" /> 停止
          </button>
          <button
            v-else
            class="btn primary"
            :disabled="loading"
            @click="$emit('action', m.name, 'start')"
          >
            <Icon name="power" :size="14" /> 启动
          </button>
        </div>
      </div>
      </div>
    </section>
  </div>
</template>

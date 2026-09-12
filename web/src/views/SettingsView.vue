<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Icon from '../components/Icon.vue'
import NetworkSection from '../components/NetworkSection.vue'
import { fetchSettings, saveSettings, type ManagerSettings } from '../api'

const settings = ref<ManagerSettings | null>(null)
const message = ref<{ ok: boolean; text: string } | null>(null)
const saving = ref(false)

// 回环避免方式：gid（meta skgid）优先 / mark（meta mark）备选
const bypassTproxy = ref<'gid' | 'mark'>('gid')
const bypassRedir = ref<'gid' | 'mark'>('gid')

// 按 exclude_gid 是否生效推导当前回环避免方式（服务端保存时二选一清零）
function deriveBypass() {
  if (!settings.value) return
  bypassTproxy.value = (settings.value.modes.tproxy?.env?.exclude_gid ?? 0) > 0 ? 'gid' : 'mark'
  bypassRedir.value = (settings.value.modes['redir-tproxy']?.env?.exclude_gid ?? 0) > 0 ? 'gid' : 'mark'
}

onMounted(async () => {
  try {
    settings.value = await fetchSettings()
    deriveBypass()
  } catch (e) {
    message.value = { ok: false, text: (e as Error).message }
  }
})

async function save() {
  if (!settings.value) return
  saving.value = true
  message.value = null
  try {
    const s = settings.value
    const update: Record<string, unknown> = { daemon: s.daemon, modes: {} }
    const modes = update.modes as Record<string, unknown>

    modes.tun = {
      routing: s.modes.tun?.routing ?? { rule_index: 0, table_index: 0 },
      preset: s.modes.tun?.preset ?? '',
    }

    for (const [name, sel] of [
      ['tproxy', bypassTproxy],
      ['redir-tproxy', bypassRedir],
    ] as const) {
      const env = { ...(s.modes[name]?.env ?? {}) }
      // 二选一：未被选中的方式清零（守护侧校验至少其一）
      if (sel.value === 'gid') env.routing_mark = 0
      else env.exclude_gid = 0
      modes[name] = { env, preset: s.modes[name]?.preset ?? '' }
    }

    if (s.modes.socks) {
      modes.socks = { env: { socks_port: s.modes.socks.env?.socks_port ?? 0 } }
    }

    await saveSettings(update)
    // 重新拉取设置同步本地状态（清零结果、守护侧规范化后的值）
    settings.value = await fetchSettings()
    deriveBypass()
    message.value = { ok: true, text: '设置已保存' }
  } catch (e) {
    message.value = { ok: false, text: (e as Error).message }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div v-if="settings">
    <div class="page-head-row">
      <div>
        <h1 class="page-title"><Icon name="sliders" :size="22" /> 系统设置</h1>
        <p class="sub">守护进程与网络参数</p>
      </div>
      <button class="btn primary" :disabled="saving" @click="save">
        <Icon name="save" :size="15" /> 保存设置
      </button>
    </div>

    <!-- ─── 守护进程 ─── -->
    <section class="list card">
      <div class="list-head">守护进程</div>
      <div class="list-row">
        <div class="row-text">
          <span class="row-label">Web 监听地址</span>
          <small class="row-hint">无鉴权：默认仅监听 IPv4</small>
        </div>
        <div class="control">
          <input v-model="settings.daemon.web_addr" class="input" />
        </div>
      </div>
      <div class="list-row">
        <span class="row-label">套规则延迟（毫秒）</span>
        <input v-model.number="settings.daemon.apply_delay_ms" type="number" class="input input-num" />
      </div>
      <div class="list-row">
        <span class="row-label">对账间隔</span>
        <input v-model="settings.daemon.reconcile_interval" class="input" />
      </div>
    </section>

    <!-- ─── TUN ─── -->
    <section class="list card">
      <div class="list-head">TUN · 路由索引（tun0 消失后按此清理）</div>
      <div class="list-row">
        <span class="row-label">ip rule 起始索引</span>
        <input v-model.number="settings.modes.tun!.routing!.rule_index" type="number" class="input input-num" />
      </div>
      <div class="list-row">
        <span class="row-label">路由表索引</span>
        <input v-model.number="settings.modes.tun!.routing!.table_index" type="number" class="input input-num" />
      </div>
      <details class="list-details">
        <summary>
          <span>预定义入站（preset）</span>
          <Icon name="chevron-down" :size="15" class="chev" />
        </summary>
        <div class="details-body">
          <textarea v-model="settings.modes.tun!.preset" class="code" style="min-height: 200px"></textarea>
        </div>
      </details>
    </section>

    <!-- ─── TPROXY / REDIR-TPROXY ─── -->
    <NetworkSection
      v-model:bypass="bypassTproxy"
      head="TPROXY · 网络参数"
      :env="settings.modes.tproxy!.env!"
    />
    <NetworkSection
      v-model:bypass="bypassRedir"
      head="REDIR-TPROXY · 网络参数（TCP REDIRECT + UDP TPROXY）"
      show-redirect
      :env="settings.modes['redir-tproxy']!.env!"
    />

    <!-- ─── SOCKS ─── -->
    <section class="list card">
      <div class="list-head">SOCKS · 入站</div>
      <div class="list-row">
        <div class="row-text">
          <span class="row-label">SOCKS 入站监听端口</span>
          <small class="row-hint">mixed 入站 · 默认 20260</small>
        </div>
        <div class="control">
          <input v-model.number="settings.modes.socks!.env!.socks_port" type="number" class="input input-num" />
        </div>
      </div>
    </section>

    <div v-if="message" class="alert" :class="message.ok ? 'ok' : 'err'">
      <Icon :name="message.ok ? 'check-circle' : 'alert-triangle'" :size="16" />
      <span>{{ message.text }}</span>
    </div>
  </div>
  <div v-else class="card" style="color: var(--text-2)">加载设置中…</div>
</template>

// REST + SSE 客户端。生产环境与后端同源；开发环境经 vite proxy。

export interface ModeStatus {
  label: string
  unit: string
  unit_state: string
  rules: string
  active: boolean
}

export interface Status {
  modes: Record<string, ModeStatus>
  active_mode: string
}

export interface EnvSettings {
  tproxy_port?: number
  redirect_port?: number
  socks_port?: number
  exclude_gid?: number
  routing_mark?: number
  fwmark?: number
  table_id?: number
  nftables_table?: string
}

export interface ModeSettings {
  label?: string
  unit?: string
  config?: string
  routing?: { rule_index?: number; table_index?: number }
  cleanup?: { nft_tables?: string[] }
  env?: EnvSettings | null
  preset?: string
}

export interface ManagerSettings {
  dirs: { config_dir: string; data_dir: string }
  daemon: { web_addr: string; cli_socket: string; apply_delay_ms: number; reconcile_interval: string }
  modes: Record<string, ModeSettings>
}

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error((body as { error?: string }).error ?? `HTTP ${res.status}`)
  }
  return body as T
}

export const fetchStatus = () => req<Status>('/api/status')

export function modeAction(mode: string, action: 'start' | 'stop') {
  return req(`/api/modes/${encodeURIComponent(mode)}/${action}`, { method: 'POST' })
}

export const configSync = () => req('/api/config/sync', { method: 'POST' })

export async function fetchGeneral(): Promise<string> {
  const r = await req<{ content: string }>('/api/config/general')
  return r.content
}

export async function saveGeneral(content: string) {
  await req('/api/config/general', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content }),
  })
}

export async function fetchModeConfig(mode: string): Promise<string> {
  const r = await req<{ content: string }>(`/api/config/mode/${encodeURIComponent(mode)}`)
  return r.content
}

export const fetchSettings = () => req<ManagerSettings>('/api/settings')

export async function saveSettings(update: Record<string, unknown>) {
  await req('/api/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(update),
  })
}

// subscribeStatus 建立 SSE 连接；返回取消函数。
// onConnect 在连接建立（true）/断开（false）时回调，用于驱动「守护进程不可达」提示。
export function subscribeStatus(onStatus: (s: Status) => void, onConnect?: (ok: boolean) => void): () => void {
  const es = new EventSource('/api/events')
  es.onopen = () => onConnect?.(true)
  es.onmessage = (ev) => {
    try {
      const msg = JSON.parse(ev.data)
      if (msg.type === 'status' && msg.data) onStatus(msg.data as Status)
    } catch {
      /* 忽略坏帧 */
    }
  }
  es.onerror = () => {
    // EventSource 自动重连，无需处理
    onConnect?.(false)
  }
  return () => es.close()
}

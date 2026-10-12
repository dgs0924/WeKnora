import { get, post, put } from '@/utils/request'

export interface CodexConnectionStatus {
  /** False for users other than the account owner; the settings section stays hidden. */
  enabled: boolean
  connected: boolean
  email?: string
  plan?: string
  /** A non-sensitive availability marker returned when the CLI cannot be used. */
  connection_error?: string
  /** True only for the server Codex connection owner. */
  manage_grants?: boolean
}

export interface CodexModelGrant {
  /** The WeKnora model ID. Use this value for grant updates. */
  id: string
  name: string
  display_name: string
  user_ids: string[]
}

export interface CodexUsageWindow {
  kind: 'primary' | 'secondary'
  used_percent: number | null
  remaining_percent: number | null
  window_duration_mins: number | null
  resets_at: number | null
}

export interface CodexUsageLimit {
  id: string
  name: string
  windows: CodexUsageWindow[]
}

export interface CodexUsage {
  available: boolean
  limits: CodexUsageLimit[]
  updated_at?: string
}

export interface CodexGrantUser {
  id: string
  username: string
  email?: string
  avatar?: string
}

interface CodexModelGrantsResponse {
  models: CodexModelGrant[]
}

interface CodexGrantUsersResponse {
  users: CodexGrantUser[]
  has_more: boolean
}

function nullableNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function normalizeUsageWindow(value: Partial<CodexUsageWindow> | null | undefined): CodexUsageWindow {
  return {
    kind: value?.kind === 'secondary' ? 'secondary' : 'primary',
    used_percent: nullableNumber(value?.used_percent),
    remaining_percent: nullableNumber(value?.remaining_percent),
    window_duration_mins: nullableNumber(value?.window_duration_mins),
    resets_at: nullableNumber(value?.resets_at),
  }
}

export function normalizeCodexUsage(value: Partial<CodexUsage> | null | undefined): CodexUsage {
  return {
    available: value?.available === true,
    limits: Array.isArray(value?.limits)
      ? value!.limits.map((limit) => ({
        id: typeof limit?.id === 'string' ? limit.id : '',
        name: typeof limit?.name === 'string' ? limit.name : '',
        windows: Array.isArray(limit?.windows) ? limit.windows.map(normalizeUsageWindow) : [],
      }))
      : [],
    updated_at: typeof value?.updated_at === 'string' ? value.updated_at : undefined,
  }
}

export interface CodexModel {
  slug: string
  display_name: string
  /** The corresponding WeKnora model ID after it has been enabled. */
  id?: string
}

interface CodexModelsResponse {
  models: CodexModel[]
}

const endpoint = '/api/v1/codex'

function payload<T>(response: T | { data?: T }): T {
  if (response && typeof response === 'object' && 'data' in response && response.data !== undefined) {
    return response.data
  }
  return response as T
}

export async function getCodexStatus(): Promise<CodexConnectionStatus> {
  return payload<CodexConnectionStatus>(await get(`${endpoint}/status`) as unknown as CodexConnectionStatus)
}

export async function getCodexUsage(): Promise<CodexUsage> {
  return normalizeCodexUsage(payload<CodexUsage>(await get(`${endpoint}/usage`) as unknown as CodexUsage))
}

export async function listCodexModels(): Promise<CodexModel[]> {
  const response = payload<CodexModelsResponse>(await get(`${endpoint}/models`) as unknown as CodexModelsResponse)
  return Array.isArray(response.models) ? response.models : []
}

export async function selectCodexModel(slug: string): Promise<void> {
  await post(`${endpoint}/models/select`, { slug })
}

/** Lists grants owned by the signed-in server Codex account. */
export async function listCodexModelGrants(): Promise<CodexModelGrant[]> {
  const response = payload<CodexModelGrantsResponse>(
    await get(`${endpoint}/grants`) as unknown as CodexModelGrantsResponse,
  )
  return Array.isArray(response.models)
    ? response.models.map((model) => ({ ...model, user_ids: Array.isArray(model.user_ids) ? model.user_ids : [] }))
    : []
}

/** Lists active WeKnora users eligible for server Codex access. */
export async function listCodexGrantUsers(): Promise<CodexGrantUser[]> {
  const users: CodexGrantUser[] = []
  const limit = 100
  for (let offset = 0; offset < 50000; offset += limit) {
    const response = payload<CodexGrantUsersResponse>(
      await get(`${endpoint}/grant-users?offset=${offset}&limit=${limit}`) as unknown as CodexGrantUsersResponse,
    )
    users.push(...(Array.isArray(response.users) ? response.users : []))
    if (!response.has_more) break
  }
  return users
}

/** Atomically replaces one user's grants. The server enforces owner scope. */
export async function updateCodexUserGrants(userId: string, modelIds: string[]): Promise<void> {
  await put(`${endpoint}/grants/users/${encodeURIComponent(userId)}`, { model_ids: modelIds })
}

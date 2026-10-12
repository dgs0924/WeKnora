import { get, post } from '@/utils/request'

export interface CodexConnectionStatus {
  /** False for users other than the account owner; the settings section stays hidden. */
  enabled: boolean
  connected: boolean
  email?: string
  plan?: string
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

export async function listCodexModels(): Promise<CodexModel[]> {
  const response = payload<CodexModelsResponse>(await get(`${endpoint}/models`) as unknown as CodexModelsResponse)
  return Array.isArray(response.models) ? response.models : []
}

export async function selectCodexModel(slug: string): Promise<void> {
  await post(`${endpoint}/models/select`, { slug })
}

import { del, get, post } from '@/utils/request'

/**
 * A personal ChatGPT subscription connection. The API deliberately exposes
 * account metadata only; OAuth tokens and authorization codes never leave the
 * backend.
 */
export interface ChatGPTConnectionStatus {
  connected: boolean
  email?: string
  expires_at?: string
  plan_enabled?: boolean
  pending?: {
    attempt_id: string
    status: string
    error?: string
  }
  loopback_required: boolean
}

export interface ChatGPTAuthorization {
  authorization_url: string
  attempt_id: string
  expires_at?: string
}

export interface ChatGPTModel {
  slug: string
  display_name: string
  /** Whether this account-authorized model accepts image input. */
  supports_vision?: boolean
  /** The corresponding WeKnora model ID, once this model has been enabled. */
  id?: string
}

export interface ChatGPTModelsResponse {
  models: ChatGPTModel[]
}

export interface ChatGPTModelSelection {
  model_id?: string
  id?: string
}

const endpoint = '/api/v1/chatgpt'

/** Accept both the new direct payload and the legacy `{ data }` API envelope. */
function payload<T>(response: T | { data?: T }): T {
  if (response && typeof response === 'object' && 'data' in response && response.data !== undefined) {
    return response.data
  }
  return response as T
}

export async function getChatGPTStatus(): Promise<ChatGPTConnectionStatus> {
  return payload<ChatGPTConnectionStatus>(await get(`${endpoint}/status`) as unknown as ChatGPTConnectionStatus)
}

export async function authorizeChatGPT(): Promise<ChatGPTAuthorization> {
  return payload<ChatGPTAuthorization>(await post(`${endpoint}/authorize`) as unknown as ChatGPTAuthorization)
}

export type ChatGPTPlanModelType = 'KnowledgeQA' | 'VLLM'

export async function listChatGPTModels(modelType: ChatGPTPlanModelType = 'KnowledgeQA'): Promise<ChatGPTModel[]> {
  const query = modelType === 'VLLM' ? '?model_type=VLLM' : ''
  const response = payload<ChatGPTModelsResponse>(await get(`${endpoint}/models${query}`) as unknown as ChatGPTModelsResponse)
  return Array.isArray(response.models) ? response.models : []
}

export async function selectChatGPTModel(
  slug: string,
  modelType: ChatGPTPlanModelType = 'KnowledgeQA',
): Promise<ChatGPTModelSelection> {
  const body = modelType === 'VLLM' ? { slug, model_type: 'VLLM' } : { slug }
  return payload<ChatGPTModelSelection>(
    await post(`${endpoint}/models/select`, body) as unknown as ChatGPTModelSelection,
  )
}

export async function disconnectChatGPT(): Promise<void> {
  await del(`${endpoint}/connection`)
}

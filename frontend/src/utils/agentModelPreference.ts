import type { CustomAgent, CustomAgentConfig } from '@/api/agent'

/**
 * Personal model preferences belong to the current user's local-space view of
 * an agent. Shared agents always retain the source space model.
 */
export function effectiveAgentModelId(
  agent?: Pick<CustomAgent, 'config' | 'personal_model_id'> | null,
  sourceTenantId?: string | null,
): string {
  if (!agent) return ''
  if (!sourceTenantId && agent.personal_model_id?.trim()) return agent.personal_model_id.trim()
  return agent.config?.model_id?.trim() || ''
}

/** Returns a shallow clone only when the effective model differs from config. */
export function configWithEffectiveAgentModel(
  agent: Pick<CustomAgent, 'config' | 'personal_model_id'>,
  sourceTenantId?: string | null,
): CustomAgentConfig {
  const modelId = effectiveAgentModelId(agent, sourceTenantId)
  if (modelId === (agent.config?.model_id || '')) return agent.config || {}
  return { ...(agent.config || {}), model_id: modelId }
}

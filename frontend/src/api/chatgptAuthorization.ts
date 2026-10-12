import type { ChatGPTConnectionStatus } from './chatgpt'

export type ChatGPTAuthorizationOutcome = 'pending' | 'succeeded' | 'failed'

const FAILED_ATTEMPT_STATES = new Set(['failed', 'error', 'expired', 'cancelled', 'canceled'])

/**
 * Only the attempt started by this browser session may complete its popup.
 * `connected` alone is insufficient: a user can renew an already connected
 * account, in which case the old connection remains usable while the new
 * attempt is still pending.
 */
export function chatGPTAuthorizationOutcome(
  status: ChatGPTConnectionStatus,
  attemptID: string,
): ChatGPTAuthorizationOutcome {
  const pending = status.pending
  if (!attemptID || !pending || pending.attempt_id !== attemptID) return 'pending'
  const attemptStatus = pending.status.trim().toLowerCase()
  if (FAILED_ATTEMPT_STATES.has(attemptStatus)) return 'failed'
  if (attemptStatus === 'succeeded' && status.connected) return 'succeeded'
  return 'pending'
}

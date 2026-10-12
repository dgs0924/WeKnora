import type { CodexModelGrant } from './codex'

/** Returns a fresh selection for exactly one user; it never reuses another user's state. */
export function grantSelectionForUser(models: CodexModelGrant[], userID: string): string[] {
  if (!userID) return []
  return models.filter((model) => model.user_ids.includes(userID)).map((model) => model.id)
}

/** Immutably updates the pending selection before the atomic user-level save. */
export function toggleGrantSelection(selection: string[], modelID: string, checked: boolean): string[] {
  return checked
    ? [...new Set([...selection, modelID])]
    : selection.filter((id) => id !== modelID)
}

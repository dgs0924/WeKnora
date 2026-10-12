import assert from 'node:assert/strict'
import test from 'node:test'
import { grantSelectionForUser, toggleGrantSelection } from './codexGrantState'

const models = [
  { id: 'model-a', name: 'a', display_name: 'A', user_ids: ['alice'] },
  { id: 'model-b', name: 'b', display_name: 'B', user_ids: ['bob'] },
]

test('switching users reconstructs selection without carrying unsaved choices across users', () => {
  const alicePending = toggleGrantSelection(grantSelectionForUser(models, 'alice'), 'model-b', true)
  assert.deepEqual(alicePending, ['model-a', 'model-b'])
  assert.deepEqual(grantSelectionForUser(models, 'bob'), ['model-b'])
  assert.deepEqual(grantSelectionForUser(models, ''), [])
})

test('revoking a selected model leaves the other selections intact', () => {
  assert.deepEqual(toggleGrantSelection(['model-a', 'model-b'], 'model-a', false), ['model-b'])
})

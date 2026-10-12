import assert from 'node:assert/strict'
import test from 'node:test'

import { chatGPTAuthorizationOutcome } from './chatgptAuthorization'

test('only the matching succeeded attempt completes ChatGPT authorization', () => {
  assert.equal(
    chatGPTAuthorizationOutcome({ connected: true, loopback_required: true, pending: { attempt_id: 'old', status: 'pending' } }, 'new'),
    'pending',
  )
  assert.equal(
    chatGPTAuthorizationOutcome({ connected: true, loopback_required: true, pending: { attempt_id: 'new', status: 'pending' } }, 'new'),
    'pending',
  )
  assert.equal(
    chatGPTAuthorizationOutcome({ connected: true, loopback_required: true, pending: { attempt_id: 'new', status: 'succeeded' } }, 'new'),
    'succeeded',
  )
  assert.equal(
    chatGPTAuthorizationOutcome({ connected: false, loopback_required: true, pending: { attempt_id: 'new', status: 'succeeded' } }, 'new'),
    'pending',
  )
})

test('matching terminal failures stop ChatGPT authorization', () => {
  assert.equal(
    chatGPTAuthorizationOutcome({ connected: true, loopback_required: true, pending: { attempt_id: 'new', status: 'expired' } }, 'new'),
    'failed',
  )
})

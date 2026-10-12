import assert from 'node:assert/strict'
import test from 'node:test'
import { usagePercent, usageResetAt, usageUpdatedAt, usageWindowDuration } from './codexUsage'

test('usage percentages clamp valid values and never turn missing data into zero', () => {
  assert.equal(usagePercent(23.4), 23)
  assert.equal(usagePercent(-1), 0)
  assert.equal(usagePercent(101), 100)
  assert.equal(usagePercent(null), null)
})

test('usage duration renders exact server-provided windows', () => {
  assert.match(usageWindowDuration(300, 'en-US') || '', /5 hours/)
  assert.match(usageWindowDuration(10080, 'en-US') || '', /7 days/)
})

test('usage reset timestamps are local date-times and missing values remain unavailable', () => {
  assert.match(usageResetAt(1792371909, 'en-US') || '', /2026/)
  assert.equal(usageResetAt(null, 'en-US'), null)
})

test('usage snapshot time remains a local date-time and invalid snapshots stay unavailable', () => {
  assert.match(usageUpdatedAt('2026-10-12T08:00:00Z', 'en-US') || '', /2026/)
  assert.equal(usageUpdatedAt('not-a-date', 'en-US'), null)
})

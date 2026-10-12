import assert from 'node:assert/strict'
import test from 'node:test'
import { createMemoryHistory, createRouter } from 'vue-router'

import {
  buildSettingsRouteQuery,
  integrationSectionKey,
  isIntegrationSection,
  normalizeSettingsSection,
  settingsQueryUnchanged,
  settingsCloseTarget,
} from './settingsRoute'

test('every settings nav item writes only section', () => {
  assert.deepEqual(
    buildSettingsRouteQuery('models', {
      section: 'system-global',
      tab: 'im',
      agentId: 'agt_1',
    }),
    { section: 'models' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery('general', { section: 'system-global' }),
    { section: 'general' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery('runtime-queues', { section: 'system-global' }),
    { section: 'runtime-queues' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery(integrationSectionKey('claw'), {
      section: 'integrations',
      tab: 'im',
    }),
    { section: 'integration-claw' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery(integrationSectionKey('api'), {
      section: 'integrations',
      tab: 'im',
      agentId: 'agt_1',
    }),
    { section: 'integration-api', agentId: 'agt_1' },
  )
})

test('legacy api / integrations / bare-tab query strings normalize to nav keys', () => {
  assert.equal(normalizeSettingsSection('api'), 'integration-api')
  assert.equal(normalizeSettingsSection('claw'), 'integration-claw')
  assert.equal(normalizeSettingsSection('cli'), 'integration-cli')
  assert.equal(normalizeSettingsSection('mcpserver'), 'integration-mcpserver')
  // 'mcp' is the MCP-client settings page, not the MCP server integration tab.
  assert.equal(normalizeSettingsSection('mcp'), 'mcp')
  assert.equal(normalizeSettingsSection('integrations', 'cli'), 'integration-cli')
  assert.equal(normalizeSettingsSection('integrations', 'embed'), 'integration-embed')
  assert.equal(normalizeSettingsSection('integrations'), 'integration-im')
  assert.equal(normalizeSettingsSection('integration-chrome'), 'integration-chrome')
  assert.equal(normalizeSettingsSection('system-global'), 'system-global')
  assert.equal(isIntegrationSection('integration-chrome'), true)
  assert.equal(isIntegrationSection('integration-cli'), true)
  assert.equal(isIntegrationSection('models'), false)
  assert.equal(isIntegrationSection('integration-unknown'), false)
})

test('canonical settings query skips a redundant replace', () => {
  assert.equal(
    settingsQueryUnchanged(
      { section: 'integration-claw' },
      { section: 'integration-claw' },
    ),
    true,
  )
  assert.equal(
    settingsQueryUnchanged(
      { section: 'integrations', tab: 'claw' },
      { section: 'integration-claw' },
    ),
    false,
  )
})

const closeRouter = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/platform', redirect: '/platform/knowledge-bases', children: [
    { path: 'settings', component: {} },
    { path: 'knowledge-bases', component: {} },
    { path: 'chat/:id', component: {} },
    { path: 'tenant', redirect: '/platform/settings' },
  ] }],
})

test('closing direct or consecutive settings entries always leaves settings', async () => {
  for (const previous of [null, undefined, '/platform/settings?section=models', '/platform/settings/?section=chatgpt', '/platform/tenant', 'https://example.com']) {
    await closeRouter.push('/platform/settings?section=models')
    await closeRouter.replace(settingsCloseTarget(previous, closeRouter.resolve))
    assert.equal(closeRouter.currentRoute.value.path, '/platform/knowledge-bases')
  }
})

test('closing settings preserves a valid chat location including query and anchor', async () => {
  const previous = '/platform/chat/conversation-1?view=history#message-2'
  await closeRouter.push('/platform/settings?section=chatgpt')
  await closeRouter.replace(settingsCloseTarget(previous, closeRouter.resolve))
  assert.equal(closeRouter.currentRoute.value.fullPath, previous)
})

import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const editor = readFileSync(new URL('./AgentEditorModal.vue', import.meta.url), 'utf8')
const input = readFileSync(new URL('../../components/Input-field.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../../api/agent/index.ts', import.meta.url), 'utf8')

function between(source: string, start: string, end: string): string {
  const from = source.indexOf(start)
  assert.notEqual(from, -1, `missing start marker: ${start}`)
  const to = source.indexOf(end, from + start.length)
  assert.notEqual(to, -1, `missing end marker: ${end}`)
  return source.slice(from, to)
}

test('built-in read-only editor saves only the personal model preference', () => {
  assert.match(api, /getAgentModelPreference\(id: string\)[\s\S]*\/api\/v1\/agents\/\$\{id\}\/model-preference/)
  assert.match(api, /updateAgentModelPreference\(id: string, modelId: string\)[\s\S]*model_id: modelId/)
  assert.match(editor, /const personalModelMode = computed\(\(\) => props\.readOnly && isBuiltinAgent\.value\)/)
  assert.match(editor, /class="settings-group" :inert="personalModelMode"/)

  const save = between(editor, 'const savePersonalModelPreference = async (): Promise<boolean> => {', 'const clearPersonalModelPreference')
  assert.match(save, /updateAgentModelPreference\(agentId, modelId\)/)
  assert.doesNotMatch(save, /updateAgent\(/)
  assert.match(save, /chatResources\.ensureAgents\(true\)/)
})

test('chat readiness and selection prefer the current user personal model', () => {
  assert.match(input, /effectiveAgentModelId, configWithEffectiveAgentModel/)
  assert.match(input, /configWithEffectiveAgentModel\(agent, sourceTenantId\)/)
  assert.match(input, /const agentModel = effectiveAgentModelId\(actualAgent, sourceTenantId\)/)
})

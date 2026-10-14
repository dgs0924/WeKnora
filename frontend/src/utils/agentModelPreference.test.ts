import assert from 'node:assert/strict'
import test from 'node:test'

import {
  configWithEffectiveAgentModel,
  effectiveAgentModelId,
} from './agentModelPreference.ts'

const sharedConfig = { agent_mode: 'quick-answer' as const, model_id: 'space-model', temperature: 0.7 }

test('个人模型优先于空间默认模型', () => {
  const agent = { config: sharedConfig, personal_model_id: 'personal-model' }
  assert.equal(effectiveAgentModelId(agent), 'personal-model')
  assert.deepEqual(configWithEffectiveAgentModel(agent), {
    ...sharedConfig,
    model_id: 'personal-model',
  })
  assert.deepEqual(agent.config, sharedConfig)
})

test('空个人模型继承空间默认，缺少智能体安全返回空字符串', () => {
  assert.equal(effectiveAgentModelId({ config: sharedConfig, personal_model_id: '  ' }), 'space-model')
  assert.equal(effectiveAgentModelId(undefined), '')
})

test('跨空间共享智能体不套用本空间个人偏好', () => {
  const agent = { config: sharedConfig, personal_model_id: 'personal-model' }
  assert.equal(effectiveAgentModelId(agent, 'source-tenant'), 'space-model')
  assert.strictEqual(configWithEffectiveAgentModel(agent, 'source-tenant'), sharedConfig)
})

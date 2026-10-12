import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true },
}).outputText

function fixture(id: string | undefined = 'owned-model') {
  const calls: unknown[][] = []
  const model = { id: 'owned-model', name: 'available-model', parameters: { provider: 'openai', extra_config: { chatgpt_plan: 'true' } } }
  const exports: Record<string, any> = {}
  runInNewContext(compiled, {
    exports, console,
    require: (path: string) => {
      if (path === '@/i18n') return { global: { t: (key: string) => key } }
      if (path === './modelUsage') return {}
      if (path === '../chatgpt') return {
        selectChatGPTModel: async (slug: string, type: string) => { calls.push(['enable', slug, type]); return { model_id: id } },
      }
      if (path === '../codex') return {
        selectCodexModel: async (slug: string) => { calls.push(['enable-codex', slug]); return { model_id: id } },
      }
      if (path === '../../utils/request') return {
        get: async (url: string) => { calls.push(['get', url]); return { success: true, data: model } },
        post: async (url: string, body: unknown) => { calls.push(['post', url, body]); return { success: true, data: model } },
      }
      throw new Error(`Unexpected dependency: ${path}`)
    },
  })
  return { create: exports.createModel, calls, model }
}

test('personal plan creation uses the account selection endpoint and returns the saved model', async () => {
  const f = fixture()
  const result = await f.create({ source: 'remote', type: 'KnowledgeQA', name: ' available-model ', parameters: {
    provider: 'chatgpt_plan', api_key: 'must-not-send', base_url: 'https://untrusted.example',
    extra_config: { chatgpt_owner: 'another-user' },
  } })
  assert.equal(result, f.model)
  assert.deepEqual(f.calls, [['enable', 'available-model', 'KnowledgeQA'], ['get', '/api/v1/models/owned-model']])
})

test('personal plan creation supports VLM and passes its type to the account selection endpoint', async () => {
  const f = fixture()
  const result = await f.create({ source: 'remote', type: 'VLLM', name: ' visual-model ', parameters: {
    provider: 'chatgpt_plan', api_key: 'must-not-send', base_url: 'https://untrusted.example',
  } })
  assert.equal(result, f.model)
  assert.deepEqual(f.calls, [['enable', 'visual-model', 'VLLM'], ['get', '/api/v1/models/owned-model']])
})

test('personal plan creation rejects unsupported types before requesting a model', async () => {
  const f = fixture()
  for (const type of ['Embedding', 'Rerank', 'ASR']) {
    await assert.rejects(f.create({ source: 'remote', type, name: 'model', parameters: { provider: 'chatgpt_plan' } }))
  }
  assert.equal(f.calls.length, 0)
})

test('a missing saved ID cannot appear as successful model creation', async () => {
  const f = fixture('')
  await assert.rejects(f.create({ source: 'remote', type: 'KnowledgeQA', name: 'model', parameters: { provider: 'chatgpt_plan' } }))
  assert.deepEqual(f.calls, [['enable', 'model', 'KnowledgeQA']])
})

test('ordinary OpenAI API models retain the existing creation path', async () => {
  const f = fixture()
  const input = { source: 'remote', type: 'KnowledgeQA', name: 'model', parameters: { provider: 'openai' } }
  assert.equal(await f.create(input), f.model)
  assert.deepEqual(f.calls, [['post', '/api/v1/models', input]])
})

test('server account model creation enables only the selected Codex model and never posts credentials', async () => {
  const f = fixture()
  const result = await f.create({ source: 'remote', type: 'KnowledgeQA', name: ' server-model ', parameters: {
    provider: 'codex_cli', api_key: 'must-not-send', base_url: 'https://untrusted.example',
  } })
  assert.equal(result, f.model)
  assert.deepEqual(f.calls, [['enable-codex', 'server-model'], ['get', '/api/v1/models/owned-model']])
})

test('server account creation supports visual models and rejects non-dialog types', async () => {
  const f = fixture()
  await f.create({ source: 'remote', type: 'VLLM', name: 'vision-model', parameters: { provider: 'codex_cli' } })
  assert.deepEqual(f.calls, [['enable-codex', 'vision-model'], ['get', '/api/v1/models/owned-model']])
  f.calls.length = 0
  await assert.rejects(f.create({ source: 'remote', type: 'Embedding', name: 'model', parameters: { provider: 'codex_cli' } }))
  assert.equal(f.calls.length, 0)
})

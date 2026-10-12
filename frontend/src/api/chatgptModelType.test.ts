import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('./chatgpt.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

function fixture(): {
  calls: unknown[][]
  listChatGPTModels: (type?: 'KnowledgeQA' | 'VLLM') => Promise<any[]>
  selectChatGPTModel: (slug: string, type?: 'KnowledgeQA' | 'VLLM') => Promise<any>
} {
  const calls: unknown[][] = []
  const exports: Record<string, any> = {}
  runInNewContext(compiled, {
    exports,
    require: (path: string) => {
      if (path === '@/utils/request') return {
        get: async (url: string) => {
          calls.push(['get', url])
          return { data: { models: [{ slug: 'gpt-vision', display_name: 'GPT Vision', supports_vision: true }] } }
        },
        post: async (url: string, body: unknown) => {
          calls.push(['post', url, body])
          return { data: { model_id: 'saved-model' } }
        },
        del: async () => undefined,
      }
      throw new Error(`Unexpected dependency: ${path}`)
    },
  })
  return { ...exports, calls } as ReturnType<typeof fixture>
}

test('ChatGPT plan API uses the VLLM query and selection body only for vision models', async () => {
  const f = fixture()
  const visionModels = await f.listChatGPTModels('VLLM')
  await f.selectChatGPTModel('gpt-vision', 'VLLM')
  await f.listChatGPTModels()
  await f.selectChatGPTModel('gpt-chat')

  assert.equal(visionModels[0].supports_vision, true)
  assert.deepEqual(JSON.parse(JSON.stringify(f.calls)), [
    ['get', '/api/v1/chatgpt/models?model_type=VLLM'],
    ['post', '/api/v1/chatgpt/models/select', { slug: 'gpt-vision', model_type: 'VLLM' }],
    ['get', '/api/v1/chatgpt/models'],
    ['post', '/api/v1/chatgpt/models/select', { slug: 'gpt-chat' }],
  ])
})

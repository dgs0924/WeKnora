import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('./codex.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

function fixture(response: unknown): { calls: unknown[][]; [key: string]: any } {
  const calls: unknown[][] = []
  const exports: Record<string, any> = {}
  runInNewContext(compiled, {
    exports,
    require: (path: string) => {
      if (path === '@/utils/request') return {
        get: async (url: string) => { calls.push(['get', url]); return response },
        post: async () => undefined,
        put: async (url: string, body: unknown) => { calls.push(['put', url, body]); return undefined },
      }
      throw new Error(`Unexpected dependency: ${path}`)
    },
  })
  return { ...exports, calls }
}

test('Codex grant payload normalizes missing user_ids to an empty list', async () => {
  const f = fixture({ data: { models: [{ id: 'model-a', name: 'a', display_name: 'A', user_ids: null }] } })
  const grants = await f.listCodexModelGrants()
  assert.deepEqual(JSON.parse(JSON.stringify(grants)), [
    { id: 'model-a', name: 'a', display_name: 'A', user_ids: [] },
  ])
})

test('Codex access is saved with one atomic user-level request', async () => {
  const f = fixture({ data: { models: [] } })
  await f.updateCodexUserGrants('user/one', ['model-a', 'model-b'])
  assert.deepEqual(JSON.parse(JSON.stringify(f.calls)), [
    ['put', '/api/v1/codex/grants/users/user%2Fone', { model_ids: ['model-a', 'model-b'] }],
  ])
})

test('Codex usage preserves unavailable values instead of treating them as zero', async () => {
  const f = fixture({ data: { available: true, limits: [{ id: 'codex', name: 'Codex', windows: [{ kind: 'primary', used_percent: null, remaining_percent: null, window_duration_mins: 10080, resets_at: null }] }] } })
  const usage = await f.getCodexUsage()
  assert.equal(usage.limits[0].windows[0].used_percent, null)
  assert.equal(usage.limits[0].windows[0].remaining_percent, null)
  assert.deepEqual(JSON.parse(JSON.stringify(f.calls)), [['get', '/api/v1/codex/usage']])
})

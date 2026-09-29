// Generates TypeScript types for every WebSocket message from docs/api/schemas (D13).
// All messages are compiled together so shared definitions (common.json) appear once.
import { compile } from 'json-schema-to-typescript'
import { readdirSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'

const schemas = resolve(import.meta.dirname, '../../docs/api/schemas')
const out = process.argv[2] ?? resolve(import.meta.dirname, '../../client/src/protocol/messages.ts')

const properties = {}
for (const dir of ['client', 'server']) {
  for (const file of readdirSync(join(schemas, 'ws', dir)).sort()) {
    if (file.endsWith('.json')) properties[`${dir}_${file.replace('.json', '')}`] = { $ref: `./ws/${dir}/${file}` }
  }
}

const ts = await compile(
  { title: 'Protocol', type: 'object', additionalProperties: false, properties },
  'Protocol',
  {
    cwd: schemas,
    declareExternallyReferenced: true,
    additionalProperties: false,
    bannerComment: '// Generated from docs/api/schemas by tools/contracts/gen-protocol.mjs. Do not edit; run `make generate`.',
    style: { singleQuote: true, semi: false },
  },
)
writeFileSync(out, ts)

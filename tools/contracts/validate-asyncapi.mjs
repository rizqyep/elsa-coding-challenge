// Validates docs/api/asyncapi.yaml, including the referenced JSON Schemas (D13).
// Uses the AsyncAPI parser directly: same validation as the AsyncAPI CLI, without its usage analytics.
import { Parser, fromFile } from '@asyncapi/parser'
import { resolve } from 'node:path'

const file = resolve(import.meta.dirname, '../../docs/api/asyncapi.yaml')
const { document, diagnostics } = await fromFile(new Parser(), file).parse()

const errors = diagnostics.filter((d) => d.severity === 0)
const warnings = diagnostics.filter((d) => d.severity === 1)
for (const d of [...errors, ...warnings]) {
  console.log(`${d.severity === 0 ? 'error' : 'warning'} ${d.code}: ${d.message} (${d.path.join('.')})`)
}
if (!document || errors.length > 0) {
  console.error(`asyncapi.yaml: invalid (${errors.length} errors)`)
  process.exit(1)
}
console.log(`asyncapi.yaml: valid (${warnings.length} warnings, ${document.allMessages().length} messages)`)

import type { FC } from 'hono/jsx'
import { docsBase } from '../../docs-base'
import { DocsLayout } from '../../layouts/docs'
import { CodeBlock } from '../../components/terminal-block'
import apiSchema from '../../generated/api-schema.json'

type FieldDef = {
  json_name: string
  type: string
  optional?: boolean
}

type TypeDef = {
  name: string
  fields: FieldDef[]
}

type ParamDef = {
  name: string
  type: string
  description: string
}

type EndpointDef = {
  method: string
  path: string
  description: string
  category: string
  response?: string
  request?: string
  query_params?: ParamDef[]
  notes?: string[]
}

const schema = apiSchema as {
  endpoints: EndpointDef[]
  types: Record<string, TypeDef>
}

const primitives = new Set(['string', 'number', 'boolean', 'any'])

function isPrimitive(t: string): boolean {
  return primitives.has(t.replace('[]', ''))
}

function typeAnchor(t: string): string {
  return `type-${t.replace('[]', '')}`
}

const MethodBadge: FC<{ method: string }> = ({ method }) => {
  const colors: Record<string, string> = {
    GET: 'text-emerald-400',
    POST: 'text-amber-400',
    PUT: 'text-blue-400',
    DELETE: 'text-red-400',
  }
  return <code class={colors[method] ?? 'text-muted'}>{method}</code>
}

const TypeLink: FC<{ type: string }> = ({ type }) => {
  const isArray = type.endsWith('[]')
  const base = type.replace('[]', '')
  if (isPrimitive(base)) {
    return <code>{type}</code>
  }
  // Check if it's a map type
  if (type.startsWith('map<')) {
    return <code>{type}</code>
  }
  return (
    <span>
      <a href={`#${typeAnchor(base)}`} class="type-link no-underline"><code>{base}</code></a>
      {isArray && <code>[]</code>}
    </span>
  )
}

const FieldTable: FC<{ typeDef: TypeDef }> = ({ typeDef }) => (
  <table>
    <thead>
      <tr><th>Field</th><th>Type</th><th></th></tr>
    </thead>
    <tbody>
      {typeDef.fields.map(f => (
        <tr>
          <td><code>{f.json_name}</code></td>
          <td><TypeLink type={f.type} /></td>
          <td>{f.optional ? <span class="text-muted text-xs">optional</span> : ''}</td>
        </tr>
      ))}
    </tbody>
  </table>
)

// Group endpoints by category, preserving order
function groupEndpoints(endpoints: EndpointDef[]): [string, EndpointDef[]][] {
  const groups: [string, EndpointDef[]][] = []
  const seen = new Set<string>()
  for (const ep of endpoints) {
    if (!seen.has(ep.category)) {
      seen.add(ep.category)
      groups.push([ep.category, []])
    }
    groups.find(g => g[0] === ep.category)![1].push(ep)
  }
  return groups
}

export const ApiDocs: FC = () => {
  const groups = groupEndpoints(schema.endpoints)

  // Collect types referenced by endpoints, ordered for rendering:
  // top-level response/request types first, then their nested dependencies.
  const typeOrder: string[] = []
  const visited = new Set<string>()

  function collectTypes(name: string) {
    const base = name.replace('[]', '')
    if (visited.has(base) || isPrimitive(base) || base.startsWith('map<')) return
    visited.add(base)
    // Add this type
    typeOrder.push(base)
    // Recurse into its fields
    const td = schema.types[base]
    if (td) {
      for (const f of td.fields) {
        collectTypes(f.type)
      }
    }
  }

  for (const ep of schema.endpoints) {
    if (ep.response) collectTypes(ep.response)
    if (ep.request) collectTypes(ep.request)
  }

  return (
    <DocsLayout title="API Reference" active={`${docsBase}/api`}>
      <p>
        The daemon exposes an HTTP API over its unix socket. When TCP is enabled, the same API is available
        over TLS with optional bearer token authentication. All responses are JSON.
      </p>

      <h2>Endpoints</h2>

      {groups.map(([category, endpoints]) => (
        <div>
          <h3>{category}</h3>
          {category === 'History' && (
            <p>
              All history endpoints accept <code>?start=</code> and <code>?end=</code> query parameters (Unix seconds).
              Bucket size auto-scales: 1 min (1h range) to 6 hr (30d range).
            </p>
          )}
          <table>
            <thead>
              <tr><th>Method</th><th>Path</th><th>Description</th><th>Response</th></tr>
            </thead>
            <tbody>
              {endpoints.map(ep => (
                <tr>
                  <td><MethodBadge method={ep.method} /></td>
                  <td><code>{ep.path}</code></td>
                  <td>{ep.description}</td>
                  <td>{ep.response ? <TypeLink type={ep.response} /> : ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ))}

      <h2>Examples</h2>

      <CodeBlock title="get daemon status">
{`curl --unix-socket /run/bewitch/bewitch.sock \\
  http://localhost/api/status`}
      </CodeBlock>

      <CodeBlock title="get CPU metrics">
{`curl --unix-socket /run/bewitch/bewitch.sock \\
  http://localhost/api/metrics/cpu`}
      </CodeBlock>

      <CodeBlock title="get history with time range">
{`curl --unix-socket /run/bewitch/bewitch.sock \\
  "http://localhost/api/history/cpu?start=$(date -d '1 hour ago' +%s)&end=$(date +%s)"`}
      </CodeBlock>

      <CodeBlock title="create alert rule">
{`curl --unix-socket /run/bewitch/bewitch.sock \\
  -H 'Content-Type: application/json' \\
  -d '{
    "name": "high-cpu",
    "type": "threshold",
    "severity": "warning",
    "metric": "cpu.aggregate",
    "operator": ">",
    "value": 90,
    "duration": "5m"
  }' \\
  http://localhost/api/alert-rules`}
      </CodeBlock>

      <CodeBlock title="execute SQL query">
{`curl --unix-socket /run/bewitch/bewitch.sock \\
  -H 'Content-Type: application/json' \\
  -d '{"sql": "SELECT COUNT(*) as n FROM cpu_metrics"}' \\
  http://localhost/api/query`}
      </CodeBlock>

      <CodeBlock title="remote access (TCP + TLS + auth)">
{`curl -k -H "Authorization: Bearer my-secret-token" \\
  https://myserver:9119/api/status`}
      </CodeBlock>

      <h2>Response Types</h2>
      <p>
        Timestamps are <code>int64</code> Unix nanoseconds. Arrays are always wrapped in objects.
        Errors return <code>&#123;"error": "message"&#125;</code>.
      </p>

      {typeOrder.map(name => {
        const td = schema.types[name]
        if (!td) return null
        return (
          <div id={typeAnchor(name)}>
            <h3>{name}</h3>
            <FieldTable typeDef={td} />
          </div>
        )
      })}

      <h2>ETag Caching</h2>
      <p>
        Live metric endpoints include <code>ETag</code> headers. Send
        <code>If-None-Match</code> to receive <code>304 Not Modified</code> when data hasn't changed.
      </p>
    </DocsLayout>
  )
}

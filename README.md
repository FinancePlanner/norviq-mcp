# norviq-mcp

Remote [MCP](https://modelcontextprotocol.io) server that lets external AI
clients (Claude, ChatGPT, Cursor, Gemini, …) operate on a user's norviq account
over Streamable HTTP.

## How it works

```
AI client ──Bearer (personal access token / OAuth)──▶ norviq-mcp ──REST──▶ norviq-backend
```

- `/mcp` — Streamable HTTP MCP endpoint. Every request must carry a norviq
  personal access token (`nvq_pat_…`) as `Authorization: Bearer`.
- The bearer is validated against the backend's `POST /v1/oauth/introspect`
  (cached 60s). MCP-connector access requires norviq Pro; the token's scopes
  determine which tools are exposed.
- `/.well-known/oauth-protected-resource` — RFC 9728 metadata pointing clients
  at the backend as the authorization server (used once OAuth 2.1 lands).
- `/healthz`, `/readyz` — liveness/readiness.

## Tools (v1)

| Scope | Tools |
|-------|-------|
| `expenses:read` | `list_expenses`, `list_expense_categories` |
| `expenses:write` | `add_expense`, `update_expense`, `delete_expense` |
| `reports:read` | `get_spending_report` |

Tools are registered per session only when the token holds their scope, so a
client never sees a tool it cannot use.

## Configuration

| Env | Default | Meaning |
|-----|---------|---------|
| `BACKEND_BASE_URL` | `http://localhost:8080` | norviq-backend base URL |
| `MCP_PUBLIC_URL` | `http://localhost:8087` | this service's public URL (for metadata) |
| `LISTEN_ADDR` | `:8087` | listen address |
| `MCP_INTROSPECTION_SECRET` | — (required) | shared secret for backend introspection |

## Local development

```sh
# backend running on :8090 with BYPASS_BILLING=true, a PAT minted for a user
export BACKEND_BASE_URL=http://localhost:8090
export MCP_INTROSPECTION_SECRET=dev-introspection-secret
make run

# in another shell — inspect with the MCP Inspector
npx @modelcontextprotocol/inspector
# or add to Claude Code
claude mcp add --transport http norviq http://localhost:8087/mcp \
  --header "Authorization: Bearer nvq_pat_…"
```

## Testing

```sh
make test
```

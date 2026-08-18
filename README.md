# norviq-mcp

Remote [MCP](https://modelcontextprotocol.io) server that lets external AI
clients (Claude, ChatGPT, Cursor, Hermes, Gemini, …) operate on a user's
Norviq account over Streamable HTTP.

**Production:** `https://mcp.norviq.org/mcp`  
**Canonical product/eng doc:** [`Documentation/mcp-integration.md`](../Documentation/mcp-integration.md)  
**Operator smoke tests:** [`mcp-test.md`](../mcp-test.md)

## Who pays for what

| Piece | Who provides | Who pays |
|-------|--------------|----------|
| MCP tools + portfolio/expense/market data | Norviq | Included with Norviq Pro (connector access) |
| LLM tokens (chat / reasoning) | User's AI client (Claude, Cursor, ChatGPT, Hermes, …) | **User** — Norviq does not bill model usage for MCP |
| Norviq personal access token (`nvq_pat_…`) | User mints in Settings → API access | Auth to Norviq tools only — **not** an OpenAI/Anthropic API key |

Bring-your-own-client is intentional: Norviq stays the data/tool layer; users keep LLM costs on their existing AI subscription.

In-app Norviq Assistant (first-party UI) is separate and uses Norviq’s server-side provider key when enabled.

## How it works

```
AI client ──Bearer (personal access token / OAuth)──▶ norviq-mcp ──REST──▶ norviq-backend
         └── (LLM billed by Claude / Cursor / ChatGPT / Hermes — not by Norviq)
```

- `/mcp` — Streamable HTTP MCP endpoint. Every request must carry a norviq
  personal access token (`nvq_pat_…`) as `Authorization: Bearer`.
- The bearer is validated against the backend's `POST /v1/oauth/introspect`
  (cached 60s). MCP-connector access requires norviq Pro; the token's scopes
  determine which tools are exposed.
- `/.well-known/oauth-protected-resource` — RFC 9728 metadata pointing clients
  at the backend as the authorization server (`https://api.norviq.org`).
- `/healthz`, `/readyz` — liveness/readiness.

## Tools

Tools are registered per session only when the token holds their scope.

| Scope | Tools |
|-------|--------|
| `expenses:read` | `list_expenses`, `list_expense_categories`, `list_recurring_expenses`, `export_expenses_csv`, `get_dca_capacity` |
| `expenses:write` | `add_expense`, `update_expense`, `delete_expense`, recurring add/update/delete, `import_expenses_csv` |
| `reports:read` | `get_spending_report` |
| `market:read` | `get_quote`, `search_symbols`, `get_portfolio_summary` |
| `insights:read` | `get_insights` |
| `tax:read` | `get_tax_dashboard`, `get_tax_loss_carryforwards` |
| planning / budget (see `internal/tools`) | `list_goals`, goal CRUD, budget snapshot/item CRUD |

Write tools require the matching `:write` scope and use the pending-confirmation flow where implemented. Source of truth: `internal/tools/*.go`.

## Connect a client (quick)

```bash
# Claude Code / similar
claude mcp add --transport http norviq https://mcp.norviq.org/mcp \
  --header "Authorization: Bearer nvq_pat_…"

# Hermes (on the agent host, after minting a PAT on the web app)
sudo hermes --profile mac-mcp mcp add norviq \
  --url https://mcp.norviq.org/mcp \
  --auth header
```

Full recipes (Claude app, ChatGPT, Cursor, Hermes, Inspector):  
[`Documentation/mcp-integration.md`](../Documentation/mcp-integration.md).

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

Production smoke tests and failure modes: [`mcp-test.md`](../mcp-test.md).

## Product UI

| Surface | Path |
|---------|------|
| Web | Settings → **API access** (`/settings/api-access`), Integrations |
| iOS | Profile → Integrations → **Connect an AI agent (MCP)** |
